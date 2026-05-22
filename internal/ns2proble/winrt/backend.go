//go:build windows

package winrt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/Alia5/VIIPER/internal/ns2proble"
	"github.com/go-ole/go-ole"
	rt "github.com/saltosystems/winrt-go"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth/advertisement"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/saltosystems/winrt-go/windows/foundation"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
)

const (
	initUUID            = "{00C5AF5D-1964-4E30-8F51-1956F96BD282}"
	inputUUID           = "{AB7DE9BE-89FE-49AD-828F-118F09DF7FD2}"
	vibrationUUID       = "{CC483F51-9258-427D-A939-630C31F72B05}"
	commandUUID         = "{649D4AC9-8EB7-4E6C-AF44-1EA54FE5F005}"
	commandResponseUUID = "{C765A961-D9D8-4D36-A20A-5315B111836A}"
)

type Backend struct {
	initOnce sync.Once
	initErr  error
}

func New() *Backend { return &Backend{} }

func (b *Backend) Scan(ctx context.Context) (ns2proble.Device, error) {
	if err := b.init(); err != nil {
		return ns2proble.Device{}, err
	}
	watcher, err := advertisement.NewBluetoothLEAdvertisementWatcher()
	if err != nil {
		return ns2proble.Device{}, err
	}
	defer watcher.Stop()

	_ = watcher.SetScanningMode(advertisement.BluetoothLEScanningModeActive)

	found := make(chan ns2proble.Device, 1)
	handlerID := rt.ParameterizedInstanceGUID(
		foundation.GUIDTypedEventHandler,
		advertisement.SignatureBluetoothLEAdvertisementWatcher,
		advertisement.SignatureBluetoothLEAdvertisementReceivedEventArgs,
	)
	handler := foundation.NewTypedEventHandler(ole.NewGUID(handlerID), func(_ *foundation.TypedEventHandler, _ unsafe.Pointer, args unsafe.Pointer) {
		ev := (*advertisement.BluetoothLEAdvertisementReceivedEventArgs)(args)
		addr, err := ev.GetBluetoothAddress()
		if err != nil {
			return
		}
		ad, err := ev.GetAdvertisement()
		if err != nil {
			return
		}
		name, _ := ad.GetLocalName()
		if !strings.Contains(strings.ToLower(name), "switch 2 pro") {
			return
		}
		select {
		case found <- ns2proble.Device{Address: addr, Name: name}:
		default:
		}
	})
	token, err := watcher.AddReceived(handler)
	if err != nil {
		return ns2proble.Device{}, err
	}
	defer watcher.RemoveReceived(token)

	if err := watcher.Start(); err != nil {
		return ns2proble.Device{}, err
	}
	select {
	case device := <-found:
		return device, nil
	case <-ctx.Done():
		return ns2proble.Device{}, ctx.Err()
	}
}

func (b *Backend) Connect(ctx context.Context, device ns2proble.Device) (ns2proble.Peripheral, error) {
	if err := b.init(); err != nil {
		return nil, err
	}
	op, err := bluetooth.BluetoothLEDeviceFromBluetoothAddressAsync(device.Address)
	if err != nil {
		return nil, err
	}
	ptr, err := await(ctx, op, bluetooth.SignatureBluetoothLEDevice)
	if err != nil {
		return nil, err
	}
	bt := (*bluetooth.BluetoothLEDevice)(ptr)
	p := &peripheral{device: bt, chars: map[string]*gatt.GattCharacteristic{}}
	if err := p.discover(ctx); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (b *Backend) init() error {
	b.initOnce.Do(func() {
		b.initErr = ole.RoInitialize(1)
	})
	return b.initErr
}

type peripheral struct {
	device *bluetooth.BluetoothLEDevice
	chars  map[string]*gatt.GattCharacteristic

	mu     sync.Mutex
	tokens []subscription
}

type subscription struct {
	char  *gatt.GattCharacteristic
	token foundation.EventRegistrationToken
}

func (p *peripheral) discover(ctx context.Context) error {
	op, err := p.device.GetGattServicesWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return err
	}
	ptr, err := await(ctx, op, gatt.SignatureGattDeviceServicesResult)
	if err != nil {
		return err
	}
	result := (*gatt.GattDeviceServicesResult)(ptr)
	status, err := result.GetStatus()
	if err != nil {
		return err
	}
	if status != gatt.GattCommunicationStatusSuccess {
		return fmt.Errorf("GATT service discovery failed: %v", status)
	}
	services, err := result.GetServices()
	if err != nil {
		return err
	}
	n, err := services.GetSize()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		raw, err := services.GetAt(i)
		if err != nil {
			return err
		}
		service := (*gatt.GattDeviceService)(raw)
		if err := p.discoverCharacteristics(ctx, service); err != nil {
			return err
		}
	}
	for _, id := range []string{initUUID, inputUUID, vibrationUUID, commandUUID, commandResponseUUID} {
		if p.chars[id] == nil {
			return fmt.Errorf("required GATT characteristic missing: %s", id)
		}
	}
	return nil
}

