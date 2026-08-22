//go:build !windows

package repository

import "net/url"

func windowsSystemProxy(*url.URL) (*url.URL, bool) {
	return nil, false
}
