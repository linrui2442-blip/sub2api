package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func diagnosticFixtureError(root error, d HTTPTransportDiagnostic) error {
	FinalizeHTTPTransportDiagnostic(&d, root, false)
	return NewHTTPTransportDiagnosticError(d, root)
}

func TestConsumerWebBootstrapSafeTransportCauseMatrix(t *testing.T) {
	dns := &net.DNSError{IsNotFound: true}
	cases := []struct {
		name string
		root error
		diag HTTPTransportDiagnostic
		want ConsumerWebTransportCause
	}{
		{"dns", dns, HTTPTransportDiagnostic{DNSStarted: true}, ConsumerWebCauseDNSFailure},
		{"tcp", errors.New("private marker"), HTTPTransportDiagnostic{ConnectStarted: true}, ConsumerWebCauseTCPConnectFailure},
		{"proxy", errors.New("private marker"), HTTPTransportDiagnostic{ProxyConfigured: true, ConnectStarted: true}, ConsumerWebCauseProxyConnectFailure},
		{"tls_eof", io.EOF, HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseEOF}, ConsumerWebCauseTLSEOF},
		{"tls_cert", errors.New("private marker"), HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseX509Hostname}, ConsumerWebCauseTLSCertificate},
		{"request_timeout", context.DeadlineExceeded, HTTPTransportDiagnostic{}, ConsumerWebCauseRequestTimeout},
		{"response_timeout", context.DeadlineExceeded, HTTPTransportDiagnostic{RequestWritten: true}, ConsumerWebCauseResponseTimeout},
		{"reset", errors.New("private marker"), HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseConnectionReset}, ConsumerWebCauseConnectionReset},
		{"redirect", ErrConsumerWebRedirectRejected, HTTPTransportDiagnostic{RedirectRejected: true}, ConsumerWebCauseRedirectFailure},
		{"cancel", context.Canceled, HTTPTransportDiagnostic{}, ConsumerWebCauseContextCancelled},
		{"unknown", errors.New("private marker"), HTTPTransportDiagnostic{}, ConsumerWebCauseUnknownTransport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := diagnosticFixtureError(tc.root, tc.diag)
			var typed *HTTPTransportDiagnosticError
			if !errors.As(err, &typed) {
				t.Fatal("typed diagnostic missing")
			}
			if got := classifyConsumerWebTransportCause(err, typed.Diagnostic()); got != tc.want {
				t.Fatalf("cause = %s, want %s", got, tc.want)
			}
			if strings.Contains(err.Error(), "private marker") {
				t.Fatal("raw transport error leaked")
			}
		})
	}
}

func TestConsumerWebBootstrapStageTraceAndErrorChain(t *testing.T) {
	const secret = "private-cookie-proxy-marker"
	root := fmt.Errorf("%s: %w", secret, io.EOF)
	tlsErr := diagnosticFixtureError(root, HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseEOF})
	script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{
		{response: consumerWebResp(200, "")},
		{response: consumerWebResp(200, "")},
		{err: tlsErr},
	}}
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: consumerWebTestCreds()}, RefreshInterval: time.Hour})
	defer m.Close()
	trace := &ConsumerWebBootstrapTrace{}
	_, err := m.AcquireSession(WithConsumerWebBootstrapTrace(context.Background(), trace), consumerWebTestAccount())
	steps := trace.Steps()
	if len(steps) != 3 || steps[0].Stage != ConsumerWebStageGooglePreflight || steps[1].Stage != ConsumerWebStageGeminiPrime || steps[2].Stage != ConsumerWebStageGeminiAPP {
		t.Fatalf("unexpected safe stage sequence: %+v", steps)
	}
	if steps[2].HTTPResponseReceived || steps[2].TransportCause != ConsumerWebCauseTLSEOF || steps[2].TLSCause != TLSCauseEOF {
		t.Fatalf("TLS transport distinction lost: %+v", steps[2])
	}
	var stage *ConsumerWebBootstrapStageError
	var transport *HTTPTransportDiagnosticError
	var tlsCause *TLSCauseDiagnosticError
	if !errors.As(err, &stage) || stage.Stage != ConsumerWebStageGeminiAPP || !errors.As(err, &transport) || !errors.As(err, &tlsCause) || tlsCause.Code != TLSCauseEOF {
		t.Fatal("stage or HTTP/TLS error chain lost through manager")
	}
	if strings.Contains(fmt.Sprint(steps), secret) || strings.Contains(err.Error(), secret) {
		t.Fatal("secret leaked from trace or error")
	}
}

func TestConsumerWebBootstrapHTTPAndTokenDistinction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"forbidden", 403, "", ErrConsumerWebAuthLost},
		{"missing_token", 200, "no token", ErrConsumerWebAPPTokenMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")}, {response: consumerWebResp(tc.status, tc.body)}}}
			tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
			trace := &ConsumerWebBootstrapTrace{}
			_, err := tr.Bootstrap(WithConsumerWebBootstrapTrace(context.Background(), trace), consumerWebTestAccount(), consumerWebTestCreds(), nil)
			steps := trace.Steps()
			if !errors.Is(err, tc.want) || len(steps) != 3 || !steps[2].HTTPResponseReceived || steps[2].HTTPStatus != tc.status || steps[2].TransportCause != "" {
				t.Fatalf("HTTP and transport collapsed: err=%v steps=%+v", err, steps)
			}
			if tc.status == 200 && steps[2].APPTokenPresent {
				t.Fatal("missing token misreported as present")
			}
		})
	}
}

