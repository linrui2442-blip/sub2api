package personal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	EnvDataDir    = "SUB2_PERSONAL_DATA_DIR"
	EnvSQLitePath = "SUB2_PERSONAL_SQLITE_PATH"
)

// DataDir resolves the durable local data directory for Personal Edition.
// Explicit Personal Edition configuration wins, followed by the upstream
// DATA_DIR override. Windows defaults to LocalAppData so the installation can
// remain fully user-scoped and does not require administrator privileges.
func DataDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv(EnvDataDir)); value != "" {
		return filepath.Clean(value), nil
	}
	if value := strings.TrimSpace(os.Getenv("DATA_DIR")); value != "" {
		return filepath.Clean(value), nil
	}

	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "Sub2 Personal"), nil
		}
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "sub2api-personal"), nil
}

// SQLitePath resolves the durable SQLite database file. Tests and advanced
// operators may override the exact path without changing the general data dir.
func SQLitePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv(EnvSQLitePath)); value != "" {
		return filepath.Clean(value), nil
	}
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "sub2api-personal.db"), nil
}
