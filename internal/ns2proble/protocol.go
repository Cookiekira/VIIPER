package ns2proble

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	NintendoManufacturerID = 0x0553
	NintendoVID            = 0x057E
	NS2ProPID              = 0x2069

	FeatureButtons = 0x01
	FeatureSticks  = 0x02
	FeatureIMU     = 0x04

	DefaultFeatureFlags = FeatureButtons | FeatureSticks | FeatureIMU
	DefaultLEDPattern   = 0x06
)

type Device struct {
	Address uint64
	Name    string
}

func (d Device) AddressString() string {
	return FormatBluetoothAddress(d.Address)
}

type Backend interface {
	Scan(ctx context.Context) (Device, error)
	Connect(ctx context.Context, device Device) (Peripheral, error)
}

type Peripheral interface {
	Close() error
	WriteInit(ctx context.Context, data []byte) error
	WriteCommand(ctx context.Context, data []byte) error
	WriteVibration(ctx context.Context, data []byte) error
	NotifyCommandResponse(ctx context.Context, cb func([]byte)) error
	NotifyInput(ctx context.Context, cb func([]byte)) error
	SetInputReportRate(ctx context.Context, data []byte) error
}

type Controller struct {
	backend      Backend
	logger       *slog.Logger
	featureFlags byte

	mu         sync.Mutex
	peripheral Peripheral
	latest     InputState
	cmdResp    chan []byte

	primaryStick   *StickCalibration
	secondaryStick *StickCalibration
}

func NewController(backend Backend, logger *slog.Logger, featureFlags byte) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	if featureFlags == 0 {
		featureFlags = DefaultFeatureFlags
	}
	return &Controller{
		backend:      backend,
		logger:       logger,
		featureFlags: featureFlags,
		cmdResp:      make(chan []byte, 4),
		latest: InputState{
			LX:            StickCenter,
			LY:            StickCenter,
			RX:            StickCenter,
			RY:            StickCenter,
			BatteryLevel:  BatteryMax,
			ExternalPower: true,
		},
	}
}

func (c *Controller) LatestInput() InputState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latest
}

func (c *Controller) SetPeripheral(p Peripheral) {
	c.mu.Lock()
	c.peripheral = p
	c.mu.Unlock()
}

func (c *Controller) Peripheral() Peripheral {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peripheral
}

func (c *Controller) ConnectAndInitialize(ctx context.Context, device Device, input func(InputState)) error {
	p, err := c.backend.Connect(ctx, device)
	if err != nil {
		return err
	}
	c.SetPeripheral(p)

	if err := p.WriteInit(ctx, []byte{0x01, 0x00}); err != nil {
		_ = p.Close()
		return fmt.Errorf("write init: %w", err)
	}
	if err := p.NotifyCommandResponse(ctx, c.onCommandResponse); err != nil {
		_ = p.Close()
		return fmt.Errorf("enable command response notifications: %w", err)
	}
	if err := c.SetPlayerLEDs(ctx, DefaultLEDPattern); err != nil {
		c.logger.Debug("initial LED command failed", "error", err)
	}
	if err := c.ReadStickCalibration(ctx); err != nil {
		c.logger.Warn("could not load stick calibration; using raw stick values", "error", err)
	}
	if _, err := c.SendCommand(ctx, ControllerCommand(0x0C, 0x02, []byte{0xFF, 0, 0, 0}, 1)); err != nil {
		c.logger.Debug("configure features failed", "error", err)
	}
	if _, err := c.SendCommand(ctx, ControllerCommand(0x0C, 0x04, []byte{c.featureFlags, 0, 0, 0}, 1)); err != nil {
		c.logger.Debug("enable features failed", "error", err)
	}
	if err := p.SetInputReportRate(ctx, []byte{0x85, 0x00}); err != nil {
		c.logger.Debug("set input report rate failed", "error", err)
	}
	if err := p.NotifyInput(ctx, func(data []byte) {
		st, err := ParseCommonReport(data, c.primaryStick, c.secondaryStick)
		if err != nil {
			c.logger.Debug("parse BLE input failed", "error", err)
			return
		}
		c.mu.Lock()
		c.latest = st
		c.mu.Unlock()
		if input != nil {
			input(st)
		}
	}); err != nil {
		_ = p.Close()
		return fmt.Errorf("enable input notifications: %w", err)
	}
	return nil
}

