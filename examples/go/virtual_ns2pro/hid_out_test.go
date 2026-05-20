package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRumbleUSB02ToBLECopiesDocumentedOutput02Payload(t *testing.T) {
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
	copy(report[1:7], []byte{0x50, 0x87, 0x89, 0x23, 0x91, 0x38})
	copy(report[17:23], []byte{0x50, 0x87, 0x89, 0x23, 0x91, 0x38})

	left, right := rumbleNonZero(report)

	require.True(t, left)
	require.True(t, right)
}

func TestRumbleNonZeroIgnoresFrequencyOnlyStopPayloads(t *testing.T) {
	report := make([]byte, 64)
	report[0] = 0x02
	copy(report[1:7], []byte{0x50, 0x87, 0x01, 0x20, 0x11, 0x00})
	copy(report[17:23], []byte{0x50, 0x87, 0x01, 0x20, 0x11, 0x00})

	left, right := rumbleNonZero(report)

	require.False(t, left)
	require.False(t, right)
}
