package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Alia5/VIIPER/apiclient"
	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/device/switch2pro"
)

func main() {
	addr := flag.String("addr", "localhost:3242", "VIIPER API server address")
	password := flag.String("password", "", "VIIPER API password, only needed when localhost auth is enabled or connecting remotely")
	busFlag := flag.Uint("bus", 0, "bus ID to use; 0 creates the next free bus")
	devType := flag.String("type", "switch2pro", "device type to create: switch2pro or ns2pro")
	cleanup := flag.Bool("cleanup", true, "remove the virtual device on exit")
	bulkReplay := flag.Bool("bulk-replay", true, "enable captured bulk init/config replay fixtures")
	interval := flag.Duration("interval", 4*time.Millisecond, "neutral input report write interval")
	holdA := flag.Bool("hold-a", false, "hold a synthetic A button in dummy input reports")
	pulseAAfter := flag.Duration("pulse-a-after", 0, "pulse a synthetic A button after this delay; 0 disables")
	pulseADuration := flag.Duration("pulse-a-duration", 800*time.Millisecond, "synthetic A pulse duration")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := apiclient.New(*addr)
	if *password != "" {
		client = apiclient.NewWithPassword(*addr, *password)
	}

	busID, createdBus, err := ensureBus(ctx, client, uint32(*busFlag))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus setup failed: %v\n", err)
		os.Exit(1)
	}

	opts := &device.CreateOptions{
		DeviceSpecific: map[string]any{
			"bulkReplay": *bulkReplay,
		},
	}

	stream, dev, err := client.AddDeviceAndConnect(ctx, busID, *devType, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "AddDeviceAndConnect failed: %v\n", err)
		if createdBus {
			_, _ = client.BusRemoveCtx(context.Background(), busID)
		}
		os.Exit(1)
	}
	defer stream.Close()

	fmt.Printf("Created and connected %s device %d-%s (%s:%s)\n", dev.Type, dev.BusID, dev.DevId, dev.Vid, dev.Pid)
	fmt.Printf("Keep this process running while Steam/Windows uses the virtual controller.\n")
	fmt.Printf("Attach busid %d-%s from your USBIP GUI when you are ready.\n", dev.BusID, dev.DevId)

	if *cleanup {
		defer func() {
			if _, err := client.DeviceRemoveCtx(context.Background(), stream.BusID, stream.DevID); err != nil {
				fmt.Printf("DeviceRemove warning: %v\n", err)
			}
			if createdBus {
				if _, err := client.BusRemoveCtx(context.Background(), busID); err != nil {
					fmt.Printf("BusRemove warning: %v\n", err)
				}
			}
		}()
	}

	go logHIDOutput(stream)

	report := switch2pro.NeutralInputReport()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	startedAt := time.Now()
	var frames uint64
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping.")
			return
		case <-ticker.C:
			frames++
			nextReport := append([]byte(nil), report...)
			if *holdA || shouldPulse(startedAt, *pulseAAfter, *pulseADuration) {
				setSyntheticA(nextReport, true)
			}
			if _, err := stream.Write(nextReport); err != nil {
				fmt.Fprintf(os.Stderr, "stream write failed: %v\n", err)
				return
			}
			if frames%250 == 0 {
				fmt.Printf("Sent %d neutral input reports\n", frames)
			}
		}
	}
}

func shouldPulse(startedAt time.Time, after, duration time.Duration) bool {
	if after <= 0 || duration <= 0 {
		return false
	}
	elapsed := time.Since(startedAt)
	return elapsed >= after && elapsed < after+duration
}

func setSyntheticA(report []byte, pressed bool) {
	if len(report) < 4 {
		return
	}
	if pressed {
		report[3] |= 0x01
		return
	}
	report[3] &^= 0x01
}

func ensureBus(ctx context.Context, client *apiclient.Client, requested uint32) (busID uint32, created bool, err error) {
	if requested == 0 {
		resp, err := client.BusCreateCtx(ctx, 0)
		if err != nil {
			return 0, false, err
		}
		fmt.Printf("Created bus %d\n", resp.BusID)
		return resp.BusID, true, nil
	}

	list, err := client.BusListCtx(ctx)
	if err != nil {
		return 0, false, err
	}
	for _, id := range list.Buses {
		if id == requested {
			fmt.Printf("Using existing bus %d\n", requested)
			return requested, false, nil
		}
	}

	resp, err := client.BusCreateCtx(ctx, requested)
	if err != nil {
		return 0, false, err
	}
	fmt.Printf("Created bus %d\n", resp.BusID)
	return resp.BusID, true, nil
}

func logHIDOutput(stream *apiclient.DeviceStream) {
	buf := make([]byte, 64)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		fmt.Printf("HID OUT %d bytes: %s\n", n, hex.EncodeToString(buf[:n]))
	}
}
