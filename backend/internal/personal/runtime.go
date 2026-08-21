package personal

import (
	"os"
	"strings"
)

const EnvEnabled = "SUB2_PERSONAL_MODE"

var personalDisabledBackgroundFeatureEnv = []string{
	"OPS_ENABLED",
	"OPS_CLEANUP_ENABLED",
	"DASHBOARD_AGGREGATION_ENABLED",
	"USAGE_CLEANUP_ENABLED",
	"BATCH_IMAGE_ENABLED",
	"GATEWAY_CN_PROVIDERS_BALANCE_CHECK_ENABLED",
}

// Enabled reports whether the current process is running with Personal Edition
// routing/policy enabled.
func Enabled() bool {
	return truthy(os.Getenv(EnvEnabled))
}

// PrepareEnvironment activates the Personal Edition runtime for dedicated
// personal builds or when SUB2_PERSONAL_MODE is explicitly enabled.
//
// Until the upstream config package gets a first-class personal run mode, we
// intentionally force its recognized SIMPLE mode underneath Personal Edition.
// This preserves upstream account/gateway behavior while disabling billing and
// quota charging semantics. Personal-specific route restrictions are enforced
// separately by the server router.
func PrepareEnvironment(buildType string) bool {
	if strings.EqualFold(strings.TrimSpace(buildType), ModeName) {
		_ = os.Setenv(EnvEnabled, "1")
	}
	if !Enabled() {
		return false
	}

	// Fail safe: Personal Edition always inherits upstream SIMPLE semantics.
	_ = os.Setenv("RUN_MODE", "simple")

	// These upstream workers belong to SaaS operations/commercial or unrelated
	// batch-provider surfaces. Personal Edition does not expose them, so keep the
	// corresponding config hard-disabled while their shared Wire dependencies are
	// being physically removed. OAuth token refresh, scheduler, account health
	// and gateway concurrency are deliberately NOT disabled here.
	for _, key := range personalDisabledBackgroundFeatureEnv {
		_ = os.Setenv(key, "false")
	}

	// Route config, install lock, logs and other upstream DATA_DIR consumers to
	// the same private user-scoped directory as the SQLite database. This makes
	// the Windows build portable from the user's point of view and avoids files
	// leaking into whichever working directory launched the EXE.
	if strings.TrimSpace(os.Getenv("DATA_DIR")) == "" {
		if dataDir, err := DataDir(); err == nil && strings.TrimSpace(dataDir) != "" {
			_ = os.Setenv("DATA_DIR", dataDir)
		}
	}

	// Windows/local-first default. Operators may explicitly bind another
	// address later (for example a trusted private LAN/VPN) by setting
	// SERVER_HOST themselves.
	if strings.TrimSpace(os.Getenv("SERVER_HOST")) == "" {
		_ = os.Setenv("SERVER_HOST", "127.0.0.1")
	}
	return true
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
