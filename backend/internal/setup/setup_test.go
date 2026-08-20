package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsSetupUsesLocalMarkers(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	if !NeedsSetup() {
		t.Fatal("empty local runtime should need owner setup")
	}
	if err := createInstallLock(); err != nil {
		t.Fatalf("createInstallLock: %v", err)
	}
	if NeedsSetup() {
		t.Fatal("install lock must prevent a second owner bootstrap")
	}
}

func TestNeedsSetupHonorsExistingConfigAndExplicitBypass(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, ConfigFileName), []byte("server: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if NeedsSetup() {
		t.Fatal("existing local config must block setup")
	}
	if err := os.Remove(filepath.Join(dataDir, ConfigFileName)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKIP_SETUP", "yes")
	if NeedsSetup() {
		t.Fatal("explicit setup bypass must be respected")
	}
}
