package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// personalRefreshTokenCache persists browser refresh-session state in the same
// local SQLite file as users and API keys. Provider OAuth credentials remain in
// the account repository; this table only holds hashed UI refresh tokens.
type personalRefreshTokenCache struct{ db *sql.DB }

func newPersonalRefreshTokenCache(db *sql.DB) service.RefreshTokenCache {
	return &personalRefreshTokenCache{db: db}
}

func (c *personalRefreshTokenCache) StoreRefreshToken(ctx context.Context, tokenHash string, data *service.RefreshTokenData, _ time.Duration) error {
	if c == nil || c.db == nil {
		return errors.New("personal refresh token store unavailable")
	}
	if data == nil {
		return errors.New("nil refresh token data")
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal refresh token data: %w", err)
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO personal_refresh_tokens (token_hash, user_id, family_id, payload, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(token_hash) DO UPDATE SET user_id=excluded.user_id, family_id=excluded.family_id, payload=excluded.payload, expires_at=excluded.expires_at`,
		tokenHash, data.UserID, data.FamilyID, string(payload), data.ExpiresAt.UTC())
	return err
}

func (c *personalRefreshTokenCache) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("personal refresh token store unavailable")
	}
	var payload string
	var expiresAt time.Time
	err := c.db.QueryRowContext(ctx, `SELECT payload, expires_at FROM personal_refresh_tokens WHERE token_hash = ?`, tokenHash).Scan(&payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(expiresAt) {
		_ = c.DeleteRefreshToken(ctx, tokenHash)
		return nil, service.ErrRefreshTokenNotFound
	}
	var data service.RefreshTokenData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return nil, fmt.Errorf("unmarshal refresh token data: %w", err)
	}
	return &data, nil
}

func (c *personalRefreshTokenCache) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM personal_refresh_tokens WHERE token_hash = ?`, tokenHash)
	return err
}
func (c *personalRefreshTokenCache) DeleteUserRefreshTokens(ctx context.Context, userID int64) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM personal_refresh_tokens WHERE user_id = ?`, userID)
	return err
}
func (c *personalRefreshTokenCache) DeleteTokenFamily(ctx context.Context, familyID string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM personal_refresh_tokens WHERE family_id = ?`, familyID)
	return err
}

// StoreRefreshToken atomically records both ownership relations. These legacy
// set operations remain idempotent compatibility calls for AuthService.
func (c *personalRefreshTokenCache) AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, _ time.Duration) error {
	_, err := c.db.ExecContext(ctx, `UPDATE personal_refresh_tokens SET user_id = ? WHERE token_hash = ?`, userID, tokenHash)
	return err
}
func (c *personalRefreshTokenCache) AddToFamilyTokenSet(ctx context.Context, familyID, tokenHash string, _ time.Duration) error {
	_, err := c.db.ExecContext(ctx, `UPDATE personal_refresh_tokens SET family_id = ? WHERE token_hash = ?`, familyID, tokenHash)
	return err
}

func (c *personalRefreshTokenCache) GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error) {
	return c.tokenHashes(ctx, `SELECT token_hash FROM personal_refresh_tokens WHERE user_id = ? AND expires_at > ?`, userID)
}
func (c *personalRefreshTokenCache) GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error) {
	return c.tokenHashes(ctx, `SELECT token_hash FROM personal_refresh_tokens WHERE family_id = ? AND expires_at > ?`, familyID)
}
func (c *personalRefreshTokenCache) tokenHashes(ctx context.Context, query string, argument any) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, query, argument, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, rows.Err()
}
func (c *personalRefreshTokenCache) IsTokenInFamily(ctx context.Context, familyID, tokenHash string) (bool, error) {
	var exists int
	err := c.db.QueryRowContext(ctx, `SELECT 1 FROM personal_refresh_tokens WHERE family_id = ? AND token_hash = ? AND expires_at > ?`, familyID, tokenHash, time.Now().UTC()).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
