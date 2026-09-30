package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type testDispatchGuard struct{ attempts atomic.Int32 }

func (g *testDispatchGuard) BeforeRoundTrip(string) error {
	if g.attempts.Add(1) > 1 {
		return errors.New("budget exceeded")
	}
	return nil
}

func TestHTTPClientDispatchGuardDefaultOff(t *testing.T) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", "http://localhost/", nil)
	if httpClientWithDispatchGuard(client, req) != client {
		t.Fatal("unguarded client changed")
	}
}

func TestHTTPClientDispatchGuardBlocksRedirectBeforeDestination(t *testing.T) {
	var sourceHits, destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { destinationHits.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceHits.Add(1)
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()
	guard := &testDispatchGuard{}
	ctx := service.WithHTTPUpstreamDispatchRole(service.WithHTTPUpstreamDispatchGuard(context.Background(), guard), "APP")
	req, _ := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
	_, err := httpClientWithDispatchGuard(&http.Client{}, req).Do(req)
	if err == nil || guard.attempts.Load() != 2 || sourceHits.Load() != 1 || destinationHits.Load() != 0 {
		t.Fatal("second round trip was not blocked before destination")
	}
}
