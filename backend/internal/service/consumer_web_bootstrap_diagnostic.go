package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// ConsumerWebBootstrapStage names a fixed lifecycle hop, never a request URL.
type ConsumerWebBootstrapStage string

const (
	ConsumerWebStageGooglePreflight ConsumerWebBootstrapStage = "GOOGLE_PREFLIGHT"
	ConsumerWebStageGeminiPrime     ConsumerWebBootstrapStage = "GEMINI_PRIME"
	ConsumerWebStageGeminiAPP       ConsumerWebBootstrapStage = "GEMINI_APP"
	ConsumerWebStageRotateCookies   ConsumerWebBootstrapStage = "ROTATE_COOKIES"
	ConsumerWebStageOTAQ            ConsumerWebBootstrapStage = "OTAQ"
)

type ConsumerWebTransportCause string

const (
	ConsumerWebCauseDNSFailure          ConsumerWebTransportCause = "DNS_FAILURE"
	ConsumerWebCauseTCPConnectFailure   ConsumerWebTransportCause = "TCP_CONNECT_FAILURE"
	ConsumerWebCauseProxyConnectFailure ConsumerWebTransportCause = "PROXY_CONNECT_FAILURE"
	ConsumerWebCauseTLSHandshakeFailure ConsumerWebTransportCause = "TLS_HANDSHAKE_FAILURE"
	ConsumerWebCauseTLSEOF              ConsumerWebTransportCause = "TLS_EOF"
	ConsumerWebCauseTLSCertificate      ConsumerWebTransportCause = "TLS_CERTIFICATE_FAILURE"
	ConsumerWebCauseRequestTimeout      ConsumerWebTransportCause = "REQUEST_TIMEOUT"
	ConsumerWebCauseResponseTimeout     ConsumerWebTransportCause = "RESPONSE_HEADER_TIMEOUT"
	ConsumerWebCauseConnectionReset     ConsumerWebTransportCause = "CONNECTION_RESET"
	ConsumerWebCauseRedirectFailure     ConsumerWebTransportCause = "REDIRECT_FAILURE"
	ConsumerWebCauseBodyReadFailure     ConsumerWebTransportCause = "BODY_READ_FAILURE"
	ConsumerWebCauseContextCancelled    ConsumerWebTransportCause = "CONTEXT_CANCELLED"
	ConsumerWebCauseUnknownTransport    ConsumerWebTransportCause = "UNKNOWN_TRANSPORT"
)

// ConsumerWebBootstrapStep contains bounded, secret-free observations only.
type ConsumerWebBootstrapStep struct {
	Stage                    ConsumerWebBootstrapStage
	AttemptOrdinal           int
	RequestSent              bool
	HTTPResponseReceived     bool
	HTTPStatus               int
	TransportCause           ConsumerWebTransportCause
	TLSCause                 TLSCauseCode
	ProxyUsed                bool
	APPTokenPresent          bool
	APPInitMarkerCount       int
	APPFinalHostClass        string
	APPFinalPathClass        string
	RotateReplacementPresent bool
	RotatePersistAttempted   bool
	RotatePersistResult      string
	RotateCASConflict        bool
}

// ConsumerWebBootstrapTrace is request-scoped. It is not a second error system
// and never stores response bodies, cookies, URLs, or raw errors.
type ConsumerWebBootstrapTrace struct {
	mu    sync.Mutex
	steps []ConsumerWebBootstrapStep
}

type consumerWebBootstrapTraceKey struct{}

func WithConsumerWebBootstrapTrace(ctx context.Context, trace *ConsumerWebBootstrapTrace) context.Context {
	if trace == nil {
		return ctx
	}
	return context.WithValue(ctx, consumerWebBootstrapTraceKey{}, trace)
}

func consumerWebBootstrapTraceFromContext(ctx context.Context) *ConsumerWebBootstrapTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(consumerWebBootstrapTraceKey{}).(*ConsumerWebBootstrapTrace)
	return trace
}

func (t *ConsumerWebBootstrapTrace) Steps() []ConsumerWebBootstrapStep {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]ConsumerWebBootstrapStep(nil), t.steps...)
}

