package main

import (
	"encoding/binary"
	"testing"

	"github.com/Alia5/VIIPER/device/ns2pro"
	"github.com/stretchr/testify/require"
)

func TestBuildInputReportSetsCounterAndFaceButtons(t *testing.T) {
	report := BuildInputReport(0x11223344, ControllerInput{
		A: true,
		B: true,
		X: true,
		Y: true,
	})

	require.Len(t, report, ns2pro.InputReportSize)
	require.Equal(t, byte(ns2pro.InputReportID), report[0])
	require.Equal(t, uint32(0x11223344), binary.LittleEndian.Uint32(report[1:5]))
	require.Equal(t, byte(0x0f), report[5])
}

func TestBuildInputReportSetsShouldersSystemAndExtraButtons(t *testing.T) {
	report := BuildInputReport(1, ControllerInput{
		L: true, R: true, ZL: true, ZR: true,
		SLLeft: true, SRLeft: true, SLRight: true, SRRight: true,
		Plus: true, Minus: true, Home: true, Capture: true,
		LeftStick: true, RightStick: true,
		GL: true, GR: true, C: true,
	})

	require.Equal(t, byte(0xf0), report[5])
	require.Equal(t, byte(0x7f), report[6])
	require.Equal(t, byte(0xf0), report[7])
	require.Equal(t, byte(0x03), report[8])
}

func TestBuildInputReportSetsDpadBits(t *testing.T) {
	report := BuildInputReport(1, ControllerInput{
		Up: true, Down: true, Left: true, Right: true,
	})

	require.Equal(t, byte(0x0f), report[7])
}

func TestBuildInputReportPacksStickDirections(t *testing.T) {
	report := BuildInputReport(1, ControllerInput{
		LeftX:  -1,
		LeftY:  1,
		RightX: 1,
		RightY: -1,
	})

	leftX, leftY := unpackStick(report[11:14])
	rightX, rightY := unpackStick(report[14:17])

	require.Equal(t, uint16(stickMin), leftX)
	require.Equal(t, uint16(stickMax), leftY)
	require.Equal(t, uint16(stickMax), rightX)
	require.Equal(t, uint16(stickMin), rightY)
}

func TestBuildInputReportCanInvertStickY(t *testing.T) {
	report := BuildInputReportWithOptions(1, ControllerInput{
		LeftY:  -1,
		RightY: 1,
	}, ReportOptions{InvertStickY: true})

	_, leftY := unpackStick(report[11:14])
	_, rightY := unpackStick(report[14:17])

	require.Equal(t, uint16(stickMax), leftY)
	require.Equal(t, uint16(stickMin), rightY)
}

func TestBuildInputReportKeepsNeutralSticksByDefault(t *testing.T) {
	report := BuildInputReport(1, ControllerInput{})

	leftX, leftY := unpackStick(report[11:14])
	rightX, rightY := unpackStick(report[14:17])

	require.Equal(t, neutralLeftX, leftX)
	require.Equal(t, neutralLeftY, leftY)
	require.Equal(t, neutralRightX, rightX)
	require.Equal(t, neutralRightY, rightY)
}

func TestHoldAUsesReport05AButtonBit(t *testing.T) {
	report := BuildInputReport(1, ControllerInput{A: true})

	require.Equal(t, byte(0x08), report[5]&0x08)
	require.Equal(t, byte(0x00), report[3]&0x01)
}

func TestTerminalExtraButtonAliases(t *testing.T) {
	require.Equal(t, []inputControl{controlC}, terminalControlsForByte('c'))
	require.Equal(t, []inputControl{controlC}, terminalControlsForByte('C'))
	require.Equal(t, []inputControl{controlGL}, terminalControlsForByte('o'))
	require.Equal(t, []inputControl{controlGL}, terminalControlsForByte('8'))
	require.Equal(t, []inputControl{controlGR}, terminalControlsForByte('p'))
	require.Equal(t, []inputControl{controlGR}, terminalControlsForByte('9'))
}
