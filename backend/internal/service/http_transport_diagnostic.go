package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"syscall"
)

type TransportDiagnosticStage string

const (
	DiagnosticStagePrepare          TransportDiagnosticStage = "PREPARE"
	DiagnosticStageDNS              TransportDiagnosticStage = "DNS"
	DiagnosticStageConnect          TransportDiagnosticStage = "CONNECT"
	DiagnosticStageProxyNegotiation TransportDiagnosticStage = "PROXY_NEGOTIATION"
	DiagnosticStageTLSHandshake     TransportDiagnosticStage = "TLS_HANDSHAKE"
	DiagnosticStageRequestWrite     TransportDiagnosticStage = "REQUEST_WRITE"
	DiagnosticStageWaitResponse     TransportDiagnosticStage = "WAIT_RESPONSE"
	DiagnosticStageResponseReceived TransportDiagnosticStage = "RESPONSE_RECEIVED"
	DiagnosticStageRedirectPolicy   TransportDiagnosticStage = "REDIRECT_POLICY"
	DiagnosticStageClosed           TransportDiagnosticStage = "CLOSED"
	DiagnosticStageUnknown          TransportDiagnosticStage = "UNKNOWN"
)

type TransportDiagnosticFailureClass string

// TLSCauseCode is a closed, secret-free classification of a TLS failure.
// It never contains an error message, endpoint, proxy, or certificate detail.
type TLSCauseCode string

const (
	TLSCauseContextDeadline      TLSCauseCode = "CONTEXT_DEADLINE"
	TLSCauseContextCanceled      TLSCauseCode = "CONTEXT_CANCELED"
	TLSCauseTimeout              TLSCauseCode = "TIMEOUT"
	TLSCauseX509UnknownAuthority TLSCauseCode = "X509_UNKNOWN_AUTHORITY"
	TLSCauseX509Hostname         TLSCauseCode = "X509_HOSTNAME"
	TLSCauseX509CertInvalid      TLSCauseCode = "X509_CERT_INVALID"
	TLSCauseEOF                  TLSCauseCode = "EOF"
	TLSCauseUnexpectedEOF        TLSCauseCode = "UNEXPECTED_EOF"
	TLSCauseConnectionReset      TLSCauseCode = "CONNECTION_RESET"
	TLSCauseConnectionAborted    TLSCauseCode = "CONNECTION_ABORTED"
	TLSCauseProtocol             TLSCauseCode = "PROTOCOL"
	TLSCauseHandshakeAlert       TLSCauseCode = "HANDSHAKE_ALERT"
	TLSCauseOtherTyped           TLSCauseCode = "OTHER_TYPED"
	TLSCauseUnknown              TLSCauseCode = "UNKNOWN"
)

func normalizeTLSCauseCode(code TLSCauseCode) TLSCauseCode {
	switch code {
	case TLSCauseContextDeadline, TLSCauseContextCanceled, TLSCauseTimeout,
		TLSCauseX509UnknownAuthority, TLSCauseX509Hostname, TLSCauseX509CertInvalid,
		TLSCauseEOF, TLSCauseUnexpectedEOF, TLSCauseConnectionReset,
		TLSCauseConnectionAborted, TLSCauseProtocol, TLSCauseHandshakeAlert,
		TLSCauseOtherTyped, TLSCauseUnknown:
		return code
	default:
		return TLSCauseUnknown
	}
}

// ClassifyTLSCause uses typed identities only; unrecognized errors stay UNKNOWN.
func ClassifyTLSCause(err error) TLSCauseCode {
	if err == nil {
		return TLSCauseUnknown
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return TLSCauseContextDeadline
	}
	if errors.Is(err, context.Canceled) {
		return TLSCauseContextCanceled
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return TLSCauseTimeout
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return TLSCauseUnexpectedEOF
	}
	if errors.Is(err, io.EOF) {
		return TLSCauseEOF
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return TLSCauseX509UnknownAuthority
	}
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return TLSCauseX509Hostname
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return TLSCauseX509CertInvalid
	}
	var errno syscall.Errno
	if runtime.GOOS == "windows" && errors.As(err, &errno) {
		switch errno {
		case syscall.Errno(10054):
			return TLSCauseConnectionReset
		case syscall.Errno(10053):
			return TLSCauseConnectionAborted
		}
	}
	var alert tls.AlertError
	if errors.As(err, &alert) {
		return TLSCauseHandshakeAlert
	}
	var record *tls.RecordHeaderError
	if errors.As(err, &record) {
		return TLSCauseProtocol
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return TLSCauseOtherTyped
	}
	return TLSCauseUnknown
}

