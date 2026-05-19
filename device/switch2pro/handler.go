package switch2pro

import (
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/internal/server/api"
	"github.com/Alia5/VIIPER/usb"
)

func init() {
	api.RegisterDevice("switch2pro", &handler{})
	api.RegisterDevice("ns2pro", &handler{})
}

type handler struct{}

func (h *handler) CreateDevice(o *device.CreateOptions) (usb.Device, error) { return New(o) }

func (h *handler) StreamHandler() api.StreamHandlerFunc {
	return func(conn net.Conn, devPtr *usb.Device, logger *slog.Logger) error {
		if devPtr == nil || *devPtr == nil {
			return fmt.Errorf("nil device")
		}
		dev, ok := (*devPtr).(*Switch2Pro)
		if !ok {
			return fmt.Errorf("device is not switch2pro")
		}

		dev.SetHIDOutputCallback(func(report []byte) {
			if _, err := conn.Write(report); err != nil {
				logger.Error("failed to send switch2pro HID output report", "error", err)
			}
		})

		buf := make([]byte, InputReportSize)
		for {
			if _, err := io.ReadFull(conn, buf); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					logger.Info("client disconnected")
					return nil
				}
				return fmt.Errorf("read switch2pro input report: %w", err)
			}
			if !dev.UpdateInputReport(buf) {
				return fmt.Errorf("invalid switch2pro input report: len=%d reportID=0x%02x", len(buf), buf[0])
			}
		}
	}
}
