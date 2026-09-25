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
	DNSStarted, DNSCompleted                                bool
	ConnectStarted, ConnectCompleted                        bool
	TLSStarted, TLSCompleted                                bool
	RequestWritten, FirstResponseByte, HTTPResponseReceived bool
	Timeout, ContextCanceled, RedirectRejected              bool
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
	if d.ConnectStarted && !d.ConnectCompleted {
		return DiagnosticFailureTCPConnect
	}
	if d.TLSStarted && !d.TLSCompleted {
		return DiagnosticFailureTLSHandshake
	}
	return DiagnosticFailureRoundTrip
}