// RecordTLSHandshakeDiagnostic retains only a bounded cause code from the
// handshake callback. The callback error itself is never retained here.
func RecordTLSHandshakeDiagnostic(d *HTTPTransportDiagnostic, err error) {
	if d == nil {
		return
	}
	d.TLSCompleted = err == nil
	if err != nil {
		d.TLSCauseCode = ClassifyTLSCause(err)
	} else {
		d.TLSCauseCode = ""
	}
}

const (
	DiagnosticFailureProxyConfig      TransportDiagnosticFailureClass = "PROXY_CONFIG"
	DiagnosticFailureDNS              TransportDiagnosticFailureClass = "DNS"
	DiagnosticFailureTCPConnect       TransportDiagnosticFailureClass = "TCP_CONNECT"
	DiagnosticFailureProxyConnect     TransportDiagnosticFailureClass = "PROXY_CONNECT"
	DiagnosticFailureTLSHandshake     TransportDiagnosticFailureClass = "TLS_HANDSHAKE"
	DiagnosticFailureTLSTrust         TransportDiagnosticFailureClass = "TLS_TRUST"
	DiagnosticFailureTimeout          TransportDiagnosticFailureClass = "TIMEOUT"
	DiagnosticFailureContextCanceled  TransportDiagnosticFailureClass = "CONTEXT_CANCELED"
	DiagnosticFailureRedirectRejected TransportDiagnosticFailureClass = "REDIRECT_REJECTED"
	DiagnosticFailureRoundTrip        TransportDiagnosticFailureClass = "ROUNDTRIP_PRE_RESPONSE"
	DiagnosticFailureUnknown          TransportDiagnosticFailureClass = "UNKNOWN"
)

type HTTPTransportDiagnostic struct {
	Stage                                                   TransportDiagnosticStage
	FailureClass                                            TransportDiagnosticFailureClass
	ProxyConfigured                                         bool
	ProxyScheme                                             string
	ProxyAuthConfigured                                     bool
	ConnectionRole                                          string
	DNSStarted, DNSCompleted                                bool
	ConnectStarted, ConnectCompleted                        bool
	TLSStarted, TLSCompleted                                bool
	RequestWritten, FirstResponseByte, HTTPResponseReceived bool
	Timeout, ContextCanceled, RedirectRejected              bool
	TLSCauseCode                                            TLSCauseCode
}

// SafeTransportDiagnostic exposes only non-sensitive lifecycle metadata.
func SafeTransportDiagnostic(d HTTPTransportDiagnostic) map[string]any {
	m := map[string]any{
		"failure_stage": d.Stage, "failure_class": d.FailureClass,
		"proxy_configured": d.ProxyConfigured, "proxy_scheme": d.ProxyScheme,
		"proxy_auth_configured": d.ProxyAuthConfigured, "connection_role": d.ConnectionRole,
		"dns_started": d.DNSStarted, "dns_completed": d.DNSCompleted,
		"connect_started": d.ConnectStarted, "connect_completed": d.ConnectCompleted,
		"tls_started": d.TLSStarted, "tls_completed": d.TLSCompleted,
		"request_written": d.RequestWritten, "first_response_byte": d.FirstResponseByte,
		"http_response_received": d.HTTPResponseReceived, "timeout": d.Timeout,
		"context_canceled": d.ContextCanceled, "redirect_rejected": d.RedirectRejected,
	}
	if d.TLSCauseCode != "" {
		m["tls_cause_code"] = normalizeTLSCauseCode(d.TLSCauseCode)
	}
	return m
}

type transportDiagnosticContextKey struct{}

func WithHTTPTransportDiagnostic(req *http.Request, d *HTTPTransportDiagnostic) *http.Request {
	if req == nil || d == nil {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), transportDiagnosticContextKey{}, d))
}
func HTTPTransportDiagnosticFromContext(ctx context.Context) *HTTPTransportDiagnostic {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(transportDiagnosticContextKey{}).(*HTTPTransportDiagnostic)
	return d
}

type HTTPTransportDiagnosticError struct {
	diag  HTTPTransportDiagnostic
	cause error
}

