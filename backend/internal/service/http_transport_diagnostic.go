package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
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
}

// SafeTransportDiagnostic exposes only non-sensitive lifecycle metadata.
func SafeTransportDiagnostic(d HTTPTransportDiagnostic) map[string]any {
	return map[string]any{
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
	return &HTTPTransportDiagnosticError{diag: diag, cause: cause}
}
func (e *HTTPTransportDiagnosticError) Error() string { return "http transport diagnostic failure" }
func (e *HTTPTransportDiagnosticError) Unwrap() error {
	// Network errors may embed proxy URLs or credentials. Preserve only context
	// sentinels through errors.Is; structured diagnostics carry the safe stage
	// and class metadata for all other causes.
	if errors.Is(e.cause, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
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
		d.HTTPResponseReceived = true
		d.Stage = DiagnosticStageResponseReceived
		d.FailureClass = ""
		return
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
