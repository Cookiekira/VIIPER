package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

type terminalInput struct {
	mu            sync.Mutex
	pulse         time.Duration
	deadline      map[inputControl]time.Time
	oldState      *term.State
	done          chan struct{}
	interrupt     chan struct{}
	interruptOnce sync.Once
}

type inputControl uint8

const (
	controlA inputControl = iota
	controlB
	controlX
	controlY
	controlL
	controlR
	controlZL
	controlZR
	controlSLLeft
	controlSRLeft
	controlSLRight
	controlSRRight
	controlPlus
	controlMinus
	controlHome
	controlCapture
	controlLeftStick
	controlRightStick
	controlUp
	controlDown
	controlLeft
	controlRight
	controlGL
	controlGR
	controlC
	controlLeftStickUp
	controlLeftStickDown
	controlLeftStickLeft
	controlLeftStickRight
	controlRightStickUp
	controlRightStickDown
	controlRightStickLeft
	controlRightStickRight
)

func startTerminalInput(ctx context.Context, pulse time.Duration) (*terminalInput, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("stdin is not a terminal")
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	t := &terminalInput{
		pulse:     pulse,
		deadline:  make(map[inputControl]time.Time),
		oldState:  oldState,
		done:      make(chan struct{}),
		interrupt: make(chan struct{}),
	}
	go t.readLoop(ctx)
	return t, nil
}

func (t *terminalInput) Close() {
	_ = term.Restore(int(os.Stdin.Fd()), t.oldState)
	select {
	case <-t.done:
	default:
		close(t.done)
	}
}

func (t *terminalInput) Input() ControllerInput {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for control, until := range t.deadline {
		if !until.After(now) {
			delete(t.deadline, control)
		}
	}

	var input ControllerInput
	input.A = t.active(controlA)
	input.B = t.active(controlB)
	input.X = t.active(controlX)
	input.Y = t.active(controlY)
	input.L = t.active(controlL)
	input.R = t.active(controlR)
	input.ZL = t.active(controlZL)
	input.ZR = t.active(controlZR)
	input.SLLeft = t.active(controlSLLeft)
	input.SRLeft = t.active(controlSRLeft)
	input.SLRight = t.active(controlSLRight)
	input.SRRight = t.active(controlSRRight)
	input.Plus = t.active(controlPlus)
	input.Minus = t.active(controlMinus)
	input.Home = t.active(controlHome)
	input.Capture = t.active(controlCapture)
	input.LeftStick = t.active(controlLeftStick)
	input.RightStick = t.active(controlRightStick)
	input.Up = t.active(controlUp)
	input.Down = t.active(controlDown)
	input.Left = t.active(controlLeft)
	input.Right = t.active(controlRight)
	input.GL = t.active(controlGL)
	input.GR = t.active(controlGR)
	input.C = t.active(controlC)
	input.LeftX = direction(t.active(controlLeftStickLeft), t.active(controlLeftStickRight))
	input.LeftY = direction(t.active(controlLeftStickUp), t.active(controlLeftStickDown))
	input.RightX = direction(t.active(controlRightStickLeft), t.active(controlRightStickRight))
	input.RightY = direction(t.active(controlRightStickUp), t.active(controlRightStickDown))
	return input
}

func terminalInterrupt(t *terminalInput) <-chan struct{} {
	if t == nil {
		return nil
	}
	return t.interrupt
}

func (t *terminalInput) active(control inputControl) bool {
	return t.deadline[control].After(time.Now())
}

func (t *terminalInput) readLoop(ctx context.Context) {
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), t.oldState)
	}()

	buf := make([]byte, 1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}
		t.handleByte(buf[0])
	}
}

func (t *terminalInput) handleByte(b byte) {
	if b == 0x03 {
		t.interruptOnce.Do(func() {
			close(t.interrupt)
		})
		return
	}
	if b == 0x1b {
		if controls := readEscapeControls(); len(controls) > 0 {
			t.press(controls...)
		}
		return
	}
	if controls := terminalControlsForByte(b); len(controls) > 0 {
		t.press(controls...)
	}
}

func (t *terminalInput) press(controls ...inputControl) {
	t.mu.Lock()
	defer t.mu.Unlock()
	until := time.Now().Add(t.pulse)
	for _, control := range controls {
		t.deadline[control] = until
	}
}

func terminalControlsForByte(b byte) []inputControl {
	switch b {
	case 'j', 'J':
		return []inputControl{controlA}
	case 'k', 'K':
		return []inputControl{controlB}
	case 'u', 'U':
		return []inputControl{controlX}
	case 'i', 'I':
		return []inputControl{controlY}
	case 'w', 'W':
		return []inputControl{controlLeftStickUp}
	case 'a', 'A':
		return []inputControl{controlLeftStickLeft}
	case 's', 'S':
		return []inputControl{controlLeftStickDown}
	case 'd', 'D':
		return []inputControl{controlLeftStickRight}
	case 't', 'T':
		return []inputControl{controlRightStickUp}
	case 'f', 'F':
		return []inputControl{controlRightStickLeft}
	case 'g', 'G':
		return []inputControl{controlRightStickDown}
	case 'h', 'H':
		return []inputControl{controlRightStickRight}
	case 'q', 'Q':
		return []inputControl{controlL}
	case 'e', 'E':
		return []inputControl{controlR}
	case '1':
		return []inputControl{controlZL}
	case '3':
		return []inputControl{controlZR}
	case '\r', '\n':
		return []inputControl{controlPlus}
	case '\b', 0x7f:
		return []inputControl{controlMinus}
	case 'm', 'M':
		return []inputControl{controlHome}
	case 'n', 'N':
		return []inputControl{controlCapture}
	case 'c', 'C':
		return []inputControl{controlC}
	case 'z', 'Z':
		return []inputControl{controlLeftStick}
	case 'x', 'X':
		return []inputControl{controlRightStick}
	case '[':
		return []inputControl{controlGL}
	case ']':
		return []inputControl{controlGR}
	case 'v', 'V':
		return []inputControl{controlC}
	case 'o', 'O', '8':
		return []inputControl{controlGL}
	case 'p', 'P', '9':
		return []inputControl{controlGR}
	case '4':
		return []inputControl{controlSLLeft}
	case '5':
		return []inputControl{controlSRLeft}
	case '6':
		return []inputControl{controlSLRight}
	case '7':
		return []inputControl{controlSRRight}
	default:
		return nil
	}
}

func readEscapeControls() []inputControl {
	var seq [4]byte
	_ = os.Stdin.SetReadDeadline(time.Now().Add(15 * time.Millisecond))
	n, _ := os.Stdin.Read(seq[:])
	_ = os.Stdin.SetReadDeadline(time.Time{})
	if n < 2 {
		return nil
	}
	switch string(seq[:n]) {
	case "[A":
		return []inputControl{controlUp}
	case "[B":
		return []inputControl{controlDown}
	case "[C":
		return []inputControl{controlRight}
	case "[D":
		return []inputControl{controlLeft}
	case "OP", "[11~":
		return []inputControl{controlSLLeft}
	case "OQ", "[12~":
		return []inputControl{controlSRLeft}
	case "OR", "[13~":
		return []inputControl{controlSLRight}
	case "OS", "[14~":
		return []inputControl{controlSRRight}
	default:
		return nil
	}
}
