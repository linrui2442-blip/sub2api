package main

import (
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

func TestPersonalApplicationBootSmoke(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", filepath.Join(t.TempDir(), "personal-app.db"))
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "18080")
	t.Setenv("SKIP_SETUP", "1")

	if !personal.PrepareEnvironment("personal") {
		t.Fatal("Personal runtime must be enabled")
	}
	repository.ClosePersonalEmbeddedRedis()
	defer repository.ClosePersonalEmbeddedRedis()

	app, err := initializePersonalApplication(handler.BuildInfo{
		Version:   "personal-smoke",
		BuildType: "personal",
	})
	if err != nil {
		t.Fatalf("initialize dedicated Personal Edition without external PostgreSQL/Redis: %v", err)
	}
	if app == nil || app.Server == nil {
		t.Fatal("Personal Edition application/server must be initialized")
	}
	app.Cleanup()
}
