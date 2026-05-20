package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

type BLERumbleOptions struct {
	Address           string
	NameContains      string
	Timeout           time.Duration
	WriteWithResponse bool
}

type BLERumbleClient struct {
	device            bluetooth.Device
	characteristic    bluetooth.DeviceCharacteristic
	commandChar       bluetooth.DeviceCharacteristic
	writeWithResponse bool
	mu                sync.Mutex
}

func ConnectBLERumble(ctx context.Context, options BLERumbleOptions) (*BLERumbleClient, error) {
	if options.Timeout <= 0 {
		options.Timeout = 12 * time.Second
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable BLE adapter: %w", err)
	}

	serviceUUID, err := bluetooth.ParseUUID(ns2BLEServiceUUIDString)
	if err != nil {
		return nil, err
	}
	commandUUID, err := bluetooth.ParseUUID(ns2BLECommandUUIDString)
	if err != nil {
		return nil, err
	}
	outputUUID, err := bluetooth.ParseUUID(ns2BLEOutput02UUIDString)
	if err != nil {
		return nil, err
	}

	target, err := scanForNS2Pro(ctx, adapter, options.Address, options.NameContains, options.Timeout)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connecting BLE rumble device %s (%s)\n", target.DisplayName(), target.Address.String())
	device, err := adapter.Connect(target.Address, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(options.Timeout),
	})
	if err != nil {
		return nil, fmt.Errorf("connect BLE rumble device: %w", err)
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

	characteristic, ok := discoverBLECommandCharacteristic(services[0], commandUUID)
	if !ok {
		_ = device.Disconnect()
		return nil, fmt.Errorf("command characteristic %s not found", commandUUID.String())
	}
	outputChar, ok := discoverBLEOutput02Characteristic(services[0], outputUUID)
	if !ok {
		_ = device.Disconnect()
		return nil, fmt.Errorf("output report 0x02 characteristic %s not found", outputUUID.String())
	}

	client := &BLERumbleClient{
		device:            device,
		characteristic:    outputChar,
		commandChar:       characteristic,
		writeWithResponse: options.WriteWithResponse,
	}
	if err := client.EnableBLERumble(); err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("enable BLE rumble: %w", err)
	}
	fmt.Printf("BLE output report 0x02 writes enabled on %s (%s)\n", target.DisplayName(), target.Address.String())
	return client, nil
}

func (c *BLERumbleClient) WriteBLEOutput02(payload []byte) error {
	if len(payload) != 42 || payload[0] != 0x00 {
		return fmt.Errorf("BLE output report 0x02 payload must be 42 bytes starting with 0x00, got %d bytes", len(payload))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return writeBLECommand(c.characteristic, payload, c.writeWithResponse)
}

func (c *BLERumbleClient) EnableBLERumble() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeBLECommands(c.commandChar, buildBLERumbleInitCommands(), c.writeWithResponse)
}

func (c *BLERumbleClient) Close() error {
	return c.device.Disconnect()
}
