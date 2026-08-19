package setup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/spf13/viper"
	_ "modernc.org/sqlite"
)

func TestPersonalInstallCreatesLocalOwnerAndSecrets(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "personal.db")
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_DATA_DIR", dataDir)
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", dbPath)
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SKIP_SETUP", "")
	if !personal.PrepareEnvironment("personal") {
		t.Fatal("Personal runtime must be enabled")
	}
	if !NeedsSetup() {
		t.Fatal("fresh Personal data directory must require setup")
	}

	const (
		email    = "owner@example.com"
		password = "PersonalPass123!"
	)
	if err := InstallPersonal(email, password); err != nil {
		t.Fatalf("install Personal Edition: %v", err)
	}
	if NeedsSetup() {
		t.Fatal("completed Personal install must not require setup")
	}
	if _, err := os.Stat(GetInstallLockPath()); err != nil {
		t.Fatalf("installation lock missing: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open installed personal db: %v", err)
	}
	defer func() { _ = db.Close() }()

	var (
		storedEmail string
		passwordHash string
		role string
		status string
		concurrency int
	)
	if err := db.QueryRow(`SELECT email, password_hash, role, status, concurrency FROM users LIMIT 1`).
		Scan(&storedEmail, &passwordHash, &role, &status, &concurrency); err != nil {
		t.Fatalf("read Personal owner: %v", err)
	}
	owner := service.User{Email: storedEmail, PasswordHash: passwordHash, Role: role, Status: status, Concurrency: concurrency}
	if owner.Email != email || !owner.IsAdmin() || !owner.IsActive() {
		t.Fatalf("unexpected Personal owner: %+v", owner)
	}
	if !owner.CheckPassword(password) {
		t.Fatal("Personal owner password hash does not validate")
	}
	if owner.Concurrency != personalOwnerConcurrency {
		t.Fatalf("owner concurrency = %d, want %d", owner.Concurrency, personalOwnerConcurrency)
	}

	var secretCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM security_secrets WHERE key IN ('jwt_secret', 'personal_totp_encryption_key')`).Scan(&secretCount); err != nil {
		t.Fatalf("read Personal bootstrap secrets: %v", err)
	}
	if secretCount != 2 {
		t.Fatalf("expected JWT + TOTP persistent secrets, got %d", secretCount)
	}
}
