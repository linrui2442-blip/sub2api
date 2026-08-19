package personal

import (
	"os"
	"testing"
)

func TestPrepareEnvironmentForPersonalBuild(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	t.Setenv("RUN_MODE", "standard")
	t.Setenv("SERVER_HOST", "")
	for _, key := range personalDisabledBackgroundFeatureEnv {
		t.Setenv(key, "true")
	}

	if !PrepareEnvironment("personal") {
		t.Fatal("personal build must enable personal runtime")
	}
	if got := os.Getenv("RUN_MODE"); got != "simple" {
		t.Fatalf("personal runtime must force upstream simple mode, got %q", got)
	}
	if got := os.Getenv("SERVER_HOST"); got != "127.0.0.1" {
		t.Fatalf("personal runtime should default to loopback, got %q", got)
	}
	for _, key := range personalDisabledBackgroundFeatureEnv {
		if got := os.Getenv(key); got != "false" {
			t.Fatalf("personal runtime must hard-disable %s, got %q", key, got)
		}
	}
	if got := os.Getenv("TOKEN_REFRESH_ENABLED"); got != "" {
		t.Fatalf("personal runtime must not disable token refresh, got %q", got)
	}
}

func TestPrepareEnvironmentRespectsExplicitHost(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv("RUN_MODE", "standard")
	t.Setenv("SERVER_HOST", "10.0.0.5")

	if !PrepareEnvironment("source") {
		t.Fatal("explicit personal env must enable personal runtime")
	}
	if got := os.Getenv("SERVER_HOST"); got != "10.0.0.5" {
		t.Fatalf("explicit host must be preserved, got %q", got)
	}
	if got := os.Getenv("RUN_MODE"); got != "simple" {
		t.Fatalf("personal runtime must still force simple mode, got %q", got)
	}
}

func TestPrepareEnvironmentNoopWhenDisabled(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	t.Setenv("RUN_MODE", "standard")
	t.Setenv("SERVER_HOST", "0.0.0.0")
	t.Setenv("OPS_ENABLED", "true")

	if PrepareEnvironment("source") {
		t.Fatal("normal source build must not enable personal runtime")
	}
	if got := os.Getenv("RUN_MODE"); got != "standard" {
		t.Fatalf("disabled personal runtime must not rewrite run mode, got %q", got)
	}
	if got := os.Getenv("OPS_ENABLED"); got != "true" {
		t.Fatalf("disabled personal runtime must not rewrite ops switch, got %q", got)
	}
}
