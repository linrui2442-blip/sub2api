package repository

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type guardedRoundTripper struct {
	base  http.RoundTripper
	guard service.HTTPUpstreamDispatchGuard
	role  string
}

func (g *guardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := g.guard.BeforeRoundTrip(g.role); err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := g.base.RoundTrip(req)
	if observer, ok := g.guard.(interface {
		AfterRoundTrip(string, int, time.Duration)
	}); ok {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		observer.AfterRoundTrip(g.role, status, time.Since(started))
	}
	return resp, err
}

func httpClientWithDispatchGuard(client *http.Client, req *http.Request) *http.Client {
	if client == nil || req == nil {
		return client
	}
	guard, role := service.HTTPUpstreamDispatchGuardFromContext(req.Context())
	if guard == nil || role == "" {
		return client
	}
	clone := *client
	base := clone.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	clone.Transport = &guardedRoundTripper{base: base, guard: guard, role: role}
	return &clone
}
