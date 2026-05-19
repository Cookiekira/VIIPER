package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/Alia5/VIIPER/apiclient"
)

type HIDOutputLogOptions struct {
	Path       string
	BLEPreview bool
	BLEWriter  BLEOutputWriter
}

type BLEOutputWriter interface {
	WriteBLEOutput02(payload []byte) error
}

func logHIDOutput(stream *apiclient.DeviceStream, options HIDOutputLogOptions) {
	var file *os.File
	if options.Path != "" {
		var err error
		file, err = os.OpenFile(options.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "HID OUT log open failed: %v\n", err)
			return
		}
		defer file.Close()
		fmt.Fprintln(file, "time\tlen\thex\tble_output_02_preview")
		fmt.Printf("Writing HID OUT reports to %s\n", options.Path)
	}

	buf := make([]byte, 64)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}

		report := append([]byte(nil), buf[:n]...)
		hexReport := hex.EncodeToString(report)
		ble, hasBLE := RumbleUSB02ToBLE(report)
		bleHex := ""
		if hasBLE {
			bleHex = hex.EncodeToString(ble)
		}

		fmt.Printf("HID OUT %d bytes: %s", n, hexReport)
		if hasBLE {
			left, right := rumbleNonZero(report)
			fmt.Printf(" rumble(left=%v right=%v)", left, right)
			if options.BLEPreview {
				fmt.Printf(" ble02=%s", bleHex)
			}
			if options.BLEWriter != nil {
				if err := options.BLEWriter.WriteBLEOutput02(ble); err != nil {
					fmt.Printf(" bleWriteErr=%v", err)
				} else {
					fmt.Printf(" bleWrite=true")
				}
			}
		}
		fmt.Println()

		if file != nil {
			fmt.Fprintf(file, "%s\t%d\t%s\t%s\n", time.Now().Format(time.RFC3339Nano), n, hexReport, bleHex)
		}
	}
}

func RumbleUSB02ToBLE(report []byte) ([]byte, bool) {
	if len(report) < 33 || report[0] != 0x02 {
		return nil, false
	}

	ble := make([]byte, 42)
	ble[0] = 0x00
	copy(ble[1:17], report[1:17])
	copy(ble[17:33], report[17:33])
	return ble, true
}

func rumbleNonZero(report []byte) (left bool, right bool) {
	if len(report) < 33 || report[0] != 0x02 {
		return false, false
	}
	return anyNonZero(report[1:17]), anyNonZero(report[17:33])
}

func anyNonZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return true
		}
	}
	return false
}
