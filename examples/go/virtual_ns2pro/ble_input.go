package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Alia5/VIIPER/device/ns2pro"
	"tinygo.org/x/bluetooth"
)

const (
	ns2BLEInput05UUIDString = "ab7de9be-89fe-49ad-828f-118f09df7fd2"
	ns2BLEInput09UUIDString = "7492866c-ec3e-4619-8258-32755ffcc0f8"
)

type BLEInputOptions struct {
	Address       string
	NameContains  string
	Timeout       time.Duration
	Report        string
	RawLogPath    string
	DecodeLogPath string
}

type BLEInputClient struct {
	device    bluetooth.Device
	rawLog    *bleInputRawLogger
	decodeLog *bleInputDecodeLogger

	mu     sync.RWMutex
	latest []byte
	count  uint64
}

func ConnectBLEInput(ctx context.Context, options BLEInputOptions) (*BLEInputClient, error) {
	if options.Timeout <= 0 {
		options.Timeout = 12 * time.Second
	}
	report := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(options.Report), "0x"))
	if report == "" {
		report = "09"
	}
	report = canonicalBLEInputReport(report)
	if report == "" {
		return nil, fmt.Errorf("unsupported BLE input report %q; use 05 or 09", options.Report)
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable BLE adapter: %w", err)
	}

	serviceUUID, err := bluetooth.ParseUUID(ns2BLEServiceUUIDString)
	if err != nil {
		return nil, err
	}

	inputUUID, err := inputCharacteristicUUID(report)
	if err != nil {
		return nil, err
	}

	target, err := scanForNS2Pro(ctx, adapter, options.Address, options.NameContains, options.Timeout)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connecting BLE input device %s (%s)\n", target.DisplayName(), target.Address.String())
	device, err := adapter.Connect(target.Address, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(options.Timeout),
	})
	if err != nil {
		return nil, fmt.Errorf("connect BLE input device: %w", err)
	}

	services, err := device.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("discover NS2Pro BLE service %s: %w", ns2BLEServiceUUIDString, err)
	}
	if len(services) == 0 {
		_ = device.Disconnect()
		return nil, fmt.Errorf("NS2Pro BLE service %s not found", ns2BLEServiceUUIDString)
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{inputUUID})
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("discover input characteristic %s: %w", inputUUID.String(), err)
	}
	if len(chars) == 0 {
		_ = device.Disconnect()
		return nil, fmt.Errorf("input characteristic %s not found", inputUUID.String())
	}

	rawLog, err := newBLEInputRawLogger(options.RawLogPath, target.Address.String(), report)
	if err != nil {
		_ = device.Disconnect()
		return nil, err
	}
	decodeLog, err := newBLEInputDecodeLogger(options.DecodeLogPath)
	if err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		_ = device.Disconnect()
		return nil, err
	}

	client := &BLEInputClient{device: device, rawLog: rawLog, decodeLog: decodeLog}
	if err := chars[0].EnableNotifications(func(payload []byte) {
		client.logNotification(report, payload)
		usbReport, ok := normalizeBLEInputReport(report, payload)
		if !ok {
			return
		}
		client.store(usbReport)
	}); err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		if decodeLog != nil {
			_ = decodeLog.Close()
		}
		_ = device.Disconnect()
		return nil, fmt.Errorf("enable BLE input notifications: %w", err)
	}

	fmt.Printf("BLE input report 0x%s notifications enabled on %s (%s)\n", report, target.DisplayName(), target.Address.String())
	return client, nil
}

func (c *BLEInputClient) LatestInputReport() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.latest == nil {
		return nil
	}
	out := make([]byte, len(c.latest))
	copy(out, c.latest)
	return out
}

func (c *BLEInputClient) Count() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.count
}

func (c *BLEInputClient) Close() error {
	var err error
	if c.rawLog != nil {
		err = c.rawLog.Close()
	}
	if c.decodeLog != nil {
		if closeErr := c.decodeLog.Close(); err == nil {
			err = closeErr
		}
	}
	if disconnectErr := c.device.Disconnect(); err == nil {
		err = disconnectErr
	}
	return err
}

func (c *BLEInputClient) logNotification(report string, payload []byte) {
	if c.rawLog != nil {
		c.rawLog.Write(payload)
	}
	if c.decodeLog != nil && report == "09" {
		if decoded, ok := DecodeBLEInput09(payload); ok {
			c.decodeLog.Write(decoded)
		}
	}
}

func (c *BLEInputClient) store(report []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = append(c.latest[:0], report...)
	c.count++
	if c.count%250 == 0 {
		fmt.Printf("Received %d BLE input reports\n", c.count)
	}
}

func canonicalBLEInputReport(report string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(report), "0x")) {
	case "05", "5":
		return "05"
	case "09", "9":
		return "09"
	default:
		return ""
	}
}

func inputCharacteristicUUID(report string) (bluetooth.UUID, error) {
	switch report {
	case "05":
		return bluetooth.ParseUUID(ns2BLEInput05UUIDString)
	case "09":
		return bluetooth.ParseUUID(ns2BLEInput09UUIDString)
	default:
		return bluetooth.UUID{}, fmt.Errorf("unsupported BLE input report %q; use 05 or 09", report)
	}
}

func normalizeBLEInputReport(report string, payload []byte) ([]byte, bool) {
	switch report {
	case "05":
		return NormalizeBLEInput05(payload)
	case "09":
		return ConvertBLEInput09ToUSB05(payload)
	default:
		return nil, false
	}
}

