package main

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBLEPlayerLEDCommandMatchesCapturedUSBCommand(t *testing.T) {
	command := buildBLEPlayerLEDCommand(0x01)

	require.Equal(t, "09910107000800000100000000000000", hex.EncodeToString(command))
}

func TestBuildBLEPlayerLEDCommandUsesBitmaskPattern(t *testing.T) {
	command := buildBLEPlayerLEDCommand(0x08)

	require.Equal(t, byte(0x08), command[8])
	require.Equal(t, make([]byte, 7), command[9:16])
}

func TestBuildBLERumbleInitCommandsMatchSDLSequence(t *testing.T) {
	commands := buildBLERumbleInitCommands()

	require.Len(t, commands, 2)
	require.Equal(t, "0a9101080014000001ffffffffffffffff3500460000000000000000", hex.EncodeToString(commands[0]))
	require.Equal(t, "0191010100000000", hex.EncodeToString(commands[1]))
}

func TestBuildBLEGyroInitCommandsEnableSDLFeatureBits(t *testing.T) {
	commands := buildBLEGyroInitCommands()

	require.Len(t, commands, 2)
	require.Equal(t, "0c9101020004000027000000", hex.EncodeToString(commands[0]))
	require.Equal(t, "0c9101040004000027000000", hex.EncodeToString(commands[1]))
}
