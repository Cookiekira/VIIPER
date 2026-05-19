//go:build windows

package main

import "golang.org/x/sys/windows"

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

const (
	vkBack   = 0x08
	vkReturn = 0x0d
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkF1     = 0x70
	vkF2     = 0x71
	vkF3     = 0x72
	vkF4     = 0x73
	vkLShift = 0xa0
	vkRShift = 0xa1
	vkOem4   = 0xdb // [
	vkOem6   = 0xdd // ]
)

func pollKeyboardInput() (ControllerInput, error) {
	var input ControllerInput

	input.A = keyDown('J')
	input.B = keyDown('K')
	input.X = keyDown('U')
	input.Y = keyDown('I')

	input.Up = keyDown(vkUp)
	input.Down = keyDown(vkDown)
	input.Left = keyDown(vkLeft)
	input.Right = keyDown(vkRight)

	input.LeftX = direction(keyDown('A'), keyDown('D'))
	input.LeftY = direction(keyDown('W'), keyDown('S'))
	input.RightX = direction(keyDown('F'), keyDown('H'))
	input.RightY = direction(keyDown('T'), keyDown('G'))

	input.L = keyDown('Q')
	input.R = keyDown('E')
	input.ZL = keyDown('1')
	input.ZR = keyDown('3')

	input.Plus = keyDown(vkReturn)
	input.Minus = keyDown(vkBack)
	input.Home = keyDown('M')
	input.Capture = keyDown('N')
	input.LeftStick = keyDown(vkLShift)
	input.RightStick = keyDown(vkRShift)

	input.GL = keyDown(vkOem4)
	input.GR = keyDown(vkOem6)
	input.C = keyDown('V') || keyDown('C')

	input.GL = input.GL || keyDown('O') || keyDown('8')
	input.GR = input.GR || keyDown('P') || keyDown('9')

	input.SLLeft = keyDown(vkF1)
	input.SRLeft = keyDown(vkF2)
	input.SLRight = keyDown(vkF3)
	input.SRRight = keyDown(vkF4)

	return input, nil
}

func keyDown(vk int) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}
