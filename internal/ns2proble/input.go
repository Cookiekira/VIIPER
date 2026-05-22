package ns2proble

import (
	"encoding/binary"
	"fmt"
)

const (
	StickMin    uint16 = 0
	StickCenter uint16 = 0x0800
	StickMax    uint16 = 0x0FFF
	BatteryMax  uint8  = 9
)

const (
	ButtonB uint32 = 1 << iota
	ButtonA
	ButtonY
	ButtonX
	ButtonR
	ButtonZR
	ButtonPlus
	ButtonRightStick
	ButtonDown
	ButtonRight
	ButtonLeft
	ButtonUp
	ButtonL
	ButtonZL
	ButtonMinus
	ButtonLeftStick
	ButtonHome
	ButtonCapture
	ButtonGR
	ButtonGL
	ButtonC
	ButtonHeadset
)

type InputState struct {
	Buttons uint32

	LX, LY uint16
	RX, RY uint16

	AccelX, AccelY, AccelZ int16
	GyroX, GyroY, GyroZ    int16

	BatteryLevel  uint8
	Charging      bool
	ExternalPower bool
}

type StickCalibration struct {
	CenterX int
	CenterY int
	MaxX    int
	MaxY    int
	MinX    int
	MinY    int
}

func UnpackStick12(data []byte) (int, int, error) {
	if len(data) < 3 {
		return 0, 0, fmt.Errorf("12-bit stick data requires 3 bytes")
	}
	x := int(data[0]) | (int(data[1]&0x0F) << 8)
	y := int(data[1]>>4) | (int(data[2]) << 4)
	return x, y, nil
}

func UnpackStickCalibration(data []byte) (StickCalibration, error) {
	if len(data) < 9 {
		return StickCalibration{}, fmt.Errorf("stick calibration requires 9 bytes")
	}
	cx, cy, err := UnpackStick12(data[0:3])
	if err != nil {
		return StickCalibration{}, err
	}
	maxX, maxY, err := UnpackStick12(data[3:6])
	if err != nil {
		return StickCalibration{}, err
	}
	minX, minY, err := UnpackStick12(data[6:9])
	if err != nil {
		return StickCalibration{}, err
	}
	return StickCalibration{CenterX: cx, CenterY: cy, MaxX: maxX, MaxY: maxY, MinX: minX, MinY: minY}, nil
}

func NormalizeStick(x, y int, cal *StickCalibration) (uint16, uint16) {
	if cal == nil {
		return uint16(clamp(x, int(StickMin), int(StickMax))), uint16(clamp(y, int(StickMin), int(StickMax)))
	}
	return uint16(normalizeAxis(x, cal.CenterX, cal.MaxX, cal.MinX)),
		uint16(normalizeAxis(y, cal.CenterY, cal.MaxY, cal.MinY))
}

func normalizeAxis(value, center, positiveSpan, negativeSpan int) int {
	if value >= center {
		if positiveSpan <= 0 {
			return int(StickCenter)
		}
		return clamp(int(StickCenter)+((value-center)*(int(StickMax)-int(StickCenter))+positiveSpan/2)/positiveSpan, int(StickMin), int(StickMax))
	}
	if negativeSpan <= 0 {
		return int(StickCenter)
	}
	return clamp(int(StickCenter)-((center-value)*(int(StickCenter)-int(StickMin))+negativeSpan/2)/negativeSpan, int(StickMin), int(StickMax))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ParseCommonReport(data []byte, primary, secondary *StickCalibration) (InputState, error) {
	if len(data) < 0x10 {
		return InputState{}, fmt.Errorf("short common report: %d", len(data))
	}
	lxRaw, lyRaw, err := UnpackStick12(data[0x0A:0x0D])
	if err != nil {
		return InputState{}, err
	}
	rxRaw, ryRaw, err := UnpackStick12(data[0x0D:0x10])
	if err != nil {
		return InputState{}, err
	}
	lx, ly := NormalizeStick(lxRaw, lyRaw, primary)
	rx, ry := NormalizeStick(rxRaw, ryRaw, secondary)

	st := InputState{
		Buttons:       MapCommonButtons(data[0x04:0x08]),
		LX:            lx,
		LY:            ly,
		RX:            rx,
		RY:            ry,
		BatteryLevel:  BatteryMax,
		ExternalPower: true,
	}
	if len(data) >= 0x21 {
		voltage := binary.LittleEndian.Uint16(data[0x1F:0x21])
		if voltage > 0 {
			st.BatteryLevel = uint8(clamp((int(voltage)-3200)*int(BatteryMax)/800, 0, int(BatteryMax)))
			st.ExternalPower = true
		}
	}
	if len(data) > 0x21 {
		st.Charging = data[0x21] == 0x34
	}
	if len(data) >= 0x3C {
		st.AccelX = int16(binary.LittleEndian.Uint16(data[0x30:0x32]))
		st.AccelY = int16(binary.LittleEndian.Uint16(data[0x32:0x34]))
		st.AccelZ = int16(binary.LittleEndian.Uint16(data[0x34:0x36]))
		st.GyroX = int16(binary.LittleEndian.Uint16(data[0x36:0x38]))
		st.GyroY = int16(binary.LittleEndian.Uint16(data[0x38:0x3A]))
		st.GyroZ = int16(binary.LittleEndian.Uint16(data[0x3A:0x3C]))
	}
	return st, nil
}

func MapCommonButtons(raw []byte) uint32 {
	if len(raw) < 4 {
		return 0
	}
	var b uint32
	if raw[0]&0x01 != 0 {
		b |= ButtonY
	}
	if raw[0]&0x02 != 0 {
		b |= ButtonX
	}
	if raw[0]&0x04 != 0 {
		b |= ButtonB
	}
	if raw[0]&0x08 != 0 {
		b |= ButtonA
	}
	if raw[0]&0x40 != 0 {
		b |= ButtonR
	}
	if raw[0]&0x80 != 0 {
		b |= ButtonZR
	}
	if raw[1]&0x01 != 0 {
		b |= ButtonMinus
	}
	if raw[1]&0x02 != 0 {
		b |= ButtonPlus
	}
	if raw[1]&0x04 != 0 {
		b |= ButtonRightStick
	}
	if raw[1]&0x08 != 0 {
		b |= ButtonLeftStick
	}
	if raw[1]&0x10 != 0 {
		b |= ButtonHome
	}
	if raw[1]&0x20 != 0 {
		b |= ButtonCapture
	}
	if raw[1]&0x40 != 0 {
		b |= ButtonC
	}
	if raw[2]&0x01 != 0 {
		b |= ButtonDown
	}
	if raw[2]&0x02 != 0 {
		b |= ButtonUp
	}
	if raw[2]&0x04 != 0 {
		b |= ButtonRight
	}
	if raw[2]&0x08 != 0 {
		b |= ButtonLeft
	}
	if raw[2]&0x40 != 0 {
		b |= ButtonL
	}
	if raw[2]&0x80 != 0 {
		b |= ButtonZL
	}
	if raw[3]&0x01 != 0 {
		b |= ButtonGR
	}
	if raw[3]&0x02 != 0 {
		b |= ButtonGL
	}
	if raw[3]&0x10 != 0 {
		b |= ButtonHeadset
	}
	return b
}
