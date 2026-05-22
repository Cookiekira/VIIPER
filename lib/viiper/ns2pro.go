package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef uintptr_t USBServerHandle;
typedef uintptr_t NS2ProDeviceHandle;

typedef struct {
	uint32_t Buttons;
	uint16_t LX;
	uint16_t LY;
	uint16_t RX;
	uint16_t RY;
	int16_t AccelX;
	int16_t AccelY;
	int16_t AccelZ;
	int16_t GyroX;
	int16_t GyroY;
	int16_t GyroZ;
	uint8_t BatteryLevel;
	uint8_t Charging;
	uint8_t ExternalPower;
} NS2ProDeviceState;

typedef void (*NS2ProOutputCallback)(NS2ProDeviceHandle handle, const uint8_t* leftRumble, const uint8_t* rightRumble, uint8_t flags, uint8_t playerLedMask);

static void viiper_call_ns2pro_output(NS2ProOutputCallback fn, NS2ProDeviceHandle handle, const uint8_t* leftRumble, const uint8_t* rightRumble, uint8_t flags, uint8_t playerLedMask) {
	fn(handle, leftRumble, rightRumble, flags, playerLedMask);
}
*/
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/cgo"
	"slices"
	"unsafe"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/device/ns2pro"
	"github.com/Alia5/VIIPER/internal/server/api"
)

// CreateNS2ProDevice creates a new Switch 2 Pro Controller device on the bus with the given ID.
// @param serverHandle Handle to the USB server.
// @param outDeviceHandle Output parameter for the created device handle.
// @param busID ID of the bus to add the device to.
// @param autoAttachLocalhost If true, the device will be automatically attached to usbip-win2 on this machine.
// @param idVendor Optional USB vendor ID (0 = default).
// @param idProduct Optional USB product ID (0 = default).
//
//export CreateNS2ProDevice
func CreateNS2ProDevice(
	serverHandle C.USBServerHandle,
	outDeviceHandle *C.NS2ProDeviceHandle,
	busID uint32,
	autoAttachLocalhost bool,
	idVendor uint16,
	idProduct uint16,
) bool {
	sh := cgo.Handle(serverHandle)
	shw, ok := sh.Value().(*usbServerHandleWrapper)
	if !ok {
		return false
	}
	bus := shw.s.GetBus(busID)
	if bus == nil {
		return false
	}

	opts := &device.CreateOptions{}
	if idVendor != 0 {
		opts.IdVendor = &idVendor
	}
	if idProduct != 0 {
		opts.IdProduct = &idProduct
	}

	d, err := ns2pro.New(opts)
	if err != nil {
		return false
	}
	devCtx, err := bus.Add(d)
	if err != nil {
		return false
	}
	exportMeta := device.GetDeviceMeta(devCtx)
	if exportMeta == nil {
		return false
	}

	if autoAttachLocalhost {
		err := api.AttachLocalhostClient(
			context.Background(),
			exportMeta,
			shw.s.GetListenPort(),
			true,
			slog.Default(),
		)
		if err != nil {
			slog.Error("failed to auto-attach localhost client", "error", err)
			return false
		}
	}

	handleWrapper := &deviceHandleWrapper{
		device:     d,
		exportMeta: exportMeta,
		usbServer:  shw,
	}
	*outDeviceHandle = C.NS2ProDeviceHandle(cgo.NewHandle(handleWrapper))

	shw.mtx.Lock()
	defer shw.mtx.Unlock()
	shw.deviceHandles[busID] = append(shw.deviceHandles[busID], deviceHandle(*outDeviceHandle))
	return true
}

// SetNS2ProDeviceState updates the input state of the Switch 2 Pro Controller device associated with the given handle.
// @param handle Handle to the NS2Pro device.
// @param state New input state to set on the device.
//
//export SetNS2ProDeviceState
func SetNS2ProDeviceState(handle C.NS2ProDeviceHandle, state C.NS2ProDeviceState) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	dev, ok := dhw.device.(*ns2pro.NS2Pro)
	if !ok {
		return false
	}
	dev.UpdateInputState(ns2pro.InputState{
		Buttons:       uint32(state.Buttons),
		LX:            uint16(state.LX),
		LY:            uint16(state.LY),
		RX:            uint16(state.RX),
		RY:            uint16(state.RY),
		AccelX:        int16(state.AccelX),
		AccelY:        int16(state.AccelY),
		AccelZ:        int16(state.AccelZ),
		GyroX:         int16(state.GyroX),
		GyroY:         int16(state.GyroY),
		GyroZ:         int16(state.GyroZ),
		BatteryLevel:  uint8(state.BatteryLevel),
		Charging:      state.Charging != 0,
		ExternalPower: state.ExternalPower != 0,
	})
	return true
}

// SetNS2ProOutputCallback sets a callback invoked when the host sends rumble/LED output to the device.
// @param handle Handle to the NS2Pro device.
// @param callback Callback receiving left/right 16-byte rumble packets, flags, and player LED mask. Pass NULL to clear.
//
//export SetNS2ProOutputCallback
func SetNS2ProOutputCallback(handle C.NS2ProDeviceHandle, cb C.NS2ProOutputCallback) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	dev, ok := dhw.device.(*ns2pro.NS2Pro)
	if !ok {
		return false
	}
	if cb == nil {
		dev.SetOutputCallback(nil)
		return true
	}
	dev.SetOutputCallback(func(out ns2pro.OutputState) {
		C.viiper_call_ns2pro_output(
			cb,
			handle,
			(*C.uint8_t)(unsafe.Pointer(&out.LeftRumble[0])),
			(*C.uint8_t)(unsafe.Pointer(&out.RightRumble[0])),
			C.uint8_t(out.Flags),
			C.uint8_t(out.PlayerLedMask),
		)
	})
	return true
}

// RemoveNS2ProDevice removes the Switch 2 Pro Controller device associated with the given handle from the server.
// @param handle Handle to the NS2Pro device to remove.
//
//export RemoveNS2ProDevice
func RemoveNS2ProDevice(handle C.NS2ProDeviceHandle) bool {
	dh := cgo.Handle(handle)
	dhw, ok := dh.Value().(*deviceHandleWrapper)
	if !ok {
		return false
	}
	if err := dhw.usbServer.s.RemoveDeviceByID(dhw.exportMeta.BusId, fmt.Sprintf("%d", dhw.exportMeta.DevId)); err != nil {
		return false
	}

	shw := dhw.usbServer
	busID := dhw.exportMeta.BusId

	shw.mtx.Lock()
	defer shw.mtx.Unlock()
	shw.deviceHandles[busID] = slices.DeleteFunc(shw.deviceHandles[busID], func(h deviceHandle) bool {
		return h == deviceHandle(handle)
	})
	dh.Delete()

	return true
}
