package personal

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
