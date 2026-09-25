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
	if ClassifyHTTPTransportError(inner, d) != DiagnosticFailureTCPConnect {
		t.Fatalf("unexpected class: %s", ClassifyHTTPTransportError(inner, d))
	}
	_ = x509.UnknownAuthorityError{}
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
