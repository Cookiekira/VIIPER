package main

import (
	"encoding/binary"
	"testing"

	"github.com/Alia5/VIIPER/device/ns2pro"
	"github.com/stretchr/testify/require"
)

func TestConvertBLEInput09ToUSB05MapsButtonsAndSticks(t *testing.T) {
	payload := make([]byte, 63)
	payload[0] = 0x2a
	payload[2] = 0xff
	payload[3] = 0xff
	payload[4] = 0x1f
	copy(payload[5:8], []byte{0x34, 0x12, 0xab})
	copy(payload[8:11], []byte{0xcd, 0x30, 0xef})

	report, ok := ConvertBLEInput09ToUSB05(payload)

	require.True(t, ok)
	require.Len(t, report, ns2pro.InputReportSize)
	require.Equal(t, byte(ns2pro.InputReportID), report[0])
	require.Equal(t, uint32(0x2a), binary.LittleEndian.Uint32(report[1:5]))
	require.Equal(t, byte(0xcf), report[5])
	require.Equal(t, byte(0x7f), report[6])
	require.Equal(t, byte(0xcf), report[7])
	require.Equal(t, byte(0x03), report[8])
	require.Equal(t, []byte{0x34, 0x12, 0xab}, report[11:14])
	require.Equal(t, []byte{0xcd, 0x30, 0xef}, report[14:17])
}

func TestConvertBLEInput09ToUSB05OptionallyMapsCompactMotion(t *testing.T) {
	payload := make([]byte, 63)
	payload[0] = 0x2a
	payload[0x0e] = 0x28
	copy(payload[0x0f:0x1d], []byte{
		0xaa, 0xbb,
		0x01, 0x00,
		0x02, 0x00,
		0x03, 0x00,
		0x04, 0x00,
		0x05, 0x00,
		0x06, 0x00,
	})

	withoutGyro, ok := ConvertBLEInput09ToUSB05(payload)
	require.True(t, ok)
	require.Equal(t, ns2pro.NeutralInputReport()[0x2f:0x3d], withoutGyro[0x2f:0x3d])

	withGyro, ok := ConvertBLEInput09ToUSB05WithOptions(payload, BLEInputConvertOptions{EnableGyro: true})
	require.True(t, ok)
	require.Equal(t, []byte{0xaa, 0xbb}, withGyro[0x2f:0x31])
	require.Equal(t, []byte{
		0x01, 0x00,
		0x02, 0x00,
		0x03, 0x00,
		0x04, 0x00,
		0x05, 0x00,
		0x06, 0x00,
	}, withGyro[0x31:0x3d])
	require.Equal(t, uint32(0), binary.LittleEndian.Uint32(withGyro[0x2b:0x2f]))
}

func TestConvertBLEInput09ToUSB05AcceptsDebugReportIDPrefix(t *testing.T) {
	payload := make([]byte, 63)
	payload[0] = 0x07
	debug := append([]byte{0x09}, payload...)

	report, ok := ConvertBLEInput09ToUSB05(debug)

	require.True(t, ok)
	require.Equal(t, byte(ns2pro.InputReportID), report[0])
	require.Equal(t, uint32(0x07), binary.LittleEndian.Uint32(report[1:5]))
}

func TestConvertBLEInput09ToUSB05RejectsMalformedPayloads(t *testing.T) {
	shortPayload := make([]byte, 11)
	wrongPrefix := append([]byte{0x05}, make([]byte, 63)...)
	tooLong := append([]byte{0x09}, make([]byte, 64)...)

	for _, payload := range [][]byte{nil, shortPayload, wrongPrefix, tooLong} {
		report, ok := ConvertBLEInput09ToUSB05(payload)

		require.False(t, ok)
		require.Nil(t, report)
	}
}

