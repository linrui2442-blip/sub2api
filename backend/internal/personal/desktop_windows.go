//go:build windows

package personal

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gogpu/systray"
	"golang.org/x/sys/windows/registry"
)

const (
	startupRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupValueName    = "Sub2API Personal"
)

const trayIconBase64 = "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAKuSURBVFhH7VehjttAEA05yZJ1chU7lx65lpzUFhUW9hMKDxYeLCw8dvBgYECza6nkSJVdI8PAwsDCgwcDt5r12tl5u7aTtFJJnzRSFM/Mzsy+mbEnk/84EbNq9T5X5cdWputvH1Dnr+JVvUxmavU517LMldwVWpqY5EqoXK1us3r5An2cjFyLT4UWv/CwEXnO1+IrBY7+DgZlUVTyMeL8cFFyO1fla/Q9CjIi48BhJ7YidSeDuvKZeIJn9MJmHnFId59X4oEIiDYECjpX4o4ORFsbhBZv0CaKnrJvZj++v0TdGEiv0HIZ+FByO8qJYi1uAkMtl6OGERAJ0RdVEPU60CG5Fk+YefTw9Mpk83cg1yZNJsZXK3S5YAEoueslJfUvKsfKnkzfRg7fy3mWdkFQ8CGfygX36EBDhClW8h514pmj8Eo0c8RLTIsn7tQxHydcjLV+9n6m+CybFrwK0BnB6KY/WPZKbpmCw/6QK5NM+H1PkktzPnfPLy7NmfccO4vGOrMNyqSEYgoOZ9m1V+pIED1w88H3fwcKnIDUekyhQ2HSNksQvBIfo/6x/2nrMQUffqljAuUnYAUCgtOshgg3TCGK1KQXPYEgB4J5sLplrtz49AMwR+90VhneirjOiXPc2BJR/oQgvjAF/wCvzXz4JE3TJgBaXjx7uYtOV7wnGhhckRMwIB1wow0gMuAemV2LZp3CMIJ24W04JE2LIrldYmH5W9C2QgNystcYIF4nzf03L65BQtH50oHIGNmIQSV6d4Ljhh1sio9fu9x6XmYYaCxj5E42Y69WTdZw59FKjoCUe4Jw74PlgqpCmVryVvI+XLt7CSp4CGwlItdxjNiy4+I5Bg0nZImOD5Q6WLunoqnG8FdRK/braKjV/hTtvbvBVdP9299rcXP0+P7X+A0dH/RHYs58YgAAAABJRU5ErkJggg=="

func DesktopSupported() bool { return true }

// RunDesktop hosts the native Windows tray. Starting and stopping the Gateway
// delegates to the same application graph used by foreground mode.
func RunDesktop(callbacks DesktopCallbacks) error {
	icon, err := base64.StdEncoding.DecodeString(trayIconBase64)
	if err != nil {
		return fmt.Errorf("decode tray icon: %w", err)
	}

	tray := systray.New()
	tray.SetIcon(icon)

	var actionMu sync.Mutex
	var exiting bool
	var rebuild func()
	rebuild = func() {
		running := callbacks.Running != nil && callbacks.Running()
		startupEnabled, startupErr := personalStartupEnabled()
		status := "已停止"
		if running {
			status = "运行中"
		}
		tray.SetTooltip("Sub2API Personal - Gateway " + status)

		menu := systray.NewMenu()
		menu.Add("Gateway 状态："+status, nil)
		menu.Add("打开管理页面", func() {
			if callbacks.OpenManagement != nil {
				callbacks.OpenManagement()
			}
		})
		if running {
			menu.Add("停止 Gateway", func() {
				go runTrayAction(&actionMu, tray, "停止 Gateway", callbacks.StopGateway, rebuild)
			})
			menu.Add("重启 Gateway", func() {
				go runTrayAction(&actionMu, tray, "重启 Gateway", callbacks.RestartGateway, rebuild)
			})
		} else {
			menu.Add("启动 Gateway", func() {
				go runTrayAction(&actionMu, tray, "启动 Gateway", callbacks.StartGateway, rebuild)
			})
		}
		menu.Add("查看日志", func() {
			if callbacks.OpenLogs != nil {
				go runTrayAction(&actionMu, tray, "打开日志", callbacks.OpenLogs, rebuild)
			}
		})
		startupLabel := "开机启动：已关闭"
		if startupEnabled {
			startupLabel = "开机启动：已开启"
		} else if startupErr != nil {
			startupLabel = "开机启动：状态未知"
		}
		menu.Add(startupLabel, func() {
			go runTrayAction(&actionMu, tray, "更新开机启动", func() error {
				enabled, err := personalStartupEnabled()
				if err != nil {
					return err
				}
				return setPersonalStartupEnabled(!enabled)
			}, rebuild)
		})
		menu.AddSeparator()
		menu.Add("退出 Sub2API", func() {
			go func() {
				actionMu.Lock()
				defer actionMu.Unlock()
				if exiting {
					return
				}
				exiting = true
				if callbacks.StopGateway != nil {
					_ = callbacks.StopGateway()
				}
				tray.Remove()
			}()
		})
		tray.SetMenu(menu)
	}

	rebuild()
	tray.OnDoubleClick(func() {
		if callbacks.OpenManagement != nil {
			callbacks.OpenManagement()
		}
	})
	tray.Show()
	return tray.Run()
}

func personalStartupEnabled() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("resolve executable: %w", err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, startupRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("open startup registry: %w", err)
	}
	defer key.Close()
	command, _, err := key.GetStringValue(startupValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("read startup registry: %w", err)
	}
	return startupCommandMatches(command, executable), nil
}

func setPersonalStartupEnabled(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupRegistryPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open startup registry: %w", err)
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue(startupValueName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("remove startup entry: %w", err)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if err := key.SetStringValue(startupValueName, startupCommand(executable)); err != nil {
		return fmt.Errorf("write startup entry: %w", err)
	}
	return nil
}

func startupCommand(executable string) string {
	return `"` + executable + `"`
}

func startupCommandMatches(command, executable string) bool {
	command = strings.TrimSpace(command)
	if len(command) >= 2 && command[0] == '"' && command[len(command)-1] == '"' {
		command = command[1 : len(command)-1]
	}
	return strings.EqualFold(filepath.Clean(command), filepath.Clean(executable))
}

func runTrayAction(mu *sync.Mutex, tray *systray.SystemTray, label string, action func() error, rebuild func()) {
	if action == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if err := action(); err != nil {
		tray.ShowNotification("Sub2API Personal", label+"失败："+err.Error())
	}
	rebuild()
}

// OpenPersonalLogs opens the durable Personal log directory in Explorer.
func OpenPersonalLogs() error {
	dataDir, err := DataDir()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return fmt.Errorf("create logs directory: %w", err)
	}
	return exec.Command("explorer.exe", logsDir).Start()
}
