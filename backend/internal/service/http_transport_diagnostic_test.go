package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"syscall"
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

func TestTLSCauseCodeTypedFixtures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want TLSCauseCode
	}{
		{"context_deadline", context.DeadlineExceeded, TLSCauseContextDeadline},
		{"context_canceled", context.Canceled, TLSCauseContextCanceled},
		{"net_timeout", &net.DNSError{Err: "synthetic", IsTimeout: true}, TLSCauseTimeout},
		{"eof", io.EOF, TLSCauseEOF},
		{"unexpected_eof", io.ErrUnexpectedEOF, TLSCauseUnexpectedEOF},
		{"x509_unknown_authority", x509.UnknownAuthorityError{}, TLSCauseX509UnknownAuthority},
		{"x509_hostname", x509.HostnameError{}, TLSCauseX509Hostname},
		{"x509_cert_invalid", x509.CertificateInvalidError{}, TLSCauseX509CertInvalid},
		{"protocol", &tls.RecordHeaderError{}, TLSCauseProtocol},
		{"handshake_alert", tls.AlertError(40), TLSCauseHandshakeAlert},
		{"unknown", errors.New("opaque synthetic error"), TLSCauseUnknown},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			struct {
				name string
				err  error
				want TLSCauseCode
			}{"connection_reset", &net.OpError{Op: "read", Err: syscall.Errno(10054)}, TLSCauseConnectionReset},
			struct {
				name string
				err  error
				want TLSCauseCode
			}{"connection_aborted", &net.OpError{Op: "read", Err: syscall.Errno(10053)}, TLSCauseConnectionAborted},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyTLSCause(fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestTLSCauseCodeSafeCarrierAndContextCompatibility(t *testing.T) {
	marker := "synthetic-proxy-secret-marker"
	root := fmt.Errorf("%s: %w", marker, syscall.Errno(10054))
	d := HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: ClassifyTLSCause(root)}
	err := fmt.Errorf("consumer web transport transient failure: %w", NewHTTPTransportDiagnosticError(d, root))
	var carrier *TLSCauseDiagnosticError
	want := TLSCauseUnknown
	if runtime.GOOS == "windows" {
		want = TLSCauseConnectionReset
	}
	if !errors.As(err, &carrier) || carrier.Code != want {
		t.Fatal("bounded cause did not survive wrapping")
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		t.Fatal("raw syscall cause escaped diagnostic boundary")
	}
	for _, safe := range []string{err.Error(), fmt.Sprint(errors.Unwrap(err)), carrier.Error(), fmt.Sprint(SafeTransportDiagnostic(d))} {
		if strings.Contains(safe, marker) {
			t.Fatal("raw error leaked")
		}
	}
	deadline := NewHTTPTransportDiagnosticError(HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseContextDeadline}, fmt.Errorf("%s: %w", marker, context.DeadlineExceeded))
	if !errors.Is(deadline, context.DeadlineExceeded) || !errors.As(deadline, &carrier) || carrier.Code != TLSCauseContextDeadline {
		t.Fatal("context compatibility or carrier lost")
	}
	if strings.Contains(deadline.Error(), marker) {
		t.Fatal("raw deadline error leaked")
	}
	malicious := NewHTTPTransportDiagnosticError(HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseCode(marker)}, root)
	if !errors.As(malicious, &carrier) || carrier.Code != TLSCauseUnknown {
		t.Fatal("unbounded code was not normalized")
	}
}

func TestRecordTLSHandshakeDiagnosticKeepsOnlyBoundedCode(t *testing.T) {
	marker := "synthetic-private-endpoint-and-cookie"
	d := &HTTPTransportDiagnostic{TLSStarted: true}
	RecordTLSHandshakeDiagnostic(d, fmt.Errorf("%s: %w", marker, io.ErrUnexpectedEOF))
	if d.TLSCompleted || d.TLSCauseCode != TLSCauseUnexpectedEOF {
		t.Fatal("callback cause not captured")
	}
	if strings.Contains(fmt.Sprint(SafeTransportDiagnostic(*d)), marker) {
		t.Fatal("callback leaked raw error")
	}
	RecordTLSHandshakeDiagnostic(d, nil)
	if !d.TLSCompleted {
		t.Fatal("successful callback not preserved")
	}
}
