package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/personal"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

const personalSecuritySecretKeyTOTP = "personal_totp_encryption_key"

// initPersonalEnt creates the single-file SQLite backend used by Personal
// Edition. Upstream PostgreSQL migrations intentionally do not run here.
// Generated Ent schemas cover application entities; a deliberately small
// Personal infrastructure bootstrap covers hand-written migration tables that
// the private GPT/Gemini gateway runtime still requires.
func initPersonalEnt(cfg *config.Config) (*ent.Client, *sql.DB, error) {
	dbPath, err := personal.SQLitePath()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create personal data dir: %w", err)
	}

	drv, db, err := openPersonalSQLite(dbPath)
	if err != nil {
		return nil, nil, err
	}

	migrationCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(migrationCtx); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("create personal sqlite schema: %w", err)
	}
	if err := ensurePersonalSQLiteInfrastructure(migrationCtx, db); err != nil {
		_ = client.Close()
		return nil, nil, err
	}

	// Reuse the upstream JWT bootstrap contract and persist a separate TOTP
	// encryption key in the same local database. config.Load generates an
	// ephemeral TOTP key when none is configured; Personal Edition replaces it
	// with this durable value before any encrypted 2FA secret can be used.
	if err := ensureBootstrapSecrets(migrationCtx, client, cfg); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if err := ensurePersonalTOTPSecret(migrationCtx, client, cfg); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("validate config after personal secret bootstrap: %w", err)
	}

	// Personal Edition currently inherits upstream SIMPLE semantics underneath
	// its private route policy, including the default platform groups.
	if cfg.RunMode == config.RunModeSimple {
		seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer seedCancel()
		if err := ensureSimpleModeDefaultGroups(seedCtx, client); err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		if err := ensureSimpleModeAdminConcurrency(seedCtx, client); err != nil {
			_ = client.Close()
			return nil, nil, err
		}
	}

	return client, db, nil
}

func ensurePersonalTOTPSecret(ctx context.Context, client *ent.Client, cfg *config.Config) error {
	secret, _, err := getOrCreateGeneratedSecuritySecret(ctx, client, personalSecuritySecretKeyTOTP, 32)
	if err != nil {
		return fmt.Errorf("ensure personal TOTP encryption key: %w", err)
	}
	cfg.Totp.EncryptionKey = secret
	cfg.Totp.EncryptionKeyConfigured = true
	return nil
}

func openPersonalSQLite(path string) (*entsql.Driver, *sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, fmt.Errorf("open personal sqlite: %w", err)
	}

	// A Personal Edition instance is intentionally a single process with low
	// concurrency. One database connection avoids cross-connection PRAGMA drift
	// and dramatically reduces SQLITE_BUSY edge cases on Windows.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range pragmas {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("configure personal sqlite (%s): %w", statement, err)
		}
	}

	drv := entsql.OpenDB(dialect.SQLite, db)
	return drv, db, nil
}
