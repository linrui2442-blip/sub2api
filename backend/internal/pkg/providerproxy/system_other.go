//go:build !windows

package providerproxy

import "net/url"

func WindowsSystemProxy(*url.URL) (*url.URL, bool) {
	return nil, false
}
