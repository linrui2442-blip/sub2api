//go:build !windows

package personal

// DesktopSupported reports whether this build has the native Personal tray.
func DesktopSupported() bool { return false }

// RunDesktop is unavailable outside Windows. Non-Windows builds keep the
// traditional foreground server lifecycle.
func RunDesktop(DesktopCallbacks) error { return nil }
