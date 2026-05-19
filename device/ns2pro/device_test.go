package ns2pro

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/Alia5/VIIPER/usbip"
	"github.com/stretchr/testify/require"
)

func TestInterruptInReturnsNeutralInputReport(t *testing.T) {
	dev, err := New(nil)
	require.NoError(t, err)

	report := dev.HandleTransfer(1, usbip.DirIn, nil)
	require.Len(t, report, InputReportSize)
	require.Equal(t, byte(InputReportID), report[0])
	require.Equal(t, neutralInputReport, report)
}

func TestUpdateInputReportAcceptsPayloadWithoutReportID(t *testing.T) {
	dev, err := New(nil)
	require.NoError(t, err)

	payload := make([]byte, InputReportSize-1)
	payload[0] = 0xAA
	require.True(t, dev.UpdateInputReport(payload))

	report := dev.HandleTransfer(1, usbip.DirIn, nil)
	require.Len(t, report, InputReportSize)
	require.Equal(t, byte(InputReportID), report[0])
	require.Equal(t, byte(0xAA), report[1])
}

func TestBulkReplayQueuesCapturedResponses(t *testing.T) {
	dev, err := New(nil)
	require.NoError(t, err)

	dev.HandleTransfer(2, usbip.DirOut, mustHex("0791000100000000"))

	resp := dev.HandleTransfer(2, usbip.DirIn, nil)
	require.Equal(t, mustHex("0701000100f8000000"), resp)

	require.Nil(t, dev.HandleTransfer(2, usbip.DirIn, nil))
}

func TestMicrosoftOSCompatibleIDDescriptor(t *testing.T) {
	dev, err := New(nil)
	require.NoError(t, err)

	resp, handled := dev.HandleControl(0xC0, msOSVendorCode, 0, msOSExtendedCompatIDIndex, 0x28, nil)
	require.True(t, handled)
	require.Len(t, resp, 40)
	require.Equal(t, uint32(40), binary.LittleEndian.Uint32(resp[0:4]))
	require.Equal(t, uint16(0x0100), binary.LittleEndian.Uint16(resp[4:6]))
	require.Equal(t, uint16(msOSExtendedCompatIDIndex), binary.LittleEndian.Uint16(resp[6:8]))
	require.Equal(t, byte(0x01), resp[8])
	require.Equal(t, byte(0x01), resp[16])
	require.Equal(t, []byte("WINUSB\x00\x00"), resp[18:26])
}

func TestMicrosoftOSExtendedPropertiesDescriptor(t *testing.T) {
	dev, err := New(nil)
	require.NoError(t, err)

	resp, handled := dev.HandleControl(0xC1, msOSVendorCode, 0, msOSExtendedPropertiesIndex, 0xFFFF, nil)
	require.True(t, handled)
	require.Equal(t, uint32(len(resp)), binary.LittleEndian.Uint32(resp[0:4]))
	require.Equal(t, uint16(0x0100), binary.LittleEndian.Uint16(resp[4:6]))
	require.Equal(t, uint16(msOSExtendedPropertiesIndex), binary.LittleEndian.Uint16(resp[6:8]))
	require.Equal(t, uint16(1), binary.LittleEndian.Uint16(resp[8:10]))
	require.True(t, bytes.Contains(resp, utf16Bytes(deviceInterfaceGUIDProperty+"\x00")))
	require.True(t, bytes.Contains(resp, utf16Bytes(deviceInterfaceGUIDForWinUSB+"\x00\x00")))
}
