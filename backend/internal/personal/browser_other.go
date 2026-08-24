//go:build !windows

package personal

func openBrowser(string) error {
	return nil
}
