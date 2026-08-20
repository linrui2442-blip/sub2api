// Package setup owns the first-run boundary for the local Personal Edition.
package setup

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/personal"
)

const (
	ConfigFileName  = "config.yaml"
	InstallLockFile = ".installed"
)

// AdminConfig is the one-time owner credential accepted by the local wizard.
// Database and Redis connection settings deliberately do not exist here.
type AdminConfig struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

var installMutex sync.Mutex

// GetDataDir returns the local user-scoped runtime directory. DATA_DIR remains
// an explicit portability override for Windows portable installations.
func GetDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("DATA_DIR")); dir != "" {
		return filepath.Clean(dir)
	}
	if dir, err := personal.DataDir(); err == nil && dir != "" {
		return dir
	}
	return "."
}

func GetConfigFilePath() string  { return filepath.Join(GetDataDir(), ConfigFileName) }
func GetInstallLockPath() string { return filepath.Join(GetDataDir(), InstallLockFile) }

func skipSetupEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SKIP_SETUP"))) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// NeedsSetup is intentionally fail-safe: either durable marker blocks another
// owner bootstrap attempt.
func NeedsSetup() bool {
	if skipSetupEnabled() {
		return false
	}
	if _, err := os.Stat(GetConfigFilePath()); !os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(GetInstallLockPath()); !os.IsNotExist(err) {
		return false
	}
	return true
}

func createInstallLock() error {
	if err := os.MkdirAll(GetDataDir(), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	return os.WriteFile(GetInstallLockPath(), []byte(fmt.Sprintf("installed_at=%s\n", time.Now().UTC().Format(time.RFC3339))), 0o400)
}

func validateEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil && len(email) <= 254
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}
	return nil
}
