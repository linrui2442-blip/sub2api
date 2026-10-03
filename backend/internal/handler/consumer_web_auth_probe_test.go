package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type authProbeResolverFixture struct{ called bool }

func (f *authProbeResolverFixture) ResolvePersonalProvider(_ context.Context, model, endpoint string) (string, string, error) {
	f.called = true
	if model != "gemini-3.1-pro-high" || endpoint != "" {
		return "", "", service.ErrPersonalRouteUnsupported
	}
	return service.PlatformAntigravity, model, nil
}

func TestConsumerWebAuthProbeMatchesVideoUnifiedProviderContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/_acceptance/consumer-web/auth-probe", nil)
	resolver := &authProbeResolverFixture{}
	if err := prepareConsumerWebProbePlatform(c, &service.APIKey{}, resolver); err != nil {
		t.Fatal(err)
	}
	platform, ok := middleware.GetForcePlatformFromContext(c)
	if !resolver.called || !ok || platform != service.PlatformAntigravity {
		t.Fatal("probe did not apply production unified-video provider context")
	}
	groupID := int64(7)
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodPost, "/_acceptance/consumer-web/auth-probe", nil)
	resolver = &authProbeResolverFixture{}
	if err := prepareConsumerWebProbePlatform(c2, &service.APIKey{GroupID: &groupID}, resolver); err != nil || resolver.called {
		t.Fatal("pinned key must retain group-scoped video selection")
	}
}

type authProbeSchedulerFixture struct {
	called, released bool
	account          *service.Account
}

func (f *authProbeSchedulerFixture) SelectAccountWithLoadAwareness(_ context.Context, _ *int64, _ string, model string, excluded map[int64]struct{}, _ string, _ int64) (*service.AccountSelectionResult, error) {
	f.called = true
	if model != "gemini-3.1-pro-high" || excluded != nil {
		return nil, service.ErrNoAvailableAccounts
	}
	return &service.AccountSelectionResult{Account: f.account, ReleaseFunc: func() { f.released = true }}, nil
}

type authProbeManagerFixture struct {
	called  bool
	account *service.Account
}

func (f *authProbeManagerFixture) AcquireSession(ctx context.Context, account *service.Account) (service.ConsumerWebSessionHandle, error) {
	f.called = true
	f.account = account
	if ctx == nil {
		return nil, service.ErrConsumerWebAuthUnavailable
	}
	return nil, nil
}

func TestConsumerWebAuthProbeUsesSchedulerAndAcquireOnly(t *testing.T) {
	account := &service.Account{ID: 73}
	scheduler := &authProbeSchedulerFixture{account: account}
	manager := &authProbeManagerFixture{}
	selected, trace, err := acquireConsumerWebForProbe(context.Background(), scheduler, manager, nil, "synthetic")
	if err != nil || selected != account || trace == nil || !scheduler.called || !scheduler.released || !manager.called || manager.account != account {
		t.Fatal("probe did not use scheduler and session manager with selected account")
	}
	// No video driver is reachable from this auth-only seam.
}

func TestConsumerWebAuthProbeRequiresExistingAPIKeyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/_acceptance/consumer-web/auth-probe", (&GatewayHandler{}).ConsumerWebAuthProbe)
	req := httptest.NewRequest(http.MethodPost, "/_acceptance/consumer-web/auth-probe", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing API key context returned %d", w.Code)
	}
}
