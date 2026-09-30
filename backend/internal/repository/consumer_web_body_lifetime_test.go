package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestConsumerWebRealUpstreamDelayedBodyLifetime(t *testing.T) {
	appHits := 0
	otaqHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			appHits++
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
			_, _ = w.Write([]byte(`"SNlM0e":"synthetic-token"`))
		case "/otaq":
			otaqHits++
			_, _ = w.Write([]byte(")]}'\n[\"wrb.fr\",\"otAQ7b\",\"[null,null,null,null,null,null,null,null,null,null,null,null,null,null,1000,null]\"]\n"))
		}
	}))
	defer server.Close()
	tr := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{
		HTTPUpstream: NewHTTPUpstream(nil), Encryptor: consumerWebBodyTestEncryptor{},
		TestEndpoints: &service.ConsumerWebEndpointSet{App: server.URL + "/app", OTAQ: server.URL + "/otaq", Rotate: server.URL + "/rotate"},
	})
	acct := &service.Account{ID: 997, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Concurrency: 1}
	creds := map[string]any{service.ConsumerWebCredentialNamespace: map[string]any{
		service.ConsumerWebPSIDCiphertextKey: "enc:synthetic-psid", service.ConsumerWebPSIDTSCiphertextKey: "enc:synthetic-psidts",
	}}
	_, err := tr.Bootstrap(context.Background(), acct, creds, nil)
	if err != nil {
		if errors.Is(err, service.ErrConsumerWebTransportTransient) && appHits == 1 && otaqHits == 0 {
			t.Fatalf("HTTP 200 headers followed by canceled body read (pre-fix reproduction)")
		}
		t.Fatalf("bootstrap error class: transient=%t auth=%t protocol=%t", errors.Is(err, service.ErrConsumerWebTransportTransient), errors.Is(err, service.ErrConsumerWebAuthLost), errors.Is(err, service.ErrConsumerWebProtocolDrift))
	}
	if appHits != 1 || otaqHits != 1 {
		t.Fatalf("dispatch counts app=%d otaq=%d", appHits, otaqHits)
	}
}

type consumerWebBodyTestEncryptor struct{}

func (consumerWebBodyTestEncryptor) Encrypt(v string) (string, error) { return "enc:" + v, nil }
func (consumerWebBodyTestEncryptor) Decrypt(v string) (string, error) {
	return strings.TrimPrefix(v, "enc:"), nil
}
