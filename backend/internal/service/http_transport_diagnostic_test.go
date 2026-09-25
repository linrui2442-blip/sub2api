package service

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
)

func TestHTTPTransportDiagnosticClassificationIsTypedAndSafe(t *testing.T) {
	d := HTTPTransportDiagnostic{ConnectStarted: true, ConnectCompleted: false, ProxyConfigured: true, ProxyScheme: "http", ProxyAuthConfigured: true}
	inner := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("synthetic proxy secret user:pass")}
	err := NewHTTPTransportDiagnosticError(d, inner)
	var typed *HTTPTransportDiagnosticError
	if !errors.As(err, &typed) || typed.Diagnostic().FailureClass != "" {
		// classification is assigned by the upstream boundary; this test verifies
		// the typed wrapper and safe public Error string independently.
		t.Fatalf("diagnostic wrapper missing")
	}
	if err.Error() != "http transport diagnostic failure" {
		t.Fatalf("unsafe diagnostic error string: %q", err.Error())
	}
	if got := ClassifyHTTPTransportError(inner, d); got != DiagnosticFailureTCPConnect && got != DiagnosticFailureProxyConnect {
		t.Fatalf("unexpected class: %s", ClassifyHTTPTransportError(inner, d))
	}
	_ = x509.UnknownAuthorityError{}
}

func TestHTTPTransportDiagnosticFailureMatrix(t *testing.T) {
	tests := []struct {
		name string
		err  error
		d    HTTPTransportDiagnostic
		want TransportDiagnosticFailureClass
	}{
		{"dns", &net.DNSError{Err: "synthetic", IsTimeout: false}, HTTPTransportDiagnostic{}, DiagnosticFailureDNS},
		{"cancel", context.Canceled, HTTPTransportDiagnostic{}, DiagnosticFailureContextCanceled},
		{"timeout", context.DeadlineExceeded, HTTPTransportDiagnostic{}, DiagnosticFailureTimeout},
		{"connect", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("closed")}, HTTPTransportDiagnostic{ConnectStarted: true}, DiagnosticFailureTCPConnect},
		{"tls", errors.New("tls"), HTTPTransportDiagnostic{TLSStarted: true}, DiagnosticFailureTLSHandshake},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyHTTPTransportError(tt.err, tt.d); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestSafeTransportDiagnosticFormatterRedactsSecrets(t *testing.T) {
	d := HTTPTransportDiagnostic{Stage: DiagnosticStageConnect, FailureClass: DiagnosticFailureProxyConnect, ProxyConfigured: true, ProxyScheme: "http", ProxyAuthConfigured: true, ConnectionRole: "PROXY"}
	b := SafeTransportDiagnostic(d)
	if b["connection_role"] != "PROXY" || b["proxy_scheme"] != "http" {
		t.Fatal("missing safe metadata")
	}
	for _, k := range []string{"url", "raw_url", "cookie", "token", "password", "username"} {
		if _, ok := b[k]; ok {
			t.Fatalf("unsafe key %s", k)
		}
	}
}

func TestDiagnosticStagePrecedence(t *testing.T) {
	if got := ClassifyHTTPTransportError(errors.New("generic"), HTTPTransportDiagnostic{TLSStarted: true, TLSCompleted: false, ConnectStarted: true}); got != DiagnosticFailureTLSHandshake {
		t.Fatalf("TLS precedence: %s", got)
	}
	if got := ClassifyHTTPTransportError(errors.New("generic"), HTTPTransportDiagnostic{RequestWritten: true}); got != DiagnosticFailureRoundTrip {
		t.Fatalf("write fallback: %s", got)
	}
}

func TestHTTPTransportDiagnosticContextIsRequestScoped(t *testing.T) {
	a := &HTTPTransportDiagnostic{}
	b := &HTTPTransportDiagnostic{}
	r1 := WithHTTPTransportDiagnostic(&http.Request{}, a)
	r2 := WithHTTPTransportDiagnostic(&http.Request{}, b)
	if HTTPTransportDiagnosticFromContext(r1.Context()) != a || HTTPTransportDiagnosticFromContext(r2.Context()) != b {
		t.Fatal("diagnostics leaked across request contexts")
	}
	var wg sync.WaitGroup
	for _, d := range []*HTTPTransportDiagnostic{a, b} {
		wg.Add(1)
		go func(d *HTTPTransportDiagnostic) { defer wg.Done(); d.DNSStarted = true }(d)
	}
	wg.Wait()
	if !a.DNSStarted || !b.DNSStarted {
		t.Fatal("request diagnostics not independently writable")
	}
	_ = context.Background()
}
