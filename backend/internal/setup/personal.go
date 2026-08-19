package setup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

// InstallPersonal initializes the local SQLite-backed Personal Edition and
// creates its single owner/admin account. It intentionally does not accept or
// test PostgreSQL/Redis settings: those runtime dependencies do not exist in
// Personal Edition.
func InstallPersonal(email, password string) error {
	if !personal.Enabled() {
		return fmt.Errorf("personal installer is only available in Personal Edition")
	}
	if !NeedsSetup() {
		return fmt.Errorf("system is already installed, re-installation is not allowed")
	}

	email = strings.TrimSpace(email)
	if !validateEmail(email) {
		return fmt.Errorf("invalid admin email format")
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load personal bootstrap config: %w", err)
	}
	client, _, err := repository.InitEnt(cfg)
	if err != nil {
		return fmt.Errorf("initialize personal sqlite database: %w", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner, err := repository.CreatePersonalOwner(ctx, client, email, password)
	if err != nil {
		return fmt.Errorf("create personal owner: %w", err)
	}

	if err := createInstallLock(); err != nil {
		// Avoid leaving a half-installed database that cannot be retried. The
		// bootstrap-only owner can be safely removed because no normal gateway
		// process has started yet.
		_ = client.User.DeleteOneID(owner.ID).Exec(ctx)
		return fmt.Errorf("create personal installation lock: %w", err)
	}
	return nil
}
