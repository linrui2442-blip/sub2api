package personal

import (
	"net"
	"net/url"
	"strings"
	"time"
)

// OpenLocalBrowser opens the Personal Edition web UI shortly after a listener
// starts. The delay avoids a race where Windows launches the browser before the
// local HTTP server has begun accepting connections.
func OpenLocalBrowser(addr, path string) {
	target := localHTTPURL(addr, path)
	if target == "" {
		return
	}
	go func() {
		time.Sleep(350 * time.Millisecond)
		_ = openBrowser(target)
	}()
}

func localHTTPURL(addr, path string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   path,
	}).String()
}