func NewHTTPTransportDiagnosticError(diag HTTPTransportDiagnostic, cause error) error {
	if diag.TLSStarted && !diag.TLSCompleted {
		diag.TLSCauseCode = normalizeTLSCauseCode(diag.TLSCauseCode)
	} else {
		diag.TLSCauseCode = ""
	}
	return &HTTPTransportDiagnosticError{diag: diag, cause: cause}
}
func (e *HTTPTransportDiagnosticError) Error() string { return "http transport diagnostic failure" }

// TLSCauseDiagnosticError is a safe errors.As carrier; it never wraps the raw cause.
type TLSCauseDiagnosticError struct{ Code TLSCauseCode }

func (e *TLSCauseDiagnosticError) Error() string { return "tls cause diagnostic" }
func (e *HTTPTransportDiagnosticError) Unwrap() error {
	// Network errors may embed proxy URLs or credentials. Preserve only context
	// sentinels and the bounded TLS code. Never expose the raw cause.
	var safe []error
	if e.diag.TLSCauseCode != "" {
		safe = append(safe, &TLSCauseDiagnosticError{Code: normalizeTLSCauseCode(e.diag.TLSCauseCode)})
	}
	if errors.Is(e.cause, context.Canceled) {
		safe = append(safe, context.Canceled)
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		safe = append(safe, context.DeadlineExceeded)
	}
	return errors.Join(safe...)
}
func (e *HTTPTransportDiagnosticError) Diagnostic() HTTPTransportDiagnostic { return e.diag }

func ClassifyHTTPTransportError(err error, d HTTPTransportDiagnostic) TransportDiagnosticFailureClass {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return DiagnosticFailureContextCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return DiagnosticFailureTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return DiagnosticFailureDNS
	}
	var certErr x509.UnknownAuthorityError
	if errors.As(err, &certErr) {
		return DiagnosticFailureTLSTrust
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return DiagnosticFailureTLSTrust
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return DiagnosticFailureTLSTrust
	}
	var tlsErr *tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return DiagnosticFailureTLSHandshake
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if d.TLSStarted && !d.TLSCompleted {
			return DiagnosticFailureTLSHandshake
		}
		if d.ConnectStarted && !d.ConnectCompleted {
			return DiagnosticFailureTCPConnect
		}
	}
	if d.TLSStarted && !d.TLSCompleted {
		return DiagnosticFailureTLSHandshake
	}
	if d.ProxyConfigured && d.ConnectCompleted && !d.TLSStarted && !d.RequestWritten {
		return DiagnosticFailureUnknown
	}
	if d.ConnectStarted && !d.ConnectCompleted {
		if d.ProxyConfigured {
			return DiagnosticFailureProxyConnect
		}
		return DiagnosticFailureTCPConnect
	}
	return DiagnosticFailureRoundTrip
}

func FinalizeHTTPTransportDiagnostic(d *HTTPTransportDiagnostic, err error, responseReceived bool) {
	if d == nil {
		return
	}
	if responseReceived {
		d.TLSCauseCode = ""
		d.HTTPResponseReceived = true
		d.Stage = DiagnosticStageResponseReceived
		d.FailureClass = ""
		return
	}
	if d.TLSStarted && !d.TLSCompleted && d.TLSCauseCode == "" {
		d.TLSCauseCode = ClassifyTLSCause(err)
	}
	d.FailureClass = ClassifyHTTPTransportError(err, *d)
	d.Timeout = errors.Is(err, context.DeadlineExceeded)
	d.ContextCanceled = errors.Is(err, context.Canceled)
	switch {
	case d.RedirectRejected:
		d.Stage, d.FailureClass = DiagnosticStageRedirectPolicy, DiagnosticFailureRedirectRejected
	case d.FirstResponseByte:
		d.Stage = DiagnosticStageResponseReceived
	case d.TLSStarted && !d.TLSCompleted:
		d.Stage = DiagnosticStageTLSHandshake
	case d.ProxyConfigured && d.ConnectCompleted && !d.TLSStarted && !d.RequestWritten:
		d.Stage = DiagnosticStageProxyNegotiation
	case d.ConnectStarted && !d.ConnectCompleted:
		d.Stage = DiagnosticStageConnect
	case d.DNSStarted && !d.DNSCompleted:
		d.Stage = DiagnosticStageDNS
	case d.RequestWritten:
		d.Stage = DiagnosticStageWaitResponse
	default:
		d.Stage = DiagnosticStagePrepare
	}
}
