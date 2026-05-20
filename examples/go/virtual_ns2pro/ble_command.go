package main

import (
	"fmt"

	"tinygo.org/x/bluetooth"
)

func buildBLEGenericCommand(commandID, subcommandID byte, data []byte) []byte {
	command := make([]byte, 8+len(data))
	command[0] = commandID
	command[1] = 0x91
	command[2] = 0x01
	command[3] = subcommandID
	command[5] = byte(len(data))
	copy(command[8:], data)
	return command
}

func buildBLEPlayerLEDCommand(pattern byte) []byte {
	data := make([]byte, 8)
	data[0] = pattern
	return buildBLEGenericCommand(0x09, 0x07, data)
}

func buildBLERumbleInitCommands() [][]byte {
	return [][]byte{
		{0x0a, 0x91, 0x01, 0x08, 0x00, 0x14, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x35, 0x00, 0x46, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x01, 0x91, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00},
	}
}

func buildBLEGyroInitCommands() [][]byte {
	featureBits := []byte{0x27, 0x00, 0x00, 0x00}
	return [][]byte{
		buildBLEGenericCommand(0x0c, 0x02, featureBits),
		buildBLEGenericCommand(0x0c, 0x04, featureBits),
	}
}

func writeBLECommand(characteristic bluetooth.DeviceCharacteristic, payload []byte, writeWithResponse bool) error {
	if writeWithResponse {
		_, err := characteristic.Write(payload)
		return err
	}

	if _, err := characteristic.WriteWithoutResponse(payload); err == nil {
		return nil
	} else if _, writeErr := characteristic.Write(payload); writeErr != nil {
		return fmt.Errorf("write without response: %v; write with response: %w", err, writeErr)
	}
	return nil
}

func writeBLECommands(characteristic bluetooth.DeviceCharacteristic, payloads [][]byte, writeWithResponse bool) error {
	for _, payload := range payloads {
		if err := writeBLECommand(characteristic, payload, writeWithResponse); err != nil {
			return err
		}
	}
	return nil
}
