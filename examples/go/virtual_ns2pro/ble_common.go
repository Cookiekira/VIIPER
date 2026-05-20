package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	ns2BLEServiceUUIDString  = "ab7de9be-89fe-49ad-828f-118f09df7fd0"
	ns2BLEOutput02UUIDString = "cc483f51-9258-427d-a939-630c31f72b05"
	ns2BLECommandUUIDString  = "649d4ac9-8eb7-4e6c-af44-1ea54fe5f005"
	ns2BLECompanyID          = 0x0553
)

type bleTarget struct {
	Address bluetooth.Address
	Name    string
}

func (t bleTarget) DisplayName() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Address.String()
}

func scanForNS2Pro(ctx context.Context, adapter *bluetooth.Adapter, address, nameContains string, timeout time.Duration) (bleTarget, error) {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}

	address = strings.TrimSpace(address)
	if address != "" {
		mac, err := bluetooth.ParseMAC(address)
		if err != nil {
			return bleTarget{}, fmt.Errorf("parse BLE address %q: %w", address, err)
		}
		addr := bluetooth.Address{}
		addr.MAC = mac
		return bleTarget{Address: addr}, nil
	}

	serviceUUID, err := bluetooth.ParseUUID(ns2BLEServiceUUIDString)
	if err != nil {
		return bleTarget{}, err
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		mu    sync.Mutex
		found bleTarget
		ok    bool
	)
	done := make(chan struct{})
	go func() {
		<-scanCtx.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			if err := adapter.StopScan(); err == nil {
				return
			}
			select {
			case <-done:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	if nameContains == "" {
		fmt.Println("Scanning for NS2Pro BLE device.")
	} else {
		fmt.Printf("Scanning for NS2Pro BLE device matching %q.\n", nameContains)
	}

	err = adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		if !matchesNS2ProScanResult(result, nameContains, serviceUUID) {
			return
		}

		mu.Lock()
		found = bleTarget{Address: result.Address, Name: scanResultLocalName(result)}
		ok = true
		mu.Unlock()
		_ = adapter.StopScan()
	})
	close(done)

	mu.Lock()
	defer mu.Unlock()
	if ok {
		return found, nil
	}
	if scanCtx.Err() != nil {
		return bleTarget{}, fmt.Errorf("scan NS2Pro BLE device timed out after %s", timeout)
	}
	if err != nil {
		return bleTarget{}, fmt.Errorf("scan NS2Pro BLE device: %w", err)
	}
	return bleTarget{}, fmt.Errorf("NS2Pro BLE device not found")
}

func matchesNS2ProScanResult(result bluetooth.ScanResult, nameContains string, serviceUUID bluetooth.UUID) bool {
	name := scanResultLocalName(result)
	if nameContains != "" && strings.Contains(strings.ToLower(name), strings.ToLower(nameContains)) {
		return true
	}
	if result.AdvertisementPayload == nil {
		return false
	}
	if result.HasServiceUUID(serviceUUID) {
		return true
	}
	for _, data := range result.ManufacturerData() {
		if data.CompanyID == ns2BLECompanyID {
			return true
		}
	}
	return false
}

func scanResultLocalName(result bluetooth.ScanResult) string {
	if result.AdvertisementPayload == nil {
		return ""
	}
	return result.LocalName()
}
