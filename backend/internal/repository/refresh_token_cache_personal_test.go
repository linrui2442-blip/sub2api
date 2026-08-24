package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "modernc.org/sqlite"
)

func newPersonalRefreshTokenCacheForTest(t *testing.T) service.RefreshTokenCache {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE personal_refresh_tokens (
		token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, family_id TEXT NOT NULL,
		payload TEXT NOT NULL, expires_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	return newPersonalRefreshTokenCache(db)
}

func TestPersonalRefreshTokenCacheSurvivesStoreAndRevokesFamily(t *testing.T) {
	cache := newPersonalRefreshTokenCacheForTest(t)
	ctx := context.Background()
	data := &service.RefreshTokenData{UserID: 9, TokenVersion: 2, FamilyID: "family", BindingHash: "binding", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
	if err := cache.StoreRefreshToken(ctx, "first", data, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.AddToUserTokenSet(ctx, data.UserID, "first", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.AddToFamilyTokenSet(ctx, data.FamilyID, "first", time.Hour); err != nil {
		t.Fatal(err)
	}
	got, err := cache.GetRefreshToken(ctx, "first")
	if err != nil || got.UserID != data.UserID || got.FamilyID != data.FamilyID || got.TokenVersion != data.TokenVersion {
		t.Fatalf("GetRefreshToken = (%+v, %v)", got, err)
	}
	inFamily, err := cache.IsTokenInFamily(ctx, data.FamilyID, "first")
	if err != nil || !inFamily {
		t.Fatalf("IsTokenInFamily = (%v, %v)", inFamily, err)
	}
	if err := cache.DeleteTokenFamily(ctx, data.FamilyID); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetRefreshToken(ctx, "first"); !errors.Is(err, service.ErrRefreshTokenNotFound) {
		t.Fatalf("revoked token error = %v", err)
	}
}