func (c *Controller) Close() error {
	p := c.Peripheral()
	if p == nil {
		return nil
	}
	return p.Close()
}

func (c *Controller) onCommandResponse(data []byte) {
	select {
	case c.cmdResp <- append([]byte(nil), data...):
	default:
		c.logger.Debug("dropping stale command response")
	}
}

func (c *Controller) SendCommand(ctx context.Context, cmd []byte) ([]byte, error) {
	p := c.Peripheral()
	if p == nil {
		return nil, errors.New("BLE controller is not connected")
	}
	for {
		select {
		case <-c.cmdResp:
		default:
			goto drained
		}
	}
drained:
	if err := p.WriteCommand(ctx, cmd); err != nil {
		return nil, err
	}
	select {
	case resp := <-c.cmdResp:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return nil, fmt.Errorf("command timeout: %x", cmd)
	}
}

func (c *Controller) SendRumble(ctx context.Context, left, right []byte) error {
	p := c.Peripheral()
	if p == nil {
		return nil
	}
	packet := make([]byte, 42)
	copy(packet[1:17], left)
	copy(packet[17:33], right)
	return p.WriteVibration(ctx, packet)
}

func (c *Controller) SetPlayerLEDs(ctx context.Context, mask byte) error {
	_, err := c.SendCommand(ctx, ControllerCommand(0x09, 0x07, append([]byte{mask}, make([]byte, 7)...), 1))
	return err
}

func (c *Controller) ReadSPI(ctx context.Context, address uint32, size byte) ([]byte, error) {
	payload := []byte{size, 0x7E, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(payload[4:8], address)
	resp, err := c.SendCommand(ctx, ControllerCommand(0x02, 0x04, payload, 1))
	if err != nil {
		return nil, err
	}
	data, err := ControllerResponsePayload(resp, 0x02, 0x04)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("short SPI response: %x", data)
	}
	gotAddr := binary.LittleEndian.Uint32(data[4:8])
	if gotAddr != address {
		return nil, fmt.Errorf("SPI address mismatch: got %#x want %#x", gotAddr, address)
	}
	n := int(data[0])
	if len(data) < 8+n {
		return nil, fmt.Errorf("short SPI payload: have %d want %d", len(data)-8, n)
	}
	return append([]byte(nil), data[8:8+n]...), nil
}

func (c *Controller) ReadStickCalibration(ctx context.Context) error {
	primaryBlock, err := c.ReadSPI(ctx, 0x13080, 0x40)
	if err != nil {
		return err
	}
	secondaryBlock, err := c.ReadSPI(ctx, 0x130C0, 0x40)
	if err != nil {
		return err
	}
	primary, err := UnpackStickCalibration(primaryBlock[0x28:0x31])
	if err != nil {
		return err
	}
	secondary, err := UnpackStickCalibration(secondaryBlock[0x28:0x31])
	if err != nil {
		return err
	}
	if userBlock, err := c.ReadSPI(ctx, 0x1FC040, 0x40); err == nil {
		if len(userBlock) >= 0x2B {
			if bytes.Equal(userBlock[0:2], []byte{0xA2, 0xB2}) {
				if cal, err := UnpackStickCalibration(userBlock[0x02:0x0B]); err == nil {
					primary = cal
				}
			}
			if bytes.Equal(userBlock[0x20:0x22], []byte{0xA2, 0xB2}) {
				if cal, err := UnpackStickCalibration(userBlock[0x22:0x2B]); err == nil {
					secondary = cal
				}
			}
		}
	} else {
		c.logger.Debug("user stick calibration read failed", "error", err)
	}
	c.primaryStick = &primary
	c.secondaryStick = &secondary
	c.logger.Info("Loaded stick calibration", "primary", primary, "secondary", secondary)
	return nil
}

