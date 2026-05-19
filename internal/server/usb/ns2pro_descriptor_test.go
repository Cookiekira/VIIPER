package usb

import (
	"encoding/hex"
	"log/slog"
	"testing"

	pusb "github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usb/hid"
	"github.com/stretchr/testify/require"
)

func TestNS2ProConfigDescriptorMatchesCapture(t *testing.T) {
	server := New(ServerConfig{}, slog.Default(), nil)
	desc := NS2ProDescriptorForConfigTest()
	got := server.buildConfigDescriptor(&desc)

	require.Equal(t,
		mustHexTest(t, "09025000020104c0fa080b0001030000000904000002030000050921110100012261000705810340000407050103400004080b0101ff0000000904010002ff0000060705020240000007058202400000"),
		got,
	)
}

func mustHexTest(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(s)
	require.NoError(t, err)
	return out
}

func NS2ProDescriptorForConfigTest() pusb.Descriptor {
	return pusb.Descriptor{
		Config: &pusb.ConfigurationDescriptor{
			IConfiguration: 0x04,
			BMAttributes:   0xC0,
			BMaxPower:      0xFA,
		},
		Interfaces: []pusb.InterfaceConfig{
			{
				Association: &pusb.InterfaceAssociationDescriptor{
					BFirstInterface:   0x00,
					BInterfaceCount:   0x01,
					BFunctionClass:    0x03,
					BFunctionSubClass: 0x00,
					BFunctionProtocol: 0x00,
					IFunction:         0x00,
				},
				Descriptor: pusb.InterfaceDescriptor{
					BInterfaceNumber:   0x00,
					BAlternateSetting:  0x00,
					BNumEndpoints:      0x02,
					BInterfaceClass:    0x03,
					BInterfaceSubClass: 0x00,
					BInterfaceProtocol: 0x00,
					IInterface:         0x05,
				},
				HID: &pusb.HIDFunction{
					Descriptor: pusb.HIDDescriptor{
						BcdHID:       0x0111,
						BCountryCode: 0x00,
						Descriptors: []pusb.HIDSubDescriptor{
							{Type: pusb.ReportDescType},
						},
					},
					Report: hid.Report{
						Items: []hid.Item{
							hid.LongItem{Tag: 0x00, Data: make(hid.Data, 94)},
						},
					},
				},
				Endpoints: []pusb.EndpointDescriptor{
					{BEndpointAddress: 0x81, BMAttributes: 0x03, WMaxPacketSize: 0x0040, BInterval: 0x04},
					{BEndpointAddress: 0x01, BMAttributes: 0x03, WMaxPacketSize: 0x0040, BInterval: 0x04},
				},
			},
			{
				Association: &pusb.InterfaceAssociationDescriptor{
					BFirstInterface:   0x01,
					BInterfaceCount:   0x01,
					BFunctionClass:    0xFF,
					BFunctionSubClass: 0x00,
					BFunctionProtocol: 0x00,
					IFunction:         0x00,
				},
				Descriptor: pusb.InterfaceDescriptor{
					BInterfaceNumber:   0x01,
					BAlternateSetting:  0x00,
					BNumEndpoints:      0x02,
					BInterfaceClass:    0xFF,
					BInterfaceSubClass: 0x00,
					BInterfaceProtocol: 0x00,
					IInterface:         0x06,
				},
				Endpoints: []pusb.EndpointDescriptor{
					{BEndpointAddress: 0x02, BMAttributes: 0x02, WMaxPacketSize: 0x0040, BInterval: 0x00},
					{BEndpointAddress: 0x82, BMAttributes: 0x02, WMaxPacketSize: 0x0040, BInterval: 0x00},
				},
			},
		},
	}
}
