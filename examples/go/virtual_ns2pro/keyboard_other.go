//go:build !windows

package main

import "fmt"

func pollKeyboardInput() (ControllerInput, error) {
	return ControllerInput{}, fmt.Errorf("--keyboard is only supported on Windows")
}
