package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	vlog "github.com/Alia5/VIIPER/internal/log"
	"github.com/Alia5/VIIPER/internal/ns2proble"
	"github.com/Alia5/VIIPER/internal/ns2proble/winrt"
	"github.com/Alia5/VIIPER/internal/ns2probridge"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		usbAddr       = flag.String("usb-addr", "localhost:3241", "USB-IP server listen address")
		deviceAddress = flag.String("device-address", "", "BLE address to connect directly before scanning")
		pairHost      = flag.Bool("pair-host", false, "Pair controller to this host for reconnect/wake")
		hostAddress   = flag.String("host-address", "", "Host Bluetooth adapter address for --pair-host")
		forgetDevice  = flag.Bool("forget-device", false, "Delete remembered BLE device and exit")
		cacheFile     = flag.String("cache-file", "", "Remembered BLE device cache path")
		logLevel      = flag.String("log-level", "info", "Log level: trace, debug, info, warn, error")
		logFile       = flag.String("log-file", "", "Log file path")
		noAutoAttach  = flag.Bool("no-auto-attach", false, "Do not auto-attach the virtual USB device through usbip-win2")
		featureFlags  = flag.String("feature-flags", "0x07", "BLE feature flags to enable")
	)
	flag.Parse()

	logger, closers, err := vlog.SetupLogger(*logLevel, *logFile)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		return 2
	}
	defer closeAll(closers)

	flags, err := strconv.ParseUint(*featureFlags, 0, 8)
	if err != nil {
		logger.Error("invalid --feature-flags", "value", *featureFlags, "error", err)
		return 2
	}

	var host uint64
	if *pairHost {
		if *hostAddress == "" {
			logger.Error("--host-address is required with --pair-host")
			return 2
		}
		host, err = ns2proble.ParseBluetoothAddress(*hostAddress)
		if err != nil {
			logger.Error("invalid --host-address", "value", *hostAddress, "error", err)
			return 2
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := ns2probridge.Config{
		USBAddr:               *usbAddr,
		DeviceAddress:         *deviceAddress,
		CacheFile:             *cacheFile,
		PairHostAddress:       host,
		PairHost:              *pairHost,
		ForgetDevice:          *forgetDevice,
		AutoAttach:            !*noAutoAttach,
		AutoAttachNativeIOCTL: true,
		FeatureFlags:          byte(flags),
	}
	if err := ns2probridge.New(cfg, winrt.New(), logger).Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("bridge failed", "error", err)
		return 1
	}
	return 0
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}
