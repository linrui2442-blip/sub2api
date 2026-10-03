package personal

import "testing"

func TestAcceptanceAuthProbeRequiresPersonalBuildAndOptIn(t *testing.T) {
	t.Setenv(EnvEnabled, "1")
	t.Setenv(EnvAcceptanceAuthProbe, "")
	ConfigureAcceptanceAuthProbe(ModeName)
	if AcceptanceAuthProbeEnabled() {
		t.Fatal("acceptance route enabled by default")
	}
	t.Setenv(EnvAcceptanceAuthProbe, "1")
	ConfigureAcceptanceAuthProbe("source")
	if AcceptanceAuthProbeEnabled() {
		t.Fatal("source build enabled acceptance route")
	}
	ConfigureAcceptanceAuthProbe(ModeName)
	if !AcceptanceAuthProbeEnabled() {
		t.Fatal("personal opt-in did not enable acceptance route")
	}
	t.Cleanup(func() { ConfigureAcceptanceAuthProbe("source") })
}
