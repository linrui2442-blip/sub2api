package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func diagnosticRequest(t *testing.T, ctx context.Context, method, target string, body io.Reader) (*http.Request, *service.HTTPTransportDiagnostic) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	d := &service.HTTPTransportDiagnostic{}
	return service.WithHTTPTransportDiagnostic(req, d), d
}

func diagnosticError(t *testing.T, err error) service.HTTPTransportDiagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	var de *service.HTTPTransportDiagnosticError
	if !errors.As(err, &de) {
		t.Fatalf("missing diagnostic error: %T", err)
	}
	return de.Diagnostic()
}

func TestLocalProxyFailureDiagnostic(t *testing.T) {
	var originHits atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { originHits.Add(1); w.WriteHeader(200) }))
	defer origin.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	req, _ := diagnosticRequest(t, context.Background(), http.MethodGet, origin.URL, nil)
	_, err = NewHTTPUpstream(nil).Do(req, "http://"+addr, 9001, 1)
	d := diagnosticError(t, err)
	if !d.ProxyConfigured || d.ConnectionRole != "PROXY" || !d.ConnectStarted || d.HTTPResponseReceived || d.Stage != service.DiagnosticStageConnect || (d.FailureClass != service.DiagnosticFailureProxyConnect && d.FailureClass != service.DiagnosticFailureTCPConnect) || originHits.Load() != 0 {
		t.Fatalf("unexpected proxy diagnostic: %+v; hits=%d", d, originHits.Load())
	}
}

func TestLocalProxyAuthDiagnostic(t *testing.T) {
	var originHits atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { originHits.Add(1) }))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("method=%s", r.Method)
		}
		w.Header().Set("Proxy-Authenticate", "Basic realm=local")
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	proxyURL.User = url.UserPassword("proxy-user-secret", "proxy-password-secret")
	req, _ := diagnosticRequest(t, context.Background(), http.MethodGet, origin.URL, nil)
	_, err := NewHTTPUpstream(nil).Do(req, proxyURL.String(), 9002, 1)
	d := diagnosticError(t, err)
	if d.Stage != service.DiagnosticStageProxyNegotiation || d.ConnectionRole != "PROXY" || !d.ProxyAuthConfigured || originHits.Load() != 0 {
		t.Fatalf("unexpected 407 diagnostic: %+v; hits=%d", d, originHits.Load())
	}
	var proxyCause *service.TLSCauseDiagnosticError
	if errors.As(err, &proxyCause) {
		t.Fatal("CONNECT failure was incorrectly classified as TLS failure")
	}
	for _, value := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprint(service.SafeTransportDiagnostic(d)), fmt.Sprint(errors.Unwrap(err))} {
		if strings.Contains(value, "proxy-user-secret") || strings.Contains(value, "proxy-password-secret") || strings.Contains(value, "Proxy-Authorization") {
			t.Fatal("proxy credential leak")
		}
	}
}

func TestLocalTLSTrustDiagnostic(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer target.Close()
	req, _ := diagnosticRequest(t, context.Background(), http.MethodGet, target.URL, nil)
	_, err := NewHTTPUpstream(nil).Do(req, "", 9003, 1)
	d := diagnosticError(t, err)
	if !d.TLSStarted || d.TLSCompleted || d.HTTPResponseReceived || d.Stage != service.DiagnosticStageTLSHandshake || d.FailureClass != service.DiagnosticFailureTLSTrust {
		t.Fatalf("unexpected TLS diagnostic: %+v", d)
	}
	var cause *service.TLSCauseDiagnosticError
	if !errors.As(err, &cause) || cause.Code != service.TLSCauseX509UnknownAuthority {
		t.Fatal("typed TLS cause missing after real local RoundTrip")
	}
	var rawCert x509.UnknownAuthorityError
	if errors.As(err, &rawCert) {
		t.Fatal("raw certificate cause escaped diagnostic boundary")
	}
	var certErr x509.UnknownAuthorityError
	_ = certErr
	_ = tls.VersionTLS13
}

