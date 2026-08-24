package repository

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/providerproxy"
)

func TestResolveProviderUpstreamProxyURLUsesPersonalResolverWhenAccountProxyIsEmpty(t *testing.T) {
	resolver := func(*http.Request) (providerproxy.Decision, error) {
		return providerproxy.Decision{
			URL:    &url.URL{Scheme: "http", Host: "127.0.0.1:7897"},
			Source: "windows_system",
		}, nil
	}

	req, err := http.NewRequest(http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	got, err := resolveProviderUpstreamProxyURLWithResolver(req, "", resolver)
	if err != nil {
		t.Fatalf("resolve provider proxy: %v", err)
	}
	if got != "http://127.0.0.1:7897" {
		t.Fatalf("unexpected proxy URL %q", got)
	}
}

func TestResolveProviderUpstreamProxyURLPrefersExplicitAccountProxy(t *testing.T) {
	resolver := func(*http.Request) (providerproxy.Decision, error) {
		t.Fatal("Personal resolver must not run for an explicit account proxy")
		return providerproxy.Decision{}, nil
	}

	got, err := resolveProviderUpstreamProxyURLWithResolver(nil, "  socks5://127.0.0.1:1080  ", resolver)
	if err != nil {
		t.Fatalf("resolve explicit proxy: %v", err)
	}
	if got != "socks5://127.0.0.1:1080" {
		t.Fatalf("unexpected explicit proxy URL %q", got)
	}
}
