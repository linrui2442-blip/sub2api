//go:build windows

package personal

import "testing"

func TestStartupCommandMatchesCurrentExecutable(t *testing.T) {
	path := `C:\Program Files\Sub2API Personal\sub2api-personal.exe`
	command := startupCommand(path)
	if command != `"C:\Program Files\Sub2API Personal\sub2api-personal.exe"` {
		t.Fatalf("startupCommand() = %q", command)
	}
	if !startupCommandMatches(command, path) {
		t.Fatal("quoted startup command should match executable")
	}
	if !startupCommandMatches(`  "c:\program files\sub2api personal\SUB2API-PERSONAL.EXE"  `, path) {
		t.Fatal("startup command comparison should be case-insensitive and trim whitespace")
	}
	if startupCommandMatches(`"C:\old\sub2api-personal.exe"`, path) {
		t.Fatal("stale startup command must not be reported as enabled")
	}
}