func TestConsumerWebBootstrapPrimingAndRotateStages(t *testing.T) {
	dnsErr := diagnosticFixtureError(&net.DNSError{IsNotFound: true}, HTTPTransportDiagnostic{DNSStarted: true})
	proxyErr := diagnosticFixtureError(errors.New("private proxy marker"), HTTPTransportDiagnostic{ProxyConfigured: true, ConnectStarted: true})
	script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{
		{err: dnsErr}, {err: proxyErr}, {response: consumerWebResp(200, `{"SNlM0e":"token"}`)}, {response: consumerWebResp(200, consumerWebBootstrapFrame())},
	}}
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	trace := &ConsumerWebBootstrapTrace{}
	result, err := tr.Bootstrap(WithConsumerWebBootstrapTrace(context.Background(), trace), consumerWebTestAccount(), consumerWebTestCreds(), nil)
	steps := trace.Steps()
	if err != nil || !result.Authenticated || len(steps) != 4 || steps[0].TransportCause != ConsumerWebCauseDNSFailure || steps[1].TransportCause != ConsumerWebCauseProxyConnectFailure || !steps[2].APPTokenPresent || steps[3].Stage != ConsumerWebStageOTAQ {
		t.Fatalf("priming trace failed: err=%v steps=%+v", err, steps)
	}
	rotateScript := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{{err: proxyErr}}}
	rotate := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: rotateScript, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	rotateTrace := &ConsumerWebBootstrapTrace{}
	_, err = rotate.PreAuthRotatePSIDTS(WithConsumerWebBootstrapTrace(context.Background(), rotateTrace), consumerWebTestAccount(), consumerWebTestCreds())
	rotateSteps := rotateTrace.Steps()
	var stage *ConsumerWebBootstrapStageError
	if !errors.As(err, &stage) || stage.Stage != ConsumerWebStageRotateCookies || len(rotateSteps) != 1 || rotateSteps[0].TransportCause != ConsumerWebCauseProxyConnectFailure {
		t.Fatalf("rotate stage lost: err=%v steps=%+v", err, rotateSteps)
	}
	if strings.Contains(fmt.Sprint(rotateSteps), "private proxy marker") {
		t.Fatal("rotate trace leaked secret")
	}
}

func TestConsumerWebBootstrapAPPTimeoutAndOTAQFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		steps     []consumerWebBootstrapStep
		wantStage ConsumerWebBootstrapStage
		wantCause ConsumerWebTransportCause
	}{
		{
			name: "app_timeout",
			steps: []consumerWebBootstrapStep{
				{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")},
				{err: diagnosticFixtureError(context.DeadlineExceeded, HTTPTransportDiagnostic{RequestWritten: true})},
			},
			wantStage: ConsumerWebStageGeminiAPP, wantCause: ConsumerWebCauseResponseTimeout,
		},
		{
			name: "otaq_transport",
			steps: []consumerWebBootstrapStep{
				{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")},
				{response: consumerWebResp(200, `{"SNlM0e":"token"}`)},
				{err: diagnosticFixtureError(io.EOF, HTTPTransportDiagnostic{TLSStarted: true, TLSCauseCode: TLSCauseEOF})},
			},
			wantStage: ConsumerWebStageOTAQ, wantCause: ConsumerWebCauseTLSEOF,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &consumerWebBootstrapScript{steps: tc.steps}
			tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
			trace := &ConsumerWebBootstrapTrace{}
			_, err := tr.Bootstrap(WithConsumerWebBootstrapTrace(context.Background(), trace), consumerWebTestAccount(), consumerWebTestCreds(), nil)
			steps := trace.Steps()
			if err == nil || len(steps) == 0 || steps[len(steps)-1].Stage != tc.wantStage || steps[len(steps)-1].TransportCause != tc.wantCause || steps[len(steps)-1].HTTPResponseReceived {
				t.Fatalf("transport stage/cause mismatch: err=%v steps=%+v", err, steps)
			}
		})
	}
}

func TestConsumerWebBootstrapTraceDoesNotRetainRequest(t *testing.T) {
	trace := &ConsumerWebBootstrapTrace{}
	trace.append(consumerWebStepFromHTTP(ConsumerWebStageGeminiAPP, &http.Response{StatusCode: 200}, nil, HTTPTransportDiagnostic{}))
	steps := trace.Steps()
	if len(steps) != 1 || !steps[0].HTTPResponseReceived || steps[0].HTTPStatus != 200 {
		t.Fatal("HTTP response metadata lost")
	}
}

func TestConsumerWebBootstrapRotateNoReplacementTrace(t *testing.T) {
	script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{
		{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "no token")},
		{response: consumerWebResp(200, "")},
		{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "no token")},
	}}
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: consumerWebTestCreds()}, RefreshInterval: time.Hour})
	defer m.Close()
	trace := &ConsumerWebBootstrapTrace{}
	_, err := m.AcquireSession(WithConsumerWebBootstrapTrace(context.Background(), trace), consumerWebTestAccount())
	if !errors.Is(err, ErrConsumerWebAPPTokenMissing) {
		t.Fatal("missing app token changed classification")
	}
	steps := trace.Steps()
	var rotate *ConsumerWebBootstrapStep
	for i := range steps {
		if steps[i].Stage == ConsumerWebStageRotateCookies {
			rotate = &steps[i]
		}
	}
	if rotate == nil || rotate.HTTPStatus != 200 || rotate.RotateReplacementPresent || rotate.RotatePersistAttempted || rotate.RotatePersistResult != "NOT_ATTEMPTED_NO_REPLACEMENT" {
		t.Fatalf("bounded no-replacement rotation trace incorrect: %+v", rotate)
	}
	if len(steps) != 7 || steps[6].Stage != ConsumerWebStageGeminiAPP || steps[6].AttemptOrdinal != 2 {
		t.Fatalf("retry stage ordinal incorrect: %+v", steps)
	}
}
