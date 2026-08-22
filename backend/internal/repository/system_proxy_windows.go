//go:build windows

package repository

import (
	"net/url"

	"golang.org/x/sys/windows/registry"
)

func windowsSystemProxy(target *url.URL) (*url.URL, bool) {
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
	return resolveWindowsProxyConfig(target, windowsProxyConfig{
		Enabled: true, Server: server, Override: override,
	})
}
