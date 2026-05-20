// Package ns2pro provides a Nintendo Switch 2 Pro Controller compatible HID device.
package ns2pro

import (
	"encoding/binary"
	"sync"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usb/hid"
	"github.com/Alia5/VIIPER/usbip"
)

type NS2Pro struct {
	stateMu    sync.Mutex
	inputState *InputState
	outputFunc func(OutputState)
	descriptor usb.Descriptor

	batteryVolts uint16

	protoMu           sync.Mutex
	activeReportID    uint8
	featureMask       uint8
	featureFlags      uint8
	usbReportsEnabled bool
	reportCounter32   uint32
	reportCounter8    uint8
	motionTimestamp   uint32
	bulkInQueue       [][]byte
}

func New(o *device.CreateOptions) (*NS2Pro, error) {
	d := &NS2Pro{
		inputState:     defaultInputState(),
		descriptor:     MakeDescriptor(),
		activeReportID: ReportIDPro,
		featureFlags:   FeatureButtons | FeatureSticks,
		batteryVolts:   BatteryVolts,
	}
	if o != nil {
		if o.IdVendor != nil {
			d.descriptor.Device.IDVendor = *o.IdVendor
		}
		if o.IdProduct != nil {
			d.descriptor.Device.IDProduct = *o.IdProduct
		}
	}
	return d, nil
}

func (d *NS2Pro) SetOutputCallback(f func(OutputState)) {
	d.outputFunc = f
}

func (d *NS2Pro) UpdateInputState(state InputState) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.inputState = &state
}

func (d *NS2Pro) HandleTransfer(ep uint32, dir uint32, out []byte) []byte {
	switch {
	case dir == usbip.DirIn && ep == 1:
		return d.nextInputReport()
	case dir == usbip.DirIn && ep == 2:
		return d.popBulkIn()
	case dir == usbip.DirOut && ep == 1:
		d.handleOutputReport(out)
	case dir == usbip.DirOut && ep == 2:
		d.handleBulkOut(out)
	}
	return nil
}

func (d *NS2Pro) HandleControl(bmRequestType, bRequest uint8, wValue, wIndex uint16, _ uint16, data []byte) ([]byte, bool) {
	const (
		hidGetReport = 0x01
		hidSetReport = 0x09
	)
	const (
		reportTypeInput  = 0x01
		reportTypeOutput = 0x02
	)

	reportType := uint8(wValue >> 8)
	reportID := uint8(wValue)

	if bmRequestType == 0xA1 && bRequest == hidGetReport && reportType == reportTypeInput {
		switch reportID {
		case ReportIDCommon, ReportIDPro, 0:
			return d.inputReportForID(reportID), true
		}
	}

	if bmRequestType == 0x21 && bRequest == hidSetReport && reportType == reportTypeOutput && reportID == ReportIDOutput {
		d.handleOutputReport(data)
		return nil, true
	}

	return nil, false
}

func (d *NS2Pro) GetDescriptor() *usb.Descriptor {
	return &d.descriptor
}

func (d *NS2Pro) GetDeviceSpecificArgs() map[string]any {
	return nil
}

func (d *NS2Pro) nextInputReport() []byte {
	d.protoMu.Lock()
	reportID := d.activeReportID
	d.protoMu.Unlock()
	return d.inputReportForID(reportID)
}

func (d *NS2Pro) inputReportForID(reportID uint8) []byte {
	d.stateMu.Lock()
	st := *d.inputState
	d.stateMu.Unlock()

	d.protoMu.Lock()
	if reportID == 0 {
		reportID = d.activeReportID
	}
	features := d.featureFlags
	var report []byte
	switch reportID {
	case ReportIDCommon:
		d.reportCounter32++
		if features&FeatureIMU != 0 {
			d.motionTimestamp += 4000
		}
		report = st.buildCommonReport(d.reportCounter32, d.motionTimestamp, features, d.batteryVolts)
	default:
		d.reportCounter8++
		report = st.buildProReport(d.reportCounter8, features)
	}
	d.protoMu.Unlock()
	return report
}

func (d *NS2Pro) handleOutputReport(out []byte) {
	if len(out) == 0 {
		return
	}

	payload := out
	if out[0] == ReportIDOutput {
		payload = out[1:]
	} else if len(out) != OutputWireSize {
		return
	}
	if len(payload) < OutputWireSize {
		return
	}

	feedback := OutputState{}
	copy(feedback.LeftRumble[:], payload[0:16])
	copy(feedback.RightRumble[:], payload[16:32])
	if d.outputFunc != nil {
		d.outputFunc(feedback)
	}
}

func (d *NS2Pro) handleBulkOut(out []byte) {
	if len(out) < 8 {
		return
	}
	cmd := out[0]
	seq := out[2]
	sub := out[3]

	switch cmd {
	case 0x02:
		d.handleFlashCommand(seq, sub, out)
	case 0x03:
		d.handleUSBCommand(seq, sub, out)
	case 0x0C:
		d.handleFeatureCommand(seq, sub, out)
	case 0x09:
		d.enqueueResponse(commandHeader(cmd, seq, sub))
	default:
		d.enqueueResponse(commandHeader(cmd, seq, sub))
	}
}

