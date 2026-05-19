package ns2pro

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

const (
	msOSStringDescriptorIndex = 0xEE
	msOSVendorCode            = 0x20

	msOSExtendedCompatIDIndex    = 0x0004
	msOSExtendedPropertiesIndex  = 0x0005
	msOSPropertyDataTypeMultiSZ  = 0x00000007
	deviceInterfaceGUIDProperty  = "DeviceInterfaceGUIDs"
	deviceInterfaceGUIDForWinUSB = "{057E2069-0001-0101-8000-00805F9B34FB}"
)

var msOSStringDescriptor = []byte{
	0x12, 0x03,
	'M', 0x00,
	'S', 0x00,
	'F', 0x00,
	'T', 0x00,
	'1', 0x00,
	'0', 0x00,
	'0', 0x00,
	msOSVendorCode, 0x00,
}

func microsoftOSFeatureDescriptor(index uint16) ([]byte, bool) {
	switch index {
	case msOSExtendedCompatIDIndex:
		return msOSCompatibleIDDescriptor(), true
	case msOSExtendedPropertiesIndex:
		return msOSExtendedPropertiesDescriptor(), true
	default:
		return nil, false
	}
}

func msOSCompatibleIDDescriptor() []byte {
	const headerLen = 16
	const functionLen = 24

	out := make([]byte, headerLen+functionLen)
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint16(out[4:6], 0x0100)
	binary.LittleEndian.PutUint16(out[6:8], msOSExtendedCompatIDIndex)
	out[8] = 0x01

	function := out[headerLen:]
	function[0] = 0x01 // MI_01, vendor/config bulk interface.
	function[1] = 0x01
	copy(function[2:10], []byte("WINUSB\x00\x00"))
	return out
}

func msOSExtendedPropertiesDescriptor() []byte {
	propertyName := utf16Bytes(deviceInterfaceGUIDProperty + "\x00")
	propertyData := utf16Bytes(deviceInterfaceGUIDForWinUSB + "\x00\x00")
	propertySize := 4 + 4 + 2 + len(propertyName) + 4 + len(propertyData)
	totalSize := 10 + propertySize

	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(totalSize))
	_ = binary.Write(&b, binary.LittleEndian, uint16(0x0100))
	_ = binary.Write(&b, binary.LittleEndian, uint16(msOSExtendedPropertiesIndex))
	_ = binary.Write(&b, binary.LittleEndian, uint16(0x0001))

	_ = binary.Write(&b, binary.LittleEndian, uint32(propertySize))
	_ = binary.Write(&b, binary.LittleEndian, uint32(msOSPropertyDataTypeMultiSZ))
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(propertyName)))
	b.Write(propertyName)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(propertyData)))
	b.Write(propertyData)

	return b.Bytes()
}

func utf16Bytes(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(encoded))
	for i, r := range encoded {
		binary.LittleEndian.PutUint16(out[i*2:], r)
	}
	return out
}
