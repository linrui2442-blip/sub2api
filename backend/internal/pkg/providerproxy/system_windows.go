//go:build windows

package providerproxy

import (
	"net/url"

	"golang.org/x/sys/windows/registry"
)

func WindowsSystemProxy(target *url.URL) (*url.URL, bool) {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, false
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, false
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil, false
	}
	override, _, _ := key.GetStringValue("ProxyOverride")
	return ResolveWindowsProxyConfig(target, WindowsProxyConfig{
		Enabled: true, Server: server, Override: override,
	})
}
