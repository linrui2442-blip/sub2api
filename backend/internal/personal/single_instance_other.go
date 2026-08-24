//go:build !windows

package personal

// AcquireDesktopInstance is a no-op on platforms without the Windows tray.
func AcquireDesktopInstance() (release func(), alreadyRunning bool, err error) {
	return func() {}, false, nil
}
