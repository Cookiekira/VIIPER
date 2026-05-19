// Package ns2pro provides a Nintendo Switch 2 Pro / NS2 Pro USB device.
package ns2pro

import (
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usb/hid"
)

var reportDescriptor = hid.Report{
	Items: []hid.Item{
		hidItem(0x05, 0x01),
		hidItem(0x09, 0x05),
		hidItem(0xA1, 0x01),
		hidItem(0x85, 0x05),
		hidItem(0x05, 0xFF),
		hidItem(0x09, 0x01),
		hidItem(0x15, 0x00),
		hidItem(0x26, 0xFF, 0x00),
		hidItem(0x95, 0x3F),
		hidItem(0x75, 0x08),
		hidItem(0x81, 0x02),
		hidItem(0x85, 0x09),
		hidItem(0x09, 0x01),
		hidItem(0x95, 0x02),
		hidItem(0x81, 0x02),
		hidItem(0x05, 0x09),
		hidItem(0x19, 0x01),
		hidItem(0x29, 0x15),
		hidItem(0x25, 0x01),
		hidItem(0x95, 0x15),
		hidItem(0x75, 0x01),
		hidItem(0x81, 0x02),
		hidItem(0x95, 0x01),
		hidItem(0x75, 0x03),
		hidItem(0x81, 0x03),
		hidItem(0x05, 0x01),
		hidItem(0x09, 0x01),
		hidItem(0xA1, 0x00),
		hidItem(0x09, 0x30),
		hidItem(0x09, 0x31),
		hidItem(0x09, 0x33),
		hidItem(0x09, 0x35),
		hidItem(0x26, 0xFF, 0x0F),
		hidItem(0x95, 0x04),
		hidItem(0x75, 0x0C),
		hidItem(0x81, 0x02),
		hidItem(0xC0),
		hidItem(0x05, 0xFF),
		hidItem(0x09, 0x02),
		hidItem(0x26, 0xFF, 0x00),
		hidItem(0x95, 0x34),
		hidItem(0x75, 0x08),
		hidItem(0x81, 0x02),
		hidItem(0x85, 0x02),
		hidItem(0x09, 0x01),
		hidItem(0x95, 0x3F),
		hidItem(0x91, 0x02),
		hidItem(0xC0),
	},
}

func hidItem(prefix byte, data ...byte) hid.Item {
	return hid.AnyItem{
		Type: hid.ItemType((prefix >> 2) & 0x03),
		Tag:  prefix >> 4,
		Data: hid.Data(data),
	}
}

// MakeDescriptor returns the captured NS2 Pro USB descriptor shape.
func MakeDescriptor() usb.Descriptor {
	return usb.Descriptor{
		Device: usb.DeviceDescriptor{
			BcdUSB:             0x0200,
			BDeviceClass:       0xEF,
			BDeviceSubClass:    0x02,
			BDeviceProtocol:    0x01,
			BMaxPacketSize0:    0x40,
			IDVendor:           0x057E,
			IDProduct:          0x2069,
			BcdDevice:          0x0101,
			IManufacturer:      0x01,
			IProduct:           0x02,
			ISerialNumber:      0x03,
			BNumConfigurations: 0x01,
			Speed:              2,
		},
		Config: &usb.ConfigurationDescriptor{
			IConfiguration: 0x04,
			BMAttributes:   0xC0,
			BMaxPower:      0xFA,
		},
		Interfaces: []usb.InterfaceConfig{
			{
				Association: &usb.InterfaceAssociationDescriptor{
					BFirstInterface:   0x00,
					BInterfaceCount:   0x01,
					BFunctionClass:    0x03,
					BFunctionSubClass: 0x00,
					BFunctionProtocol: 0x00,
					IFunction:         0x00,
				},
				Descriptor: usb.InterfaceDescriptor{
					BInterfaceNumber:   0x00,
					BAlternateSetting:  0x00,
					BNumEndpoints:      0x02,
					BInterfaceClass:    0x03,
					BInterfaceSubClass: 0x00,
					BInterfaceProtocol: 0x00,
					IInterface:         0x05,
				},
				HID: &usb.HIDFunction{
					Descriptor: usb.HIDDescriptor{
						BcdHID:       0x0111,
						BCountryCode: 0x00,
						Descriptors: []usb.HIDSubDescriptor{
							{Type: usb.ReportDescType},
						},
					},
					Report: reportDescriptor,
				},
				Endpoints: []usb.EndpointDescriptor{
					{BEndpointAddress: 0x81, BMAttributes: 0x03, WMaxPacketSize: 0x0040, BInterval: 0x04},
					{BEndpointAddress: 0x01, BMAttributes: 0x03, WMaxPacketSize: 0x0040, BInterval: 0x04},
				},
			},
			{
				Association: &usb.InterfaceAssociationDescriptor{
					BFirstInterface:   0x01,
					BInterfaceCount:   0x01,
					BFunctionClass:    0xFF,
					BFunctionSubClass: 0x00,
					BFunctionProtocol: 0x00,
					IFunction:         0x00,
				},
				Descriptor: usb.InterfaceDescriptor{
					BInterfaceNumber:   0x01,
					BAlternateSetting:  0x00,
					BNumEndpoints:      0x02,
					BInterfaceClass:    0xFF,
					BInterfaceSubClass: 0x00,
					BInterfaceProtocol: 0x00,
					IInterface:         0x06,
				},
				Endpoints: []usb.EndpointDescriptor{
					{BEndpointAddress: 0x02, BMAttributes: 0x02, WMaxPacketSize: 0x0040, BInterval: 0x00},
					{BEndpointAddress: 0x82, BMAttributes: 0x02, WMaxPacketSize: 0x0040, BInterval: 0x00},
				},
			},
		},
		Strings: map[uint8]string{
			0: "\u0409",
			1: "Nintendo",
			2: "Pro Controller",
			3: "00",
			4: "If_Hid",
			5: "If_Hid",
			6: "If_Hid",
		},
		RawStrings: map[uint8][]byte{
			msOSStringDescriptorIndex: msOSStringDescriptor,
		},
	}
}
