//go:build windows

package personal

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

const desktopInstanceMutexName = `Local\Sub2APIPersonalDesktop`

// AcquireDesktopInstance ensures that only one Windows tray process owns the
// Personal desktop shell for the current interactive session. The caller must
// release the returned function when the process exits.
func AcquireDesktopInstance() (release func(), alreadyRunning bool, err error) {
	return acquireDesktopInstanceMutex(desktopInstanceMutexName)
}

func acquireDesktopInstanceMutex(name string) (release func(), alreadyRunning bool, err error) {
	mutexName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, fmt.Errorf("encode desktop instance mutex name: %w", err)
	}

	handle, createErr := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, true, nil
	}
	if createErr != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, false, fmt.Errorf("create desktop instance mutex: %w", createErr)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = windows.CloseHandle(handle)
		})
	}, false, nil
}