func (d *NS2Pro) handleFlashCommand(seq, sub uint8, out []byte) {
	if sub != 0x01 || len(out) < 16 {
		d.enqueueResponse(commandHeader(0x02, seq, sub))
		return
	}

	address := binary.LittleEndian.Uint32(out[12:16])
	resp := make([]byte, 0x50)
	copy(resp[0:8], commandHeader(0x02, seq, sub))
	resp[8] = 0x40
	binary.LittleEndian.PutUint32(resp[12:16], address)
	copy(resp[16:], d.flashBlock(address))
	d.enqueueResponse(resp)
}

func (d *NS2Pro) handleUSBCommand(seq, sub uint8, out []byte) {
	switch sub {
	case 0x03:
		if len(out) >= 9 {
			d.usbReportsEnabled = out[8] != 0
		}
		d.enqueueResponse(append(commandHeader(0x03, seq, sub), 0x01, 0x00, 0x00, 0x00))
	case 0x0A:
		if len(out) >= 9 {
			switch out[8] {
			case ReportIDCommon, ReportIDPro:
				d.activeReportID = out[8]
			}
		}
		d.enqueueResponse(commandHeader(0x03, seq, sub))
	case 0x0D:
		d.usbReportsEnabled = true
		d.enqueueResponse(append(commandHeader(0x03, seq, sub), 0x01, 0x00, 0x00, 0x00))
	default:
		d.enqueueResponse(commandHeader(0x03, seq, sub))
	}
}

func (d *NS2Pro) handleFeatureCommand(seq, sub uint8, out []byte) {
	flags := uint8(0)
	if len(out) >= 9 {
		flags = out[8]
	}

	switch sub {
	case 0x01:
		payload := make([]byte, 12)
		copy(payload[4:], featureInfo(flags))
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), payload...))
	case 0x02:
		d.featureMask = flags
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), 0x00, 0x00, 0x00, 0x00))
	case 0x03:
		d.featureMask = 0
		d.featureFlags = 0
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), 0x00, 0x00, 0x00, 0x00))
	case 0x04:
		d.featureFlags |= d.maskedFeatures(flags)
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), 0x00, 0x00, 0x00, 0x00))
	case 0x05:
		d.featureFlags &^= d.maskedFeatures(flags)
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), 0x00, 0x00, 0x00, 0x00))
	default:
		d.enqueueResponse(append(commandHeader(0x0C, seq, sub), 0x00, 0x00, 0x00, 0x00))
	}
}

func (d *NS2Pro) maskedFeatures(flags uint8) uint8 {
	return d.maskedFeaturesLocked(flags)
}

func (d *NS2Pro) maskedFeaturesLocked(flags uint8) uint8 {
	if d.featureMask == 0 {
		return flags
	}
	return flags & d.featureMask
}

func (d *NS2Pro) enqueueResponse(resp []byte) {
	d.protoMu.Lock()
	defer d.protoMu.Unlock()
	for len(resp) > 64 {
		chunk := append([]byte(nil), resp[:64]...)
		d.bulkInQueue = append(d.bulkInQueue, chunk)
		resp = resp[64:]
	}
	d.bulkInQueue = append(d.bulkInQueue, append([]byte(nil), resp...))
}

func (d *NS2Pro) popBulkIn() []byte {
	d.protoMu.Lock()
	defer d.protoMu.Unlock()
	if len(d.bulkInQueue) == 0 {
		return nil
	}
	chunk := d.bulkInQueue[0]
	d.bulkInQueue = d.bulkInQueue[1:]
	return append([]byte(nil), chunk...)
}

func commandHeader(cmd, seq, sub uint8) []byte {
	return []byte{cmd, 0x01, seq, sub, 0x10, 0x78, 0x00, 0x00}
}

func featureInfo(flags uint8) []byte {
	out := make([]byte, 8)
	if flags&FeatureButtons != 0 {
		out[0] = 0x07
	}
	if flags&FeatureSticks != 0 {
		out[1] = 0x07
	}
	if flags&FeatureIMU != 0 {
		out[2] = 0x01
	}
	if flags&FeatureMouse != 0 {
		out[4] = 0x03
	}
	if flags&FeatureRumble != 0 {
		out[5] = 0x03
	}
	return out
}

func (d *NS2Pro) flashBlock(address uint32) []byte {
	return minimalFlashBlock(address)
}

func minimalFlashBlock(address uint32) []byte {
	block := make([]byte, 0x40)
	switch address {
	case 0x13000:
		copy(block[2:], []byte("VIIPER-NS2PRO-00"))
	case 0x13080, 0x130C0:
		encodeStickCalibration(block[0x28:], StickCenter, StickCenter, 2047, 2047, 2048, 2048)
	case 0x13040, 0x13100, 0x1FC040, 0x1FC080:
		// Zeroed data is intentional: no gyro/accel bias and no user calibration magic.
	default:
	}
	return block
}

