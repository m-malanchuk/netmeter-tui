package app

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

func initializeScreen() (tcell.Screen, error) {
	screen, createErr := tcell.NewScreen()
	if createErr == nil {
		if initErr := screen.Init(); initErr == nil {
			return screen, nil
		} else {
			createErr = initErr
		}
	}

	// Some containers and IDE terminals expose usable standard streams without
	// providing a controlling /dev/tty. Try that backend before giving up.
	stdio, ttyErr := tcell.NewStdIoTty()
	if ttyErr == nil {
		fallback, fallbackCreateErr := tcell.NewTerminfoScreenFromTty(stdio)
		if fallbackCreateErr == nil {
			if fallbackInitErr := fallback.Init(); fallbackInitErr == nil {
				return fallback, nil
			} else {
				createErr = fmt.Errorf("%w; initialize standard-stream fallback: %v", createErr, fallbackInitErr)
			}
		} else {
			createErr = fmt.Errorf("%w; create standard-stream fallback: %v", createErr, fallbackCreateErr)
		}
	}
	return nil, fmt.Errorf("initialize terminal screen: %w", createErr)
}
