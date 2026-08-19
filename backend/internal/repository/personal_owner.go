package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const personalOwnerConcurrency = 30

// CreatePersonalOwner creates the one initial administrator used by Personal
// Edition. It intentionally bypasses public registration, invitation, billing,
// captcha and email-verification flows. It is valid only for an empty local DB.
func CreatePersonalOwner(ctx context.Context, client *ent.Client, email, password string) (*service.User, error) {
	if client == nil {
		return nil, fmt.Errorf("nil personal ent client")
	}
	email = strings.TrimSpace(email)
	if email == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("owner email and password are required")
	}

	count, err := client.User.Query().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count existing personal users: %w", err)
	}
	if count != 0 {
		return nil, fmt.Errorf("personal owner bootstrap requires an empty user database")
	}

	owner := &service.User{
		Email:       email,
		Role:        service.RoleAdmin,
		Status:      service.StatusActive,
		Balance:     0,
		Concurrency: personalOwnerConcurrency,
		SignupSource: "email",
	}
	if err := owner.SetPassword(password); err != nil {
		return nil, fmt.Errorf("hash personal owner password: %w", err)
	}

	created, err := client.User.Create().
		SetEmail(owner.Email).
		SetPasswordHash(owner.PasswordHash).
		SetRole(owner.Role).
		SetBalance(owner.Balance).
		SetConcurrency(owner.Concurrency).
		SetStatus(owner.Status).
		SetSignupSource(owner.SignupSource).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create personal owner: %w", err)
	}

	if err := ensureEmailAuthIdentityWithClient(ctx, client, created.ID, created.Email, "personal_owner_bootstrap"); err != nil {
		_ = client.User.DeleteOneID(created.ID).Exec(ctx)
		return nil, fmt.Errorf("create personal owner identity: %w", err)
	}

	owner.ID = created.ID
	owner.CreatedAt = created.CreatedAt
	owner.UpdatedAt = created.UpdatedAt
	return owner, nil
}
