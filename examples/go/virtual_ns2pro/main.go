package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Alia5/VIIPER/apiclient"
	"github.com/Alia5/VIIPER/device"
)

func main() {
	addr := flag.String("addr", "localhost:3242", "VIIPER API server address")
	password := flag.String("password", "", "VIIPER API password, only needed when localhost auth is enabled or connecting remotely")
	busFlag := flag.Uint("bus", 0, "bus ID to use; 0 creates the next free bus")
	devType := flag.String("type", "ns2pro", "device type to create")
	cleanup := flag.Bool("cleanup", true, "remove the virtual device on exit")
	bulkReplay := flag.Bool("bulk-replay", true, "enable captured bulk init/config replay fixtures")
	interval := flag.Duration("interval", 4*time.Millisecond, "neutral input report write interval")
	keyboardMode := flag.Bool("keyboard", false, "poll the local keyboard and map keys to NS2Pro input report 0x05")
	terminalMode := flag.Bool("terminal", false, "read key presses directly from this terminal and map them to NS2Pro input report 0x05 pulses")
	terminalPulse := flag.Duration("terminal-pulse", 120*time.Millisecond, "duration to hold each --terminal key press")
	invertStickY := flag.Bool("invert-stick-y", false, "invert both left and right stick Y axes")
	hidOutLog := flag.String("hid-out-log", "", "optional TSV path for HID OUT reports from Steam")
	hidOutBLEPreview := flag.Bool("hid-out-ble-preview", false, "print derived BLE output payload preview for HID OUT report 0x02")
	bleInput := flag.Bool("ble-input", false, "subscribe to a real NS2Pro BLE input characteristic and forward it as USB report 0x05")
	bleInputReport := flag.String("ble-input-report", "09", "BLE input report characteristic to use with --ble-input: 09 by default, or 05 as a fallback")
	bleInputLog := flag.String("ble-input-log", "", "optional TSV path for raw BLE input notifications")
	bleInputDecodeLog := flag.String("ble-input-decode-log", "", "optional TSV path for decoded BLE report 0x09 input notifications")
	blePlayerLED := flag.Uint("ble-player-led", 1, "BLE player LED bitmask to set after input notifications start; 0 disables")
	bleRumble := flag.Bool("ble-rumble", false, "write Steam HID OUT report 0x02 rumble packets to the real NS2Pro BLE output report characteristic")
	bleGyro := flag.Bool("ble-gyro", false, "experimental: enable BLE IMU reporting and bridge report 0x09 motion bytes into USB report 0x05")
	bleAddress := flag.String("ble-address", "", "optional BLE address to connect for BLE input/rumble/gyro")
	bleName := flag.String("ble-name", "Pro Controller", "BLE local-name substring to scan for when --ble-address is empty")
	bleScanTimeout := flag.Duration("ble-scan-timeout", 12*time.Second, "BLE scan/connect timeout")
	bleWriteWithResponse := flag.Bool("ble-write-with-response", false, "use BLE Write Request instead of Write Without Response")
	printKeymap := flag.Bool("print-keymap", false, "print the keyboard mapping used by --keyboard")
	holdA := flag.Bool("hold-a", false, "hold a synthetic A button in dummy input reports")
	pulseAAfter := flag.Duration("pulse-a-after", 0, "pulse a synthetic A button after this delay; 0 disables")
	pulseADuration := flag.Duration("pulse-a-duration", 800*time.Millisecond, "synthetic A pulse duration")
	flag.Parse()

	inputModeCount := 0
	for _, enabled := range []bool{*keyboardMode, *terminalMode, *bleInput} {
		if enabled {
			inputModeCount++
		}
	}
	if inputModeCount > 1 {
		fmt.Fprintln(os.Stderr, "--keyboard, --terminal, and --ble-input are mutually exclusive")
		os.Exit(2)
	}
	if *blePlayerLED > 0xff {
		fmt.Fprintln(os.Stderr, "--ble-player-led must be between 0 and 255")
		os.Exit(2)
	}
	if *bleGyro && !*bleInput {
		fmt.Fprintln(os.Stderr, "--ble-gyro requires --ble-input")
		os.Exit(2)
	}

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
	if *printKeymap || *keyboardMode || *terminalMode {
		fmt.Print(keymapHelp())
	}
	if *keyboardMode {
		fmt.Printf("Keyboard mode enabled. Polling local keyboard every %s.\n", interval.String())
	}
	var bleInputClient *BLEInputClient
	var bleWriter BLEOutputWriter
	if *bleInput {
		var err error
		bleInputClient, err = ConnectBLEInput(ctx, BLEInputOptions{
			Address:           *bleAddress,
			NameContains:      *bleName,
			Timeout:           *bleScanTimeout,
			Report:            *bleInputReport,
			RawLogPath:        *bleInputLog,
			DecodeLogPath:     *bleInputDecodeLog,
			PlayerLED:         byte(*blePlayerLED),
			WriteWithResponse: *bleWriteWithResponse,
			EnableGyro:        *bleGyro,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "BLE input setup failed: %v\n", err)
			return
		}
		defer bleInputClient.Close()
		if *bleGyro {
			if err := bleInputClient.EnableBLEGyro(); err != nil {
				fmt.Fprintf(os.Stderr, "BLE gyro setup failed: %v\n", err)
				return
			}
			fmt.Printf("BLE gyro bridge enabled on the existing BLE input connection.\n")
		}
		if *bleRumble {
			if err := bleInputClient.EnableBLERumble(); err != nil {
				fmt.Fprintf(os.Stderr, "BLE rumble setup failed: %v\n", err)
				return
			}
			bleWriter = bleInputClient
			fmt.Printf("BLE rumble enabled on the existing BLE input connection.\n")
		}
	}
	if *bleRumble && bleWriter == nil {
		ble, err := ConnectBLERumble(ctx, BLERumbleOptions{
			Address:           *bleAddress,
			NameContains:      *bleName,
			Timeout:           *bleScanTimeout,
			WriteWithResponse: *bleWriteWithResponse,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "BLE rumble setup failed: %v\n", err)
			return
		}
		defer ble.Close()
		bleWriter = ble
	}
	var terminal *terminalInput
	if *terminalMode {
		var err error
		terminal, err = startTerminalInput(ctx, *terminalPulse)
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal input setup failed: %v\n", err)
			return
		}
		defer terminal.Close()
		fmt.Printf("Terminal mode enabled. Press mapped keys in this terminal; Ctrl+C stops.\n")
	}

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

	go logHIDOutput(stream, HIDOutputLogOptions{
		Path:       *hidOutLog,
		BLEPreview: *hidOutBLEPreview,
		BLEWriter:  bleWriter,
	})

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	startedAt := time.Now()
	var frames uint32
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping.")
			return
		case <-terminalInterrupt(terminal):
			fmt.Println("Stopping.")
			return
		case <-ticker.C:
			frames++
			var input ControllerInput
			var nextReport []byte
			if bleInputClient != nil {
				nextReport = bleInputClient.LatestInputReport()
			}
			if nextReport == nil && *keyboardMode {
				keyboardInput, err := pollKeyboardInput()
				if err != nil {
					fmt.Fprintf(os.Stderr, "keyboard polling failed: %v\n", err)
					return
				}
				input = keyboardInput
			} else if terminal != nil {
				input = terminal.Input()
			}
			if *holdA || shouldPulse(startedAt, *pulseAAfter, *pulseADuration) {
				input.A = true
			}
			if nextReport == nil {
				nextReport = BuildInputReportWithOptions(frames, input, ReportOptions{
					InvertStickY: *invertStickY,
				})
			}
			if _, err := stream.Write(nextReport); err != nil {
				fmt.Fprintf(os.Stderr, "stream write failed: %v\n", err)
				return
			}
			if frames%250 == 0 {
				fmt.Printf("Sent %d NS2Pro input reports\n", frames)
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
