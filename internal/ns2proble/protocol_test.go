package ns2proble

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestControllerCommand(t *testing.T) {
	got := ControllerCommand(0x09, 0x07, []byte{0x06, 0, 0}, 3)
	want := []byte{0x09, 0x91, 0x03, 0x07, 0, 3, 0, 0, 0x06, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestControllerResponsePayload(t *testing.T) {
	got, err := ControllerResponsePayload([]byte{0x02, 0x91, 0, 0x04, 0, 2, 0, 0, 0xAA, 0xBB}, 0x02, 0x04)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0xAA, 0xBB}) {
		t.Fatalf("payload = %x", got)
	}
	if _, err := ControllerResponsePayload([]byte{0x02, 0x91, 0, 0x05, 0, 0, 0, 0}, 0x02, 0x04); err == nil {
		t.Fatal("expected command/subcommand mismatch error")
	}
}

func TestBluetoothAddressParsingAndFormatting(t *testing.T) {
	addr, err := ParseBluetoothAddress("12:34:56:AB:CD:EF")
	if err != nil {
		t.Fatal(err)
	}
	if addr != 0x123456ABCDEF {
		t.Fatalf("addr = %#x", addr)
	}
	if got := FormatBluetoothAddress(addr); got != "12:34:56:AB:CD:EF" {
		t.Fatalf("format = %q", got)
	}
	if got := BluetoothAddressBytes(addr); !bytes.Equal(got, []byte{0x12, 0x34, 0x56, 0xAB, 0xCD, 0xEF}) {
		t.Fatalf("bytes = %x", got)
	}
	if got := ReverseBytes([]byte{1, 2, 3}); !bytes.Equal(got, []byte{3, 2, 1}) {
		t.Fatalf("reverse = %x", got)
	}
}

func TestCachedDeviceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	device := Device{Address: 0x123456ABCDEF, Name: "Switch 2 Pro Controller"}
	if err := SaveCachedDevice(path, device); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadCachedDevice(path)
	if !ok {
		t.Fatal("cache did not load")
	}
	if got.Address != device.Address {
		t.Fatalf("cached address = %#x, want %#x", got.Address, device.Address)
	}
	if err := ForgetCachedDevice(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCachedDevice(path); ok {
		t.Fatal("cache still loads after forget")
	}
}