func TestDecodeBLEInput09ExposesDebugFields(t *testing.T) {
	payload := make([]byte, 63)
	payload[0] = 0x7b
	payload[2] = 0x40
	payload[3] = 0x80
	payload[4] = 0x1f
	packStick(payload[5:8], 0x123, 0x456)
	packStick(payload[8:11], 0x789, 0xabc)
	payload[0x0e] = 0x28

	decoded, ok := DecodeBLEInput09(payload)

	require.True(t, ok)
	require.Equal(t, byte(0x7b), decoded.Counter)
	require.Equal(t, [3]byte{0x40, 0x80, 0x1f}, decoded.Buttons)
	require.Equal(t, uint16(0x123), decoded.LeftX)
	require.Equal(t, uint16(0x456), decoded.LeftY)
	require.Equal(t, uint16(0x789), decoded.RightX)
	require.Equal(t, uint16(0xabc), decoded.RightY)
	require.True(t, decoded.Home)
	require.True(t, decoded.Capture)
	require.True(t, decoded.GL)
	require.True(t, decoded.GR)
	require.True(t, decoded.C)
	require.Equal(t, byte(0x28), decoded.MotionLen)
}

func TestDecodeBLEInput09ExposesCompactMotion(t *testing.T) {
	payload := make([]byte, 63)
	payload[0x0e] = 0x28
	copy(payload[0x0f:0x1d], []byte{
		0x10, 0x20,
		0x01, 0x00,
		0xff, 0xff,
		0x00, 0x80,
		0x34, 0x12,
		0xcc, 0xed,
		0x7f, 0x7f,
	})

	decoded, ok := DecodeBLEInput09(payload)

	require.True(t, ok)
	require.True(t, decoded.HasMotion)
	require.Equal(t, "compact", decoded.MotionSource)
	require.Equal(t, [2]byte{0x10, 0x20}, decoded.MotionMetadata)
	require.Equal(t, [3]int16{1, -1, -32768}, decoded.Accel)
	require.Equal(t, [3]int16{0x1234, -0x1234, 0x7f7f}, decoded.Gyro)
}

func TestConvertBLEInput09ToUSB05FallsBackToLegacyMotionWindow(t *testing.T) {
	payload := make([]byte, 63)
	payload[0x0e] = 0x28
	copy(payload[0x0f:0x11], []byte{0xaa, 0xbb})
	copy(payload[0x30:0x3c], []byte{
		0x09, 0x00,
		0x08, 0x00,
		0x07, 0x00,
		0x06, 0x00,
		0x05, 0x00,
		0x04, 0x00,
	})

	report, ok := ConvertBLEInput09ToUSB05WithOptions(payload, BLEInputConvertOptions{EnableGyro: true})
	require.True(t, ok)
	require.Equal(t, []byte{
		0x09, 0x00,
		0x08, 0x00,
		0x07, 0x00,
		0x06, 0x00,
		0x05, 0x00,
		0x04, 0x00,
	}, report[0x31:0x3d])

	decoded, ok := DecodeBLEInput09(payload)
	require.True(t, ok)
	require.Equal(t, "legacy_0x30", decoded.MotionSource)
	require.Equal(t, [3]int16{9, 8, 7}, decoded.Accel)
	require.Equal(t, [3]int16{6, 5, 4}, decoded.Gyro)
}

func TestBLEInputClientStoresOnlyLatestFrame(t *testing.T) {
	client := &BLEInputClient{}

	client.store([]byte{0x05, 0x01})
	client.store([]byte{0x05, 0x02})

	latest := client.LatestInputReport()
	require.Equal(t, []byte{0x05, 0x02}, latest)

	latest[1] = 0xff
	require.Equal(t, []byte{0x05, 0x02}, client.LatestInputReport())
}

func TestBLEInputClientAddsSyntheticMotionTimestamp(t *testing.T) {
	client := &BLEInputClient{}
	report := ns2pro.NeutralInputReport()
	report[0x31] = 0x01

	client.storeFrame(report, true)

	latest := client.LatestInputReport()
	require.NotZero(t, binary.LittleEndian.Uint32(latest[0x2b:0x2f]))
	require.Equal(t, byte(0x01), latest[0x31])
	require.Equal(t, uint32(0), binary.LittleEndian.Uint32(client.latest[0x2b:0x2f]))
}
