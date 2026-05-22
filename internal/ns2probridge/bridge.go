package ns2probridge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/device/ns2pro"
	"github.com/Alia5/VIIPER/internal/ns2proble"
	"github.com/Alia5/VIIPER/internal/server/api"
	srvusb "github.com/Alia5/VIIPER/internal/server/usb"
	"github.com/Alia5/VIIPER/virtualbus"
)

type Config struct {
	USBAddr               string
	DeviceAddress         string
	CacheFile             string
	PairHostAddress       uint64
	PairHost              bool
	ForgetDevice          bool
	AutoAttach            bool
	AutoAttachNativeIOCTL bool
	FeatureFlags          byte
}

type Bridge struct {
	cfg     Config
	backend ns2proble.Backend
	logger  *slog.Logger

	usbSrv *srvusb.Server
	bus    *virtualbus.VirtualBus
	dev    *ns2pro.NS2Pro
	ctrl   *ns2proble.Controller
}

func New(cfg Config, backend ns2proble.Backend, logger *slog.Logger) *Bridge {
	if cfg.USBAddr == "" {
		cfg.USBAddr = "localhost:3241"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{cfg: cfg, backend: backend, logger: logger}
}

func (b *Bridge) Run(ctx context.Context) error {
	if b.cfg.ForgetDevice {
		return ns2proble.ForgetCachedDevice(b.cfg.CacheFile)
	}
	if err := b.startUSB(ctx); err != nil {
		return err
	}
	defer b.shutdown()

	b.ctrl = ns2proble.NewController(b.backend, b.logger, b.cfg.FeatureFlags)
	b.dev.SetOutputCallback(func(out ns2pro.OutputState) {
		p := b.ctrl.Peripheral()
		if p == nil {
			return
		}
		opCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if out.Flags&ns2pro.OutputFlagRumble != 0 {
			if err := b.ctrl.SendRumble(opCtx, out.LeftRumble[:], out.RightRumble[:]); err != nil {
				b.logger.Debug("BLE rumble write failed", "error", err)
			}
		}
		if out.Flags&ns2pro.OutputFlagLED != 0 {
			if err := b.ctrl.SetPlayerLEDs(opCtx, out.PlayerLedMask); err != nil {
				b.logger.Debug("BLE LED write failed", "error", err)
			}
		}
	})

	for ctx.Err() == nil {
		device, err := b.findDevice(ctx)
		if err != nil {
			return err
		}
		b.logger.Info("Connecting BLE controller", "address", device.AddressString(), "name", device.Name)
		if err := b.ctrl.ConnectAndInitialize(ctx, device, func(st ns2proble.InputState) {
			b.dev.UpdateInputState(toDeviceInput(st))
		}); err != nil {
			b.logger.Error("BLE connection/init failed", "error", err)
			_ = b.ctrl.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		_ = ns2proble.SaveCachedDevice(b.cfg.CacheFile, device)
		if b.cfg.PairHost {
			pairCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := b.ctrl.PairHost(pairCtx, b.cfg.PairHostAddress)
			cancel()
			if err != nil {
				b.logger.Error("pair-host failed", "error", err)
			} else {
				b.logger.Info("pair-host completed")
			}
		}
		b.logger.Info("BLE controller initialized", "address", device.AddressString())
		<-ctx.Done()
	}
	return ctx.Err()
}

func (b *Bridge) startUSB(ctx context.Context) error {
	b.usbSrv = srvusb.New(srvusb.ServerConfig{
		Addr:                    b.cfg.USBAddr,
		ConnectionTimeout:       30 * time.Second,
		BusCleanupTimeout:       5 * time.Second,
		WriteBatchFlushInterval: time.Millisecond,
	}, b.logger, nil)
	errCh := make(chan error, 1)
	go func() { errCh <- b.usbSrv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-b.usbSrv.Ready():
	case <-ctx.Done():
		return ctx.Err()
	}

	busID := b.usbSrv.NextFreeBusID()
	bus, err := virtualbus.NewWithBusId(busID)
	if err != nil {
		return err
	}
	b.bus = bus
	if err := b.usbSrv.AddBus(bus); err != nil {
		return err
	}
	dev, err := ns2pro.New(nil)
	if err != nil {
		return err
	}
	b.dev = dev
	devCtx, err := bus.Add(dev)
	if err != nil {
		return err
	}
	meta := device.GetDeviceMeta(devCtx)
	if meta == nil {
		return fmt.Errorf("virtual device metadata missing")
	}
	b.logger.Info("Virtual USB NS2Pro is active", "bus", meta.BusId, "device", meta.DevId)
	if b.cfg.AutoAttach {
		if err := api.AttachLocalhostClient(ctx, meta, b.usbSrv.GetListenPort(), b.cfg.AutoAttachNativeIOCTL, b.logger); err != nil {
			return fmt.Errorf("auto-attach virtual USB device: %w", err)
		}
	}
	return nil
}

func (b *Bridge) findDevice(ctx context.Context) (ns2proble.Device, error) {
	if b.cfg.DeviceAddress != "" {
		addr, err := ns2proble.ParseBluetoothAddress(b.cfg.DeviceAddress)
		if err != nil {
			return ns2proble.Device{}, err
		}
		return ns2proble.Device{Address: addr}, nil
	}
	if cached, ok := ns2proble.LoadCachedDevice(b.cfg.CacheFile); ok {
		b.logger.Info("Using cached BLE controller", "address", cached.AddressString())
		return cached, nil
	}
	b.logger.Info("Scanning for Switch 2 Pro Controller")
	return b.backend.Scan(ctx)
}

func (b *Bridge) shutdown() {
	if b.ctrl != nil {
		_ = b.ctrl.Close()
	}
	if b.bus != nil && b.dev != nil {
		_ = b.bus.Remove(b.dev)
	}
	if b.usbSrv != nil {
		_ = b.usbSrv.Close()
	}
	if b.bus != nil {
		_ = b.bus.Close()
	}
}

func toDeviceInput(st ns2proble.InputState) ns2pro.InputState {
	return ns2pro.InputState{
		Buttons:       st.Buttons,
		LX:            st.LX,
		LY:            st.LY,
		RX:            st.RX,
		RY:            st.RY,
		AccelX:        st.AccelX,
		AccelY:        st.AccelY,
		AccelZ:        st.AccelZ,
		GyroX:         st.GyroX,
		GyroY:         st.GyroY,
		GyroZ:         st.GyroZ,
		BatteryLevel:  st.BatteryLevel,
		Charging:      st.Charging,
		ExternalPower: st.ExternalPower,
	}
}
