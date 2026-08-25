package personal

import "sync"

// DesktopCallbacks connects the Windows tray shell to the existing Personal
// Gateway lifecycle. The shell deliberately owns no account, OAuth, routing,
// or persistence logic.
type DesktopCallbacks struct {
	Running        func() bool
	StartGateway   func() error
	StopGateway    func() error
	RestartGateway func() error
	OpenManagement func()
	OpenLogs       func() error
}

type desktopExitHandler struct {
	once sync.Once
	stop func() error
	quit func()
}

func newDesktopExitHandler(stop func() error, quit func()) *desktopExitHandler {
	return &desktopExitHandler{stop: stop, quit: quit}
}

func (h *desktopExitHandler) Exit() {
	h.once.Do(func() {
		if h.stop != nil {
			_ = h.stop()
		}
		if h.quit != nil {
			h.quit()
		}
	})
}
