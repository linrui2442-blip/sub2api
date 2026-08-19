//go:build windows

package personal

import "os/exec"

func openBrowser(target string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