func (c *Controller) PairHost(ctx context.Context, hostAddress uint64) error {
	primary := BluetoothAddressBytes(hostAddress)
	secondary := append([]byte(nil), primary...)
	secondary[5]--
	payload := append([]byte{0x00, 0x02}, ReverseBytes(primary)...)
	payload = append(payload, ReverseBytes(secondary)...)
	resp, err := c.SendCommand(ctx, ControllerCommand(0x15, 0x01, payload, 1))
	if err != nil {
		return err
	}
	data, err := ControllerResponsePayload(resp, 0x15, 0x01)
	if err != nil {
		return err
	}
	if len(data) < 9 || data[0] != 1 {
		return fmt.Errorf("address exchange failed: %x", data)
	}

	hostKey := make([]byte, 16)
	if _, err := rand.Read(hostKey); err != nil {
		return err
	}
	resp, err = c.SendCommand(ctx, ControllerCommand(0x15, 0x04, append([]byte{0}, hostKey...), 1))
	if err != nil {
		return err
	}
	data, err = ControllerResponsePayload(resp, 0x15, 0x04)
	if err != nil {
		return err
	}
	if len(data) < 17 || data[0] != 1 {
		return fmt.Errorf("key exchange failed: %x", data)
	}
	deviceKey := data[1:17]
	ltk := make([]byte, 16)
	for i := range ltk {
		ltk[i] = hostKey[i] ^ deviceKey[i]
	}
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return err
	}
	resp, err = c.SendCommand(ctx, ControllerCommand(0x15, 0x02, append([]byte{0}, challenge...), 1))
	if err != nil {
		return err
	}
	data, err = ControllerResponsePayload(resp, 0x15, 0x02)
	if err != nil {
		return err
	}
	if len(data) < 17 || data[0] != 1 {
		return fmt.Errorf("LTK confirmation failed: %x", data)
	}
	block, err := aes.NewCipher(ReverseBytes(ltk))
	if err != nil {
		return err
	}
	expected := make([]byte, 16)
	block.Encrypt(expected, ReverseBytes(challenge))
	if !bytes.Equal(data[1:17], expected) {
		return errors.New("controller LTK confirmation response did not match")
	}
	resp, err = c.SendCommand(ctx, ControllerCommand(0x15, 0x03, []byte{0}, 1))
	if err != nil {
		return err
	}
	data, err = ControllerResponsePayload(resp, 0x15, 0x03)
	if err != nil {
		return err
	}
	if len(data) == 0 || data[0] != 1 {
		return fmt.Errorf("pairing finalise failed: %x", data)
	}
	return nil
}

func ControllerCommand(command, subcommand byte, payload []byte, seq byte) []byte {
	out := []byte{command, 0x91, seq, subcommand, 0x00, byte(len(payload)), 0x00, 0x00}
	return append(out, payload...)
}

func ControllerResponsePayload(response []byte, command, subcommand byte) ([]byte, error) {
	if len(response) < 8 {
		return nil, fmt.Errorf("short controller response: %x", response)
	}
	if response[0] != command || response[3] != subcommand {
		return nil, fmt.Errorf("unexpected controller response for %02x/%02x: %x", command, subcommand, response)
	}
	return response[8:], nil
}

func ParseBluetoothAddress(text string) (uint64, error) {
	cleaned := regexp.MustCompile(`[^0-9A-Fa-f]`).ReplaceAllString(text, "")
	if len(cleaned) != 12 {
		return 0, fmt.Errorf("invalid Bluetooth address %q", text)
	}
	var addr uint64
	for i := 0; i < 12; i += 2 {
		b, err := strconv.ParseUint(cleaned[i:i+2], 16, 8)
		if err != nil {
			return 0, err
		}
		addr = (addr << 8) | uint64(b)
	}
	return addr, nil
}

func FormatBluetoothAddress(addr uint64) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		byte(addr>>40), byte(addr>>32), byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
}

func BluetoothAddressBytes(addr uint64) []byte {
	return []byte{byte(addr >> 40), byte(addr >> 32), byte(addr >> 24), byte(addr >> 16), byte(addr >> 8), byte(addr)}
}

func ReverseBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func DefaultCacheFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".viiper", "ns2pro_ble_device.json")
}

func LoadCachedDevice(path string) (Device, bool) {
	if path == "" {
		path = DefaultCacheFile()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Device{}, false
	}
	var raw struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Device{}, false
	}
	addr, err := ParseBluetoothAddress(raw.Address)
	if err != nil {
		return Device{}, false
	}
	return Device{Address: addr}, true
}

func SaveCachedDevice(path string, d Device) error {
	if path == "" {
		path = DefaultCacheFile()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(struct {
		Address string `json:"address"`
	}{Address: d.AddressString()}, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

func ForgetCachedDevice(path string) error {
	if path == "" {
		path = DefaultCacheFile()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