func NormalizeBLEInput05(payload []byte) ([]byte, bool) {
	switch {
	case len(payload) == ns2pro.InputReportSize && payload[0] == ns2pro.InputReportID:
		return cloneReport(payload), true
	case len(payload) == ns2pro.InputReportSize-1:
		report := make([]byte, ns2pro.InputReportSize)
		report[0] = ns2pro.InputReportID
		copy(report[1:], payload)
		return report, true
	default:
		return nil, false
	}
}

const bleInput09PayloadSize = ns2pro.InputReportSize - 1

type BLEInput09Decoded struct {
	Counter   byte
	Buttons   [3]byte
	LeftX     uint16
	LeftY     uint16
	RightX    uint16
	RightY    uint16
	Home      bool
	Capture   bool
	GL        bool
	GR        bool
	C         bool
	MotionLen byte
}

func ConvertBLEInput09ToUSB05(payload []byte) ([]byte, bool) {
	payload, ok := normalizeBLEInput09Payload(payload)
	if !ok {
		return nil, false
	}

	input := ControllerInput{
		B:          payload[2]&0x01 != 0,
		A:          payload[2]&0x02 != 0,
		Y:          payload[2]&0x04 != 0,
		X:          payload[2]&0x08 != 0,
		R:          payload[2]&0x10 != 0,
		ZR:         payload[2]&0x20 != 0,
		Plus:       payload[2]&0x40 != 0,
		RightStick: payload[2]&0x80 != 0,

		Down:      payload[3]&0x01 != 0,
		Right:     payload[3]&0x02 != 0,
		Left:      payload[3]&0x04 != 0,
		Up:        payload[3]&0x08 != 0,
		L:         payload[3]&0x10 != 0,
		ZL:        payload[3]&0x20 != 0,
		Minus:     payload[3]&0x40 != 0,
		LeftStick: payload[3]&0x80 != 0,

		Home:    payload[4]&0x01 != 0,
		Capture: payload[4]&0x02 != 0,
		GR:      payload[4]&0x04 != 0,
		GL:      payload[4]&0x08 != 0,
		C:       payload[4]&0x10 != 0,
	}

	report := BuildInputReport(uint32(payload[0]), input)
	copy(report[11:14], payload[5:8])
	copy(report[14:17], payload[8:11])
	return report, true
}

func DecodeBLEInput09(payload []byte) (BLEInput09Decoded, bool) {
	payload, ok := normalizeBLEInput09Payload(payload)
	if !ok {
		return BLEInput09Decoded{}, false
	}

	var decoded BLEInput09Decoded
	decoded.Counter = payload[0]
	copy(decoded.Buttons[:], payload[2:5])
	decoded.LeftX, decoded.LeftY = unpackStick(payload[5:8])
	decoded.RightX, decoded.RightY = unpackStick(payload[8:11])
	decoded.Home = payload[4]&0x01 != 0
	decoded.Capture = payload[4]&0x02 != 0
	decoded.GR = payload[4]&0x04 != 0
	decoded.GL = payload[4]&0x08 != 0
	decoded.C = payload[4]&0x10 != 0
	decoded.MotionLen = payload[0x0e]
	return decoded, true
}

func normalizeBLEInput09Payload(payload []byte) ([]byte, bool) {
	switch {
	case len(payload) == ns2pro.InputReportSize && payload[0] == 0x09:
		return payload[1:], true
	case len(payload) == bleInput09PayloadSize:
		return payload, true
	default:
		return nil, false
	}
}

func cloneReport(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

type bleInputRawLogger struct {
	mu      sync.Mutex
	file    *os.File
	address string
	report  string
}

func newBLEInputRawLogger(path, address, report string) (*bleInputRawLogger, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("BLE input raw log open failed: %w", err)
	}
	if _, err := fmt.Fprintln(file, "time\taddress\treport\tlen\thex"); err != nil {
		_ = file.Close()
		return nil, err
	}
	fmt.Printf("Writing raw BLE input notifications to %s\n", path)
	return &bleInputRawLogger{file: file, address: address, report: report}, nil
}

func (l *bleInputRawLogger) Write(payload []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.file, "%s\t%s\t%s\t%d\t%s\n",
		time.Now().Format(time.RFC3339Nano),
		l.address,
		l.report,
		len(payload),
		hex.EncodeToString(payload),
	)
}

func (l *bleInputRawLogger) Close() error {
	return l.file.Close()
}

type bleInputDecodeLogger struct {
	mu   sync.Mutex
	file *os.File
}

func newBLEInputDecodeLogger(path string) (*bleInputDecodeLogger, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("BLE input decode log open failed: %w", err)
	}
	if _, err := fmt.Fprintln(file, "time\tcounter\tbuttons_hex\tleft_x\tleft_y\tright_x\tright_y\thome\tcapture\tgl\tgr\tc\tmotion_len"); err != nil {
		_ = file.Close()
		return nil, err
	}
	fmt.Printf("Writing decoded BLE input report 0x09 notifications to %s\n", path)
	return &bleInputDecodeLogger{file: file}, nil
}

func (l *bleInputDecodeLogger) Write(decoded BLEInput09Decoded) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.file, "%s\t%d\t%s\t%d\t%d\t%d\t%d\t%t\t%t\t%t\t%t\t%t\t%d\n",
		time.Now().Format(time.RFC3339Nano),
		decoded.Counter,
		hex.EncodeToString(decoded.Buttons[:]),
		decoded.LeftX,
		decoded.LeftY,
		decoded.RightX,
		decoded.RightY,
		decoded.Home,
		decoded.Capture,
		decoded.GL,
		decoded.GR,
		decoded.C,
		decoded.MotionLen,
	)
}

func (l *bleInputDecodeLogger) Close() error {
	return l.file.Close()
}
