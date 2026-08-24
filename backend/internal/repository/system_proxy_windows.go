//go:build windows

package repository

import (
	"net/url"

	"github.com/Wei-Shaw/sub2api/internal/pkg/providerproxy"
)

func windowsSystemProxy(target *url.URL) (*url.URL, bool) {
	return providerproxy.WindowsSystemProxy(target)
}
