package main

import (
	"context"
	"fmt"
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
	Address      string
	NameContains string
	Timeout      time.Duration
	Report       string
}

type BLEInputClient struct {
	device bluetooth.Device

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
		report = "05"
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

	result, err := scanForNS2Pro(ctx, adapter, options.Address, options.NameContains, options.Timeout)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connecting BLE input device %s (%s)\n", result.LocalName(), result.Address.String())
	device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{
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

	client := &BLEInputClient{device: device}
	if err := chars[0].EnableNotifications(func(payload []byte) {
		report, ok := normalizeBLEInputReport(report, payload)
		if !ok {
			return
		}
		client.store(report)
	}); err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("enable BLE input notifications: %w", err)
	}

	fmt.Printf("BLE input report 0x%s notifications enabled on %s (%s)\n", report, result.LocalName(), result.Address.String())
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
	return c.device.Disconnect()
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

func inputCharacteristicUUID(report string) (bluetooth.UUID, error) {
	switch report {
	case "05", "5":
		return bluetooth.ParseUUID(ns2BLEInput05UUIDString)
	case "09", "9":
		return bluetooth.ParseUUID(ns2BLEInput09UUIDString)
	default:
		return bluetooth.UUID{}, fmt.Errorf("unsupported BLE input report %q; use 05 or 09", report)
	}
}

func normalizeBLEInputReport(report string, payload []byte) ([]byte, bool) {
	switch report {
	case "05", "5":
		return NormalizeBLEInput05(payload)
	case "09", "9":
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

func ConvertBLEInput09ToUSB05(payload []byte) ([]byte, bool) {
	if len(payload) == 64 && payload[0] == 0x09 {
		payload = payload[1:]
	}
	if len(payload) < 11 {
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

func cloneReport(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