func encodeStickCalibration(out []byte, neutralX, neutralY, maxX, maxY, minX, minY uint16) {
	if len(out) < 9 {
		return
	}
	packStick12(out[0:3], neutralX, neutralY)
	packStick12(out[3:6], maxX, maxY)
	packStick12(out[6:9], minX, minY)
}

func MakeDescriptor() usb.Descriptor {
	return usb.Descriptor{
		Device: usb.DeviceDescriptor{
			BcdUSB:             0x0200,
			BDeviceClass:       0xEF,
			BDeviceSubClass:    0x02,
			BDeviceProtocol:    0x01,
			BMaxPacketSize0:    0x40,
			IDVendor:           DefaultVID,
			IDProduct:          DefaultPID,
			BcdDevice:          0x0200,
			IManufacturer:      0x01,
			IProduct:           0x02,
			ISerialNumber:      0x03,
			BNumConfigurations: 0x01,
			Speed:              2,
		},
		Interfaces: []usb.InterfaceConfig{
			{
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
					{BEndpointAddress: EndpointHIDIn, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
					{BEndpointAddress: EndpointHIDOut, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
				},
			},
			{
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
					{BEndpointAddress: EndpointBulkOut, BMAttributes: 0x02, WMaxPacketSize: 64, BInterval: 0},
					{BEndpointAddress: EndpointBulkIn, BMAttributes: 0x02, WMaxPacketSize: 64, BInterval: 0},
				},
			},
		},
		Strings: map[uint8]string{
			0: "\u0409",
			1: "Nintendo",
			2: "Switch 2 Pro Controller",
			3: DefaultSerial,
			5: "Nintendo Switch 2 Pro Controller",
			6: "Vendor Interface",
		},
	}
}

var reportDescriptor = hid.Report{Items: []hid.Item{
	hidShort(hid.ItemTypeGlobal, 0x0, 0x01),
	hidShort(hid.ItemTypeLocal, 0x0, 0x05),
	hidShort(hid.ItemTypeMain, 0xA, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x8, ReportIDCommon),
	hidShort(hid.ItemTypeGlobal, 0x0, 0xFF),
	hidShort(hid.ItemTypeLocal, 0x0, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x1, 0x00),
	hidShort(hid.ItemTypeGlobal, 0x2, 0xFF, 0x00),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x3F),
	hidShort(hid.ItemTypeGlobal, 0x7, 0x08),
	hidShort(hid.ItemTypeMain, 0x8, 0x02),
	hidShort(hid.ItemTypeGlobal, 0x8, ReportIDPro),
	hidShort(hid.ItemTypeLocal, 0x0, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x02),
	hidShort(hid.ItemTypeMain, 0x8, 0x02),
	hidShort(hid.ItemTypeGlobal, 0x0, 0x09),
	hidShort(hid.ItemTypeLocal, 0x1, 0x01),
	hidShort(hid.ItemTypeLocal, 0x2, 0x15),
	hidShort(hid.ItemTypeGlobal, 0x2, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x15),
	hidShort(hid.ItemTypeGlobal, 0x7, 0x01),
	hidShort(hid.ItemTypeMain, 0x8, 0x02),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x7, 0x03),
	hidShort(hid.ItemTypeMain, 0x8, 0x03),
	hidShort(hid.ItemTypeGlobal, 0x0, 0x01),
	hidShort(hid.ItemTypeLocal, 0x0, 0x01),
	hidShort(hid.ItemTypeMain, 0xA, 0x00),
	hidShort(hid.ItemTypeLocal, 0x0, 0x30),
	hidShort(hid.ItemTypeLocal, 0x0, 0x31),
	hidShort(hid.ItemTypeLocal, 0x0, 0x33),
	hidShort(hid.ItemTypeLocal, 0x0, 0x35),
	hidShort(hid.ItemTypeGlobal, 0x2, 0xFF, 0x0F),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x04),
	hidShort(hid.ItemTypeGlobal, 0x7, 0x0C),
	hidShort(hid.ItemTypeMain, 0x8, 0x02),
	hidShort(hid.ItemTypeMain, 0xC),
	hidShort(hid.ItemTypeGlobal, 0x0, 0xFF),
	hidShort(hid.ItemTypeLocal, 0x0, 0x02),
	hidShort(hid.ItemTypeGlobal, 0x2, 0xFF, 0x00),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x34),
	hidShort(hid.ItemTypeGlobal, 0x7, 0x08),
	hidShort(hid.ItemTypeMain, 0x8, 0x02),
	hidShort(hid.ItemTypeGlobal, 0x8, ReportIDOutput),
	hidShort(hid.ItemTypeLocal, 0x0, 0x01),
	hidShort(hid.ItemTypeGlobal, 0x9, 0x3F),
	hidShort(hid.ItemTypeMain, 0x9, 0x02),
	hidShort(hid.ItemTypeMain, 0xC),
}}

func hidShort(itemType hid.ItemType, tag uint8, data ...uint8) hid.AnyItem {
	return hid.AnyItem{Type: itemType, Tag: tag, Data: hid.Data(data)}
}