func (t *ConsumerWebBootstrapTrace) append(step ConsumerWebBootstrapStep) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	step.AttemptOrdinal = 1
	for _, prior := range t.steps {
		if prior.Stage == step.Stage {
			step.AttemptOrdinal++
		}
	}
	t.steps = append(t.steps, step)
}

func (t *ConsumerWebBootstrapTrace) updateLast(stage ConsumerWebBootstrapStage, update func(*ConsumerWebBootstrapStep)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].Stage == stage {
			update(&t.steps[i])
			return
		}
	}
}

// ConsumerWebBootstrapStageError retains the safe stage while preserving the
// existing errors.As chain down to HTTPTransportDiagnosticError.
type ConsumerWebBootstrapStageError struct {
	Stage ConsumerWebBootstrapStage
	Err   error
}

func (e *ConsumerWebBootstrapStageError) Error() string {
	return "consumer web bootstrap stage failure"
}
func (e *ConsumerWebBootstrapStageError) Unwrap() error { return e.Err }

func classifyConsumerWebTransportCause(err error, d HTTPTransportDiagnostic) ConsumerWebTransportCause {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || d.ContextCanceled {
		return ConsumerWebCauseContextCancelled
	}
	if errors.Is(err, ErrConsumerWebRedirectRejected) || d.RedirectRejected {
		return ConsumerWebCauseRedirectFailure
	}
	if d.TLSStarted && !d.TLSCompleted {
		switch normalizeTLSCauseCode(d.TLSCauseCode) {
		case TLSCauseEOF, TLSCauseUnexpectedEOF:
			return ConsumerWebCauseTLSEOF
		case TLSCauseX509UnknownAuthority, TLSCauseX509Hostname, TLSCauseX509CertInvalid:
			return ConsumerWebCauseTLSCertificate
		case TLSCauseConnectionReset, TLSCauseConnectionAborted:
			return ConsumerWebCauseConnectionReset
		case TLSCauseContextDeadline, TLSCauseTimeout:
			return ConsumerWebCauseRequestTimeout
		}
		return ConsumerWebCauseTLSHandshakeFailure
	}
	if errors.Is(err, context.DeadlineExceeded) || d.Timeout {
		if d.RequestWritten && !d.FirstResponseByte {
			return ConsumerWebCauseResponseTimeout
		}
		return ConsumerWebCauseRequestTimeout
	}
	var dns *net.DNSError
	if errors.As(err, &dns) || d.FailureClass == DiagnosticFailureDNS {
		return ConsumerWebCauseDNSFailure
	}
	switch d.FailureClass {
	case DiagnosticFailureProxyConnect, DiagnosticFailureProxyConfig:
		return ConsumerWebCauseProxyConnectFailure
	case DiagnosticFailureTCPConnect:
		return ConsumerWebCauseTCPConnectFailure
	case DiagnosticFailureTLSTrust:
		return ConsumerWebCauseTLSCertificate
	case DiagnosticFailureTLSHandshake:
		return ConsumerWebCauseTLSHandshakeFailure
	case DiagnosticFailureRedirectRejected:
		return ConsumerWebCauseRedirectFailure
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ConsumerWebCauseConnectionReset
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return ConsumerWebCauseRequestTimeout
	}
	return ConsumerWebCauseUnknownTransport
}

func consumerWebStepFromHTTP(stage ConsumerWebBootstrapStage, resp *http.Response, err error, d HTTPTransportDiagnostic) ConsumerWebBootstrapStep {
	step := ConsumerWebBootstrapStep{Stage: stage, RequestSent: d.RequestWritten || resp != nil, ProxyUsed: d.ProxyConfigured, TLSCause: d.TLSCauseCode}
	if resp != nil {
		step.HTTPResponseReceived = true
		step.HTTPStatus = resp.StatusCode
	}
	if err != nil {
		var typed *HTTPTransportDiagnosticError
		if errors.As(err, &typed) {
			d = typed.Diagnostic()
			step.ProxyUsed = d.ProxyConfigured
			step.TLSCause = d.TLSCauseCode
			step.RequestSent = d.RequestWritten || resp != nil
		}
		if !step.HTTPResponseReceived || errors.Is(err, ErrConsumerWebRedirectRejected) || d.RedirectRejected {
			step.TransportCause = classifyConsumerWebTransportCause(err, d)
		}
	}
	return step
}
