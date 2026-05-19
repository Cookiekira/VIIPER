package main

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/Alia5/VIIPER/device/ns2pro"
)

const (
	stickMin = 0x000
	stickMax = 0x0fff
)

type ControllerInput struct {
	A, B, X, Y  bool
	L, R        bool
	ZL, ZR      bool
	SLLeft      bool
	SRLeft      bool
	SLRight     bool
	SRRight     bool
	Plus        bool
	Minus       bool
	Home        bool
	Capture     bool
	LeftStick   bool
	RightStick  bool
	Up, Down    bool
	Left, Right bool
	GL, GR, C   bool

	LeftX, LeftY   int8
	RightX, RightY int8
}

type ReportOptions struct {
	InvertStickY bool
}

func BuildInputReport(counter uint32, input ControllerInput) []byte {
	return BuildInputReportWithOptions(counter, input, ReportOptions{})
}

func BuildInputReportWithOptions(counter uint32, input ControllerInput, options ReportOptions) []byte {
	report := ns2pro.NeutralInputReport()
	binary.LittleEndian.PutUint32(report[1:5], counter)

	setButton(report, input.Y, 0, 0x01)
	setButton(report, input.X, 0, 0x02)
	setButton(report, input.B, 0, 0x04)
	setButton(report, input.A, 0, 0x08)
	setButton(report, input.SRRight, 0, 0x10)
	setButton(report, input.SLRight, 0, 0x20)
	setButton(report, input.R, 0, 0x40)
	setButton(report, input.ZR, 0, 0x80)

	setButton(report, input.Minus, 1, 0x01)
	setButton(report, input.Plus, 1, 0x02)
	setButton(report, input.RightStick, 1, 0x04)
	setButton(report, input.LeftStick, 1, 0x08)
	setButton(report, input.Home, 1, 0x10)
	setButton(report, input.Capture, 1, 0x20)
	setButton(report, input.C, 1, 0x40)

	setButton(report, input.Down, 2, 0x01)
	setButton(report, input.Up, 2, 0x02)
	setButton(report, input.Right, 2, 0x04)
	setButton(report, input.Left, 2, 0x08)
	setButton(report, input.SRLeft, 2, 0x10)
	setButton(report, input.SLLeft, 2, 0x20)
	setButton(report, input.L, 2, 0x40)
	setButton(report, input.ZL, 2, 0x80)

	setButton(report, input.GR, 3, 0x01)
	setButton(report, input.GL, 3, 0x02)

	leftYDir := input.LeftY
	rightYDir := input.RightY
	if options.InvertStickY {
		leftYDir = -leftYDir
		rightYDir = -rightYDir
	}
	leftX, leftY := stickValues(input.LeftX, leftYDir, neutralLeftX, neutralLeftY)
	rightX, rightY := stickValues(input.RightX, rightYDir, neutralRightX, neutralRightY)
	packStick(report[11:14], leftX, leftY)
	packStick(report[14:17], rightX, rightY)

	return report
}

func setButton(report []byte, pressed bool, buttonByte int, mask byte) {
	if pressed {
		report[5+buttonByte] |= mask
	}
}

func stickValues(xDir, yDir int8, neutralX, neutralY uint16) (uint16, uint16) {
	return axisValue(xDir, neutralX), axisValue(yDir, neutralY)
}

func axisValue(dir int8, neutral uint16) uint16 {
	switch {
	case dir < 0:
		return stickMin
	case dir > 0:
		return stickMax
	default:
		return neutral
	}
}

func direction(negative, positive bool) int8 {
	switch {
	case negative && !positive:
		return -1
	case positive && !negative:
		return 1
	default:
		return 0
	}
}

func packStick(dst []byte, x, y uint16) {
	x &= 0x0fff
	y &= 0x0fff
	dst[0] = byte(x)
	dst[1] = byte((x >> 8) | ((y & 0x000f) << 4))
	dst[2] = byte(y >> 4)
}

func unpackStick(src []byte) (uint16, uint16) {
	x := uint16(src[0]) | (uint16(src[1]&0x0f) << 8)
	y := uint16(src[1]>>4) | (uint16(src[2]) << 4)
	return x, y
}

var (
	neutralReport                = ns2pro.NeutralInputReport()
	neutralLeftX, neutralLeftY   = unpackStick(neutralReport[11:14])
	neutralRightX, neutralRightY = unpackStick(neutralReport[14:17])
)

func keymapHelp() string {
	lines := []string{
		"NS2Pro keyboard map:",
		"  J=A, K=B, U=X, I=Y",
		"  Arrow keys=D-pad",
		"  W/A/S/D=left stick, T/F/G/H=right stick",
		"  Q=L, E=R, 1=ZL, 3=ZR",
		"  Enter=Plus, Backspace=Minus, M=Home, N=Capture",
		"  LeftShift=Left Stick, RightShift=Right Stick (--terminal: Z/X)",
		"  [ or O or 8=GL, ] or P or 9=GR, V or C=C",
		"  F1/F2/F3/F4=SL Left/SR Left/SL Right/SR Right (--terminal: 4/5/6/7 also work)",
	}
	return fmt.Sprintf("%s\n", strings.Join(lines, "\n"))
}
