//go:build !windows

package winrt

import (
	"context"
	"fmt"

	"github.com/Alia5/VIIPER/internal/ns2proble"
)

type Backend struct{}

func New() *Backend { return &Backend{} }

func (b *Backend) Scan(ctx context.Context) (ns2proble.Device, error) {
	return ns2proble.Device{}, fmt.Errorf("WinRT BLE backend is only available on Windows")
}

func (b *Backend) Connect(ctx context.Context, device ns2proble.Device) (ns2proble.Peripheral, error) {
	return nil, fmt.Errorf("WinRT BLE backend is only available on Windows")
}