func (p *peripheral) discoverCharacteristics(ctx context.Context, service *gatt.GattDeviceService) error {
	op, err := service.GetCharacteristicsWithCacheModeAsync(bluetooth.BluetoothCacheModeUncached)
	if err != nil {
		return err
	}
	ptr, err := await(ctx, op, gatt.SignatureGattCharacteristicsResult)
	if err != nil {
		return err
	}
	result := (*gatt.GattCharacteristicsResult)(ptr)
	status, err := result.GetStatus()
	if err != nil {
		return err
	}
	if status != gatt.GattCommunicationStatusSuccess {
		return nil
	}
	chars, err := result.GetCharacteristics()
	if err != nil {
		return err
	}
	n, err := chars.GetSize()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		raw, err := chars.GetAt(i)
		if err != nil {
			return err
		}
		ch := (*gatt.GattCharacteristic)(raw)
		id, err := ch.GetUuid()
		if err != nil {
			return err
		}
		key := strings.ToUpper((*ole.GUID)(unsafe.Pointer(&id)).String())
		p.chars[key] = ch
	}
	return nil
}

func (p *peripheral) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sub := range p.tokens {
		_ = sub.char.RemoveValueChanged(sub.token)
	}
	p.tokens = nil
	if p.device != nil {
		return p.device.Close()
	}
	return nil
}

func (p *peripheral) WriteInit(ctx context.Context, data []byte) error {
	return p.write(ctx, initUUID, data)
}

func (p *peripheral) WriteCommand(ctx context.Context, data []byte) error {
	return p.write(ctx, commandUUID, data)
}

func (p *peripheral) WriteVibration(ctx context.Context, data []byte) error {
	return p.write(ctx, vibrationUUID, data)
}

func (p *peripheral) SetInputReportRate(context.Context, []byte) error {
	return nil
}

func (p *peripheral) NotifyCommandResponse(ctx context.Context, cb func([]byte)) error {
	return p.notify(ctx, commandResponseUUID, cb)
}

func (p *peripheral) NotifyInput(ctx context.Context, cb func([]byte)) error {
	return p.notify(ctx, inputUUID, cb)
}

func (p *peripheral) write(ctx context.Context, uuid string, data []byte) error {
	ch := p.chars[uuid]
	if ch == nil {
		return fmt.Errorf("GATT characteristic unavailable: %s", uuid)
	}
	buf, err := bufferFromBytes(data)
	if err != nil {
		return err
	}
	op, err := ch.WriteValueWithOptionAsync(buf, gatt.GattWriteOptionWriteWithoutResponse)
	if err != nil {
		return err
	}
	_, err = await(ctx, op, gatt.SignatureGattCommunicationStatus)
	return err
}

func (p *peripheral) notify(ctx context.Context, uuid string, cb func([]byte)) error {
	ch := p.chars[uuid]
	if ch == nil {
		return fmt.Errorf("GATT characteristic unavailable: %s", uuid)
	}
	handlerID := rt.ParameterizedInstanceGUID(
		foundation.GUIDTypedEventHandler,
		gatt.SignatureGattCharacteristic,
		gatt.SignatureGattValueChangedEventArgs,
	)
	handler := foundation.NewTypedEventHandler(ole.NewGUID(handlerID), func(_ *foundation.TypedEventHandler, _ unsafe.Pointer, args unsafe.Pointer) {
		ev := (*gatt.GattValueChangedEventArgs)(args)
		buf, err := ev.GetCharacteristicValue()
		if err != nil {
			return
		}
		data, err := bytesFromBuffer(buf)
		if err == nil {
			cb(data)
		}
	})
	token, err := ch.AddValueChanged(handler)
	if err != nil {
		return err
	}
	op, err := ch.WriteClientCharacteristicConfigurationDescriptorAsync(gatt.GattClientCharacteristicConfigurationDescriptorValueNotify)
	if err != nil {
		_ = ch.RemoveValueChanged(token)
		return err
	}
	if _, err := await(ctx, op, gatt.SignatureGattCommunicationStatus); err != nil {
		_ = ch.RemoveValueChanged(token)
		return err
	}
	p.mu.Lock()
	p.tokens = append(p.tokens, subscription{char: ch, token: token})
	p.mu.Unlock()
	return nil
}

func await(ctx context.Context, op *foundation.IAsyncOperation, resultSignature string) (unsafe.Pointer, error) {
	done := make(chan error, 1)
	handlerID := rt.ParameterizedInstanceGUID(foundation.GUIDAsyncOperationCompletedHandler, resultSignature)
	handler := foundation.NewAsyncOperationCompletedHandler(ole.NewGUID(handlerID), func(_ *foundation.AsyncOperationCompletedHandler, _ *foundation.IAsyncOperation, status foundation.AsyncStatus) {
		if status != foundation.AsyncStatusCompleted {
			done <- fmt.Errorf("WinRT async operation ended with status %v", status)
			return
		}
		done <- nil
	})
	if err := op.SetCompleted(handler); err != nil {
		return nil, err
	}
	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}
		return op.GetResults()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func bufferFromBytes(data []byte) (*streams.IBuffer, error) {
	writer, err := streams.NewDataWriter()
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	if err := writer.WriteBytes(uint32(len(data)), data); err != nil {
		return nil, err
	}
	return writer.DetachBuffer()
}

func bytesFromBuffer(buf *streams.IBuffer) ([]byte, error) {
	n, err := buf.GetLength()
	if err != nil {
		return nil, err
	}
	reader, err := streams.DataReaderFromBuffer(buf)
	if err != nil {
		return nil, err
	}
	return reader.ReadBytes(n)
}