func TestLocalContextCancelDiagnostic(t *testing.T) {
	started := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer target.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := diagnosticRequest(t, ctx, http.MethodGet, target.URL, nil)
	done := make(chan error, 1)
	go func() { _, err := NewHTTPUpstream(nil).Do(req, "", 9004, 1); done <- err }()
	<-started
	cancel()
	err := <-done
	d := diagnosticError(t, err)
	if d.FailureClass != service.DiagnosticFailureContextCanceled || !d.ContextCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel diagnostic: %+v", d)
	}
}

func TestLocalTimeoutDiagnostic(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req, _ := diagnosticRequest(t, ctx, http.MethodGet, target.URL, nil)
	_, err := NewHTTPUpstream(nil).Do(req, "", 9005, 1)
	d := diagnosticError(t, err)
	if d.FailureClass != service.DiagnosticFailureTimeout || !d.Timeout || d.ContextCanceled || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout diagnostic: %+v", d)
	}
}

func TestLocalRedirectDiagnostic(t *testing.T) {
	var untrustedHits atomic.Int32
	untrusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { untrustedHits.Add(1) }))
	defer untrusted.Close()
	trusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, untrusted.URL+"/?token=redirect-secret", http.StatusFound)
	}))
	defer trusted.Close()
	req, _ := diagnosticRequest(t, context.Background(), http.MethodGet, trusted.URL+"/?at=query-secret", nil)
	req.Header.Set("Cookie", "session=cookie-secret")
	_, err := NewHTTPUpstream(nil).(service.HTTPUpstreamRedirector).DoWithRedirectPolicy(req, "", 9006, 1, func(_ *http.Request, _ []*http.Request) error { return errors.New("rejected") })
	d := diagnosticError(t, err)
	if !d.RedirectRejected || d.Stage != service.DiagnosticStageRedirectPolicy || d.FailureClass != service.DiagnosticFailureRedirectRejected || untrustedHits.Load() != 0 {
		t.Fatalf("redirect diagnostic: %+v; hits=%d", d, untrustedHits.Load())
	}
	for _, value := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprint(service.SafeTransportDiagnostic(d)), fmt.Sprint(errors.Unwrap(err))} {
		for _, marker := range []string{"query-secret", "redirect-secret", "cookie-secret"} {
			if strings.Contains(value, marker) {
				t.Fatal("request secret leak")
			}
		}
	}
}

func TestLocalHTTPResponseDiagnostic(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/fixture" || r.Header.Get("Cookie") != "session=marker" {
					t.Error("request semantics changed")
				}
				w.WriteHeader(status)
			}))
			defer target.Close()
			req, d := diagnosticRequest(t, context.Background(), http.MethodPost, target.URL+"/fixture", strings.NewReader("body"))
			req.Header.Set("Cookie", "session=marker")
			resp, err := NewHTTPUpstream(nil).Do(req, "", int64(9100+status), 1)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if !d.FirstResponseByte || !d.HTTPResponseReceived || d.Stage != service.DiagnosticStageResponseReceived || d.FailureClass != "" {
				t.Fatalf("response diagnostic: %+v", d)
			}
		})
	}
}

func TestRequestURLSecretRedactionDiagnostic(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	req, _ := diagnosticRequest(t, context.Background(), http.MethodGet, "http://"+addr+"/fixture?at=at-secret&token=query-secret", nil)
	_, err = NewHTTPUpstream(nil).Do(req, "", 9200, 1)
	d := diagnosticError(t, err)
	b, _ := json.Marshal(service.SafeTransportDiagnostic(d))
	for _, value := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprint(errors.Unwrap(err)), string(b)} {
		if strings.Contains(value, "at-secret") || strings.Contains(value, "query-secret") || strings.Contains(value, "?at=") {
			t.Fatal("URL secret leak")
		}
	}
}
