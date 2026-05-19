package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRumbleUSB02ToBLECopiesLeftAndRightLRA(t *testing.T) {
	report := make([]byte, 64)
	report[0] = 0x02
	for i := 1; i <= 32; i++ {
		report[i] = byte(i)
	}

	ble, ok := RumbleUSB02ToBLE(report)

	require.True(t, ok)
	require.Len(t, ble, 42)
	require.Equal(t, byte(0x00), ble[0])
	require.Equal(t, report[1:17], ble[1:17])
	require.Equal(t, report[17:33], ble[17:33])
	require.Equal(t, make([]byte, 9), ble[33:42])
}

func TestRumbleUSB02ToBLERejectsNonRumbleReport(t *testing.T) {
	_, ok := RumbleUSB02ToBLE([]byte{0x05})

	require.False(t, ok)
}

func TestRumbleNonZeroDetectsLeftAndRightPayloads(t *testing.T) {
	report := make([]byte, 64)
	report[0] = 0x02
	report[1] = 0x01
	report[17] = 0x02

	left, right := rumbleNonZero(report)

	require.True(t, left)
	require.True(t, right)
}
