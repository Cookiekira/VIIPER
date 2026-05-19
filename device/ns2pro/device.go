package ns2pro

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usbip"
)

type NS2Pro struct {
	descriptor usb.Descriptor

	inputMu     sync.RWMutex
	inputReport []byte

	bulkMu        sync.Mutex
	bulkIn        [][]byte
	bulkReplay    bool
	hidOutputFunc func([]byte)
}

type CreateOptions struct {
	BulkReplay *bool `json:"bulkReplay"`
}

// New returns a dummy Switch 2 Pro / NS2 Pro USB device.
func New(o *device.CreateOptions) (*NS2Pro, error) {
	d := &NS2Pro{
		descriptor:  MakeDescriptor(),
		inputReport: cloneBytes(neutralInputReport),
		bulkReplay:  true,
	}
	if o != nil {
		if o.IdVendor != nil {
			d.descriptor.Device.IDVendor = *o.IdVendor
		}
		if o.IdProduct != nil {
			d.descriptor.Device.IDProduct = *o.IdProduct
		}
		if o.DeviceSpecific != nil {
			data, err := json.Marshal(o.DeviceSpecific)
			if err != nil {
				return nil, fmt.Errorf("invalid JSON payload: %w", err)
			}
			var args CreateOptions
			if err := json.Unmarshal(data, &args); err != nil {
				return nil, fmt.Errorf("invalid JSON payload: %w", err)
			}
			if args.BulkReplay != nil {
				d.bulkReplay = *args.BulkReplay
			}
		}
	}
	return d, nil
}

func (d *NS2Pro) SetHIDOutputCallback(f func([]byte)) {
	d.hidOutputFunc = f
}

func (d *NS2Pro) UpdateInputReport(report []byte) bool {
	normalized, ok := NormalizeInputReport05(report)
	if !ok {
		return false
	}
	d.inputMu.Lock()
	d.inputReport = normalized
	d.inputMu.Unlock()
	return true
}

func NormalizeInputReport05(report []byte) ([]byte, bool) {
	switch {
	case len(report) == InputReportSize && report[0] == InputReportID:
		return cloneBytes(report), true
	case len(report) == InputReportSize-1:
		normalized := make([]byte, InputReportSize)
		normalized[0] = InputReportID
		copy(normalized[1:], report)
		return normalized, true
	default:
		return nil, false
	}
}

func (d *NS2Pro) HandleTransfer(ep uint32, dir uint32, out []byte) []byte {
	switch {
	case dir == usbip.DirIn && ep == 1:
		return d.currentInputReport()
	case dir == usbip.DirOut && ep == 1:
		d.handleHIDOut(out)
		return nil
	case dir == usbip.DirOut && ep == 2:
		d.handleBulkOut(out)
		return nil
	case dir == usbip.DirIn && ep == 2:
		return d.popBulkIn()
	default:
		if dir == usbip.DirOut || len(out) > 0 {
			slog.Info("ns2pro unsupported transfer",
				"ep", ep,
				"dir", dir,
				"len", len(out),
				"data", hex.EncodeToString(out),
			)
		}
		return nil
	}
}

func (d *NS2Pro) GetDescriptor() *usb.Descriptor {
	return &d.descriptor
}

func (d *NS2Pro) GetDeviceSpecificArgs() map[string]any {
	return map[string]any{"bulkReplay": d.bulkReplay}
}

func (d *NS2Pro) HandleControl(bmRequestType, bRequest uint8, wValue, wIndex, wLength uint16, data []byte) ([]byte, bool) {
	const (
		hidGetReport = 0x01
		hidGetIdle   = 0x02
		hidSetReport = 0x09
		hidSetIdle   = 0x0A
	)
	const (
		reportTypeInput  = 0x01
		reportTypeOutput = 0x02
	)

	reportType := uint8(wValue >> 8)
	reportID := uint8(wValue & 0xFF)

	slog.Info("ns2pro control request",
		"bmRequestType", bmRequestType,
		"bRequest", bRequest,
		"wValue", fmt.Sprintf("0x%04x", wValue),
		"wIndex", wIndex,
		"wLength", wLength,
		"outLen", len(data),
		"outData", hex.EncodeToString(data),
	)

	if bmRequestType == 0xA1 && bRequest == hidGetReport && reportType == reportTypeInput && reportID == InputReportID {
		return d.currentInputReport(), true
	}
	if bmRequestType == 0xA1 && bRequest == hidGetIdle {
		return []byte{0x00}, true
	}
	if bmRequestType == 0x21 && bRequest == hidSetIdle {
		return nil, true
	}
	if bmRequestType == 0x21 && bRequest == hidSetReport && reportType == reportTypeOutput && reportID == OutputReportID {
		d.handleHIDOut(data)
		return nil, true
	}

	if bRequest == msOSVendorCode && (bmRequestType&0xE0) == 0xC0 {
		if resp, ok := microsoftOSFeatureDescriptor(wIndex); ok {
			return resp, true
		}
	}

	return nil, false
}

func (d *NS2Pro) currentInputReport() []byte {
	d.inputMu.RLock()
	defer d.inputMu.RUnlock()
	return cloneBytes(d.inputReport)
}

func (d *NS2Pro) handleHIDOut(out []byte) {
	slog.Info("ns2pro HID OUT",
		"len", len(out),
		"data", hex.EncodeToString(out),
	)
	if d.hidOutputFunc != nil {
		d.hidOutputFunc(cloneBytes(out))
	}
}

func (d *NS2Pro) handleBulkOut(out []byte) {
	key := hex.EncodeToString(out)
	slog.Info("ns2pro bulk OUT",
		"len", len(out),
		"data", key,
	)
	if !d.bulkReplay {
		return
	}
	responses, ok := bulkReplayResponses[key]
	if !ok {
		slog.Info("ns2pro bulk OUT has no replay fixture", "data", key)
		return
	}

	d.bulkMu.Lock()
	defer d.bulkMu.Unlock()
	for _, response := range responses {
		d.bulkIn = append(d.bulkIn, cloneBytes(response))
	}
}

func (d *NS2Pro) popBulkIn() []byte {
	d.bulkMu.Lock()
	defer d.bulkMu.Unlock()
	if len(d.bulkIn) == 0 {
		return nil
	}
	response := d.bulkIn[0]
	copy(d.bulkIn, d.bulkIn[1:])
	d.bulkIn[len(d.bulkIn)-1] = nil
	d.bulkIn = d.bulkIn[:len(d.bulkIn)-1]
	slog.Info("ns2pro bulk IN replay",
		"len", len(response),
		"data", hex.EncodeToString(response),
	)
	return cloneBytes(response)
}
