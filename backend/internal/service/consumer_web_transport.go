package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	consumerWebGooglePreflightURL = "https://www.google.com/"
	consumerWebGeminiPrimeURL     = "https://gemini.google.com/?hl=en"
	consumerWebAppURL             = "https://gemini.google.com/app"
	consumerWebOTAQURL            = "https://gemini.google.com/_/BardChatUi/data/batchexecute"
	consumerWebRotateURL          = "https://accounts.google.com/RotateCookies"
)

var (
	ErrConsumerWebTransportTransient = errors.New("consumer web transport transient failure")
	ErrConsumerWebAuthLost           = errors.New("consumer web authentication lost")
	ErrConsumerWebProtocolDrift      = errors.New("consumer web protocol drift")
	ErrConsumerWebRateLimited        = errors.New("consumer web rate limited")
	ErrConsumerWebForbidden          = errors.New("consumer web forbidden")
	ErrConsumerWebTransportClosed    = errors.New("consumer web transport closed")
	ErrConsumerWebRedirectRejected   = errors.New("consumer web redirect rejected")
	ErrConsumerWebAPPTokenMissing    = errors.New("consumer web app session token missing")
)

type consumerWebEndpointSet struct{ googlePreflight, geminiPrime, app, otaq, rotate string }
type consumerWebRecoveryRoleKey struct{}

func withConsumerWebRecoveryRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, consumerWebRecoveryRoleKey{}, role)
}

// ConsumerWebEndpointSet is an internal test seam for local integration tests.
// Production construction leaves it nil and uses the fixed Google endpoints.
type ConsumerWebEndpointSet struct{ GooglePreflight, GeminiPrime, App, OTAQ, Rotate string }
type consumerWebHTTPHandle struct {
	jar                              http.CookieJar
	endpoints                        consumerWebEndpointSet
	language, build, sid, snlm, push string
	clientSessionID                  string
	registry                         []ConsumerWebModel
	closed                           atomic.Bool
	reqID                            atomic.Uint64
}

type ConsumerWebHTTPTransportOptions struct {
	HTTPUpstream  HTTPUpstream
	Encryptor     SecretEncryptor
	Timeout       time.Duration
	Endpoints     *consumerWebEndpointSet // internal test seam; production constructor leaves nil
	TestEndpoints *ConsumerWebEndpointSet // cross-package local integration seam
}

type ConsumerWebHTTPTransport struct {
	upstream  HTTPUpstream
	encryptor SecretEncryptor
	timeout   time.Duration
	endpoints consumerWebEndpointSet
	mu        sync.Mutex
}

func NewConsumerWebHTTPTransport(o ConsumerWebHTTPTransportOptions) *ConsumerWebHTTPTransport {
	e := consumerWebEndpointSet{googlePreflight: consumerWebGooglePreflightURL, geminiPrime: consumerWebGeminiPrimeURL, app: consumerWebAppURL, otaq: consumerWebOTAQURL, rotate: consumerWebRotateURL}
	if o.Endpoints != nil {
		e = *o.Endpoints
	} else if o.TestEndpoints != nil {
		e = consumerWebEndpointSet{googlePreflight: o.TestEndpoints.GooglePreflight, geminiPrime: o.TestEndpoints.GeminiPrime, app: o.TestEndpoints.App, otaq: o.TestEndpoints.OTAQ, rotate: o.TestEndpoints.Rotate}
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	return &ConsumerWebHTTPTransport{upstream: o.HTTPUpstream, encryptor: o.Encryptor, timeout: o.Timeout, endpoints: e}
}

func (t *ConsumerWebHTTPTransport) Bootstrap(ctx context.Context, account *Account, credentials map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	if account == nil || t.upstream == nil {
		return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireSessionInitFailed, Err: ErrConsumerWebTransportTransient}
	}
	status := ConsumerWebCredentialsStatus(credentials)
	if !status.HasSecure1PSID || !status.HasSecure1PSIDTS {
		return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireCredentialNamespaceMissing, Err: ErrConsumerWebAuthLost}
	}
	psid, psidts, err := DecryptConsumerWebCredentials(t.encryptor, credentials)
	if err != nil {
		return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireCredentialDecryptFailed, Err: ErrConsumerWebAuthLost}
	}
	if psid == "" || psidts == "" {
		return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireAccountNotReady, Err: ErrConsumerWebAuthLost}
	}
	h := t.newCredentialHandle(psid, psidts)
	jar := h.jar
	// Both navigation steps are bounded, best-effort cookie priming. The
	// durable authenticated pair always wins over response auxiliary cookies.
	for _, endpoint := range []string{t.endpoints.googlePreflight, t.endpoints.geminiPrime} {
		if endpoint == "" { // Older local endpoint fixtures intentionally omit navigation.
			continue
		}
		prime, primeErr := t.do(ctx, account, h, http.MethodGet, endpoint, nil, "")
		if primeErr != nil {
			if ctx.Err() != nil {
				return ConsumerWebBootstrapResult{}, primeErr
			}
			continue
		}
		if prime.StatusCode == http.StatusOK {
			mergeConsumerWebAuxiliaryCookies(jar, endpoint, prime.Cookies())
		}
		_ = prime.Body.Close()
	}
	resp, err := t.do(ctx, account, h, http.MethodGet, t.endpoints.app, nil, "")
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		consumerWebBootstrapTraceFromContext(ctx).updateLast(ConsumerWebStageGeminiAPP, func(s *ConsumerWebBootstrapStep) { s.TransportCause = ConsumerWebCauseBodyReadFailure })
		return ConsumerWebBootstrapResult{}, &ConsumerWebBootstrapStageError{Stage: ConsumerWebStageGeminiAPP, Err: fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)}
	}
	if resp.StatusCode != http.StatusOK {
		return ConsumerWebBootstrapResult{}, &ConsumerWebBootstrapStageError{Stage: ConsumerWebStageGeminiAPP, Err: mapConsumerWebStatus(resp.StatusCode)}
	}
	mergeConsumerWebAuxiliaryCookies(jar, t.endpoints.app, resp.Cookies())
	runtime := parseConsumerWebRuntime(string(body))
	consumerWebBootstrapTraceFromContext(ctx).updateLast(ConsumerWebStageGeminiAPP, func(s *ConsumerWebBootstrapStep) {
		s.APPTokenPresent = runtime.AuthToken != ""
		for _, present := range []bool{runtime.BuildLabel != "", runtime.SessionID != "", runtime.Language != "", runtime.PushID != ""} {
			if present {
				s.APPInitMarkerCount++
			}
		}
	})
	h.snlm, h.build, h.sid, h.language, h.push = runtime.AuthToken, runtime.BuildLabel, runtime.SessionID, runtime.Language, runtime.PushID
	if h.snlm == "" {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAPPTokenMissing
	}
	q, err := t.otAQ(ctx, account, h)
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	if !q.Authenticated {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAuthLost
	}
	h.registry = append([]ConsumerWebModel(nil), q.Registry...)
	return ConsumerWebBootstrapResult{Authenticated: true, BuildLabel: h.build, SessionID: h.sid, Language: h.language, Session: h, Registry: q.Registry}, nil
}

func (t *ConsumerWebHTTPTransport) otAQ(ctx context.Context, account *Account, h *consumerWebHTTPHandle) (ConsumerWebBootstrapResult, error) {
	h.reqID.Add(1)
	form := url.Values{"rpcids": {"otAQ7b"}, "source-path": {"/app"}, "hl": {h.language}, "_reqid": {strconv.FormatUint(h.reqID.Load(), 10)}, "rt": {"c"}, "f.req": {"[[[\"otAQ7b\",\"[]\",null,\"generic\"]]]"}, "at": {h.snlm}}
	if h.build != "" {
		form.Set("bl", h.build)
	}
	if h.sid != "" {
		form.Set("f.sid", h.sid)
	}
	resp, err := t.do(ctx, account, h, http.MethodPost, h.endpoints.otaq, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded;charset=UTF-8")
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ConsumerWebBootstrapResult{}, &ConsumerWebBootstrapStageError{Stage: ConsumerWebStageOTAQ, Err: mapConsumerWebStatus(resp.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		consumerWebBootstrapTraceFromContext(ctx).updateLast(ConsumerWebStageOTAQ, func(s *ConsumerWebBootstrapStep) { s.TransportCause = ConsumerWebCauseBodyReadFailure })
		return ConsumerWebBootstrapResult{}, &ConsumerWebBootstrapStageError{Stage: ConsumerWebStageOTAQ, Err: fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)}
	}
	status, registry, err := parseConsumerWebOTAQ(string(b))
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	return ConsumerWebBootstrapResult{Authenticated: status == 1000, Registry: registry, Session: h}, nil
}

func (t *ConsumerWebHTTPTransport) RotatePSIDTS(ctx context.Context, account *Account, session ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	h, ok := session.(*consumerWebHTTPHandle)
	if !ok || h.closed.Load() {
		return ConsumerWebRotationResult{}, ErrConsumerWebTransportClosed
	}
	psidts, err := t.rotate(ctx, account, h)
	if err != nil {
		return ConsumerWebRotationResult{}, err
	}
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: psidts, Session: h}, nil
}

// PreAuthRotatePSIDTS uses only the durable cookie pair; it does not publish an
// authenticated session. The Manager owns conditional persistence and retry.
func (t *ConsumerWebHTTPTransport) PreAuthRotatePSIDTS(ctx context.Context, account *Account, credentials map[string]any) (ConsumerWebRotationResult, error) {
	if account == nil || t.upstream == nil {
		return ConsumerWebRotationResult{}, ErrConsumerWebTransportTransient
	}
	psid, psidts, err := DecryptConsumerWebCredentials(t.encryptor, credentials)
	if err != nil || psid == "" {
		return ConsumerWebRotationResult{}, ErrConsumerWebAuthLost
	}
	h := t.newCredentialHandle(psid, psidts)
	rotateCookies := []*http.Cookie{{Name: "__Secure-1PSID", Value: psid}}
	if psidts != "" {
		rotateCookies = append(rotateCookies, &http.Cookie{Name: "__Secure-1PSIDTS", Value: psidts})
	}
	setConsumerWebCredentialCookies(h.jar, t.endpoints.rotate, rotateCookies)
	replacement, err := t.rotate(withConsumerWebRecoveryRole(ctx, "PREAUTH_ROTATE"), account, h)
	if err != nil {
		return ConsumerWebRotationResult{}, err
	}
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: replacement}, nil
}

func (t *ConsumerWebHTTPTransport) newCredentialHandle(psid, psidts string) *consumerWebHTTPHandle {
	jar, _ := cookiejar.New(nil)
	h := &consumerWebHTTPHandle{jar: jar, endpoints: t.endpoints, language: "en", clientSessionID: consumerWebClientSessionID()}
	cookies := []*http.Cookie{{Name: "__Secure-1PSID", Value: psid, Path: "/"}}
	if psidts != "" {
		cookies = append(cookies, &http.Cookie{Name: "__Secure-1PSIDTS", Value: psidts, Path: "/"})
	}
	setConsumerWebCredentialCookies(jar, t.endpoints.app, cookies)
	if t.endpoints.googlePreflight != "" {
		setConsumerWebCredentialCookies(jar, t.endpoints.googlePreflight, cookies)
	}
	if t.endpoints.geminiPrime != "" {
		setConsumerWebCredentialCookies(jar, t.endpoints.geminiPrime, cookies)
	}
	return h
}

func mergeConsumerWebAuxiliaryCookies(jar http.CookieJar, rawURL string, cookies []*http.Cookie) {
	u := mustURL(rawURL)
	for _, cookie := range cookies {
		if cookie.Name == "__Secure-1PSID" || cookie.Name == "__Secure-1PSIDTS" {
			continue
		}
		jar.SetCookies(u, []*http.Cookie{cookie})
	}
}

func setConsumerWebCredentialCookies(jar http.CookieJar, rawURL string, cookies []*http.Cookie) {
	u := mustURL(rawURL)
	for _, cookie := range cookies {
		jar.SetCookies(u, []*http.Cookie{{Name: cookie.Name, Value: cookie.Value, Path: "/", Secure: u.Scheme == "https"}})
	}
}

// rotate is the single RotateCookies wire implementation for both lifecycle paths.
func (t *ConsumerWebHTTPTransport) rotate(ctx context.Context, account *Account, h *consumerWebHTTPHandle) (string, error) {
	resp, err := t.do(ctx, account, h, http.MethodPost, h.endpoints.rotate, strings.NewReader(`[000,"-0000000000000000000"]`), "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", &ConsumerWebBootstrapStageError{Stage: ConsumerWebStageRotateCookies, Err: mapConsumerWebStatus(resp.StatusCode)}
	}
	var psidts string
	for _, c := range resp.Cookies() {
		if c.Name == "__Secure-1PSIDTS" && c.Value != "" {
			psidts = c.Value
			replacement := []*http.Cookie{{Name: c.Name, Value: c.Value}}
			setConsumerWebCredentialCookies(h.jar, h.endpoints.app, replacement)
		}
	}
	return psidts, nil
}

func (t *ConsumerWebHTTPTransport) Close(_ context.Context, session ConsumerWebSessionHandle) error {
	if h, ok := session.(*consumerWebHTTPHandle); ok {
		h.closed.Store(true)
		return nil
	}
	return ErrConsumerWebTransportClosed
}

func (t *ConsumerWebHTTPTransport) do(ctx context.Context, account *Account, h *consumerWebHTTPHandle, method, raw string, body io.Reader, contentType string) (*http.Response, error) {
	if h.closed.Load() {
		return nil, ErrConsumerWebTransportClosed
	}
	reqCtx, cancel := context.WithTimeout(ctx, t.timeout)
	if guard, _ := HTTPUpstreamDispatchGuardFromContext(ctx); guard != nil {
		role, _ := ctx.Value(consumerWebRecoveryRoleKey{}).(string)
		switch raw {
		case t.endpoints.googlePreflight:
			if role == "RETRY_APP" {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "RETRY_GOOGLE_PREFLIGHT")
			} else {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "GOOGLE_PREFLIGHT")
			}
		case t.endpoints.geminiPrime:
			if role == "RETRY_APP" {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "RETRY_GEMINI_PRIME")
			} else {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "GEMINI_PRIME")
			}
		case t.endpoints.app:
			if role == "RETRY_APP" {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, role)
			} else {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "APP")
			}
		case t.endpoints.otaq:
			reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "OTAQ")
		case t.endpoints.rotate:
			if role == "PREAUTH_ROTATE" {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, role)
			} else {
				reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "ROTATE")
			}
		}
	}
	req, err := http.NewRequestWithContext(reqCtx, method, raw, body)
	if err != nil {
		cancel()
		return nil, ErrConsumerWebProtocolDrift
	}
	diagnostic := &HTTPTransportDiagnostic{}
	var bootstrapStage ConsumerWebBootstrapStage
	switch raw {
	case t.endpoints.googlePreflight:
		bootstrapStage = ConsumerWebStageGooglePreflight
	case t.endpoints.geminiPrime:
		bootstrapStage = ConsumerWebStageGeminiPrime
	case t.endpoints.app:
		bootstrapStage = ConsumerWebStageGeminiAPP
	case t.endpoints.rotate:
		bootstrapStage = ConsumerWebStageRotateCookies
	case t.endpoints.otaq:
		bootstrapStage = ConsumerWebStageOTAQ
	}
	req = WithHTTPTransportDiagnostic(req, diagnostic)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if method == http.MethodGet {
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
	} else if raw == t.endpoints.otaq {
		req.Header.Set("Origin", "https://gemini.google.com")
		req.Header.Set("X-Same-Domain", "1")
	}
	if raw != t.endpoints.googlePreflight {
		req.Header.Set("Referer", "https://gemini.google.com/")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	u, _ := url.Parse(raw)
	for _, c := range h.jar.Cookies(u) {
		req.AddCookie(c)
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	var resp *http.Response
	if redirector, ok := t.upstream.(HTTPUpstreamRedirector); ok {
		resp, err = redirector.DoWithRedirectPolicy(req, proxy, account.ID, account.Concurrency, t.redirectPolicy)
	} else {
		resp, err = t.upstream.Do(req, proxy, account.ID, account.Concurrency)
	}
	if err != nil {
		cancel()
		consumerWebBootstrapTraceFromContext(ctx).append(consumerWebStepFromHTTP(bootstrapStage, resp, err, *diagnostic))
		if diagnostic.RedirectRejected || errors.Is(err, ErrConsumerWebRedirectRejected) {
			return nil, &ConsumerWebBootstrapStageError{Stage: bootstrapStage, Err: ErrConsumerWebRedirectRejected}
		}
		return nil, &ConsumerWebBootstrapStageError{Stage: bootstrapStage, Err: fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)}
	}
	if resp == nil || resp.Body == nil {
		cancel()
		consumerWebBootstrapTraceFromContext(ctx).append(consumerWebStepFromHTTP(bootstrapStage, resp, ErrConsumerWebTransportTransient, *diagnostic))
		return nil, &ConsumerWebBootstrapStageError{Stage: bootstrapStage, Err: ErrConsumerWebTransportTransient}
	}
	step := consumerWebStepFromHTTP(bootstrapStage, resp, nil, *diagnostic)
	if bootstrapStage == ConsumerWebStageGeminiAPP && resp.Request != nil && resp.Request.URL != nil {
		if strings.EqualFold(resp.Request.URL.Hostname(), "gemini.google.com") {
			step.APPFinalHostClass = "GEMINI"
		} else {
			step.APPFinalHostClass = "OTHER"
		}
		if resp.Request.URL.Path == "/app" {
			step.APPFinalPathClass = "APP"
		} else {
			step.APPFinalPathClass = "OTHER"
		}
	}
	consumerWebBootstrapTraceFromContext(ctx).append(step)
	resp.Body = &consumerWebCancelOnBodyDone{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// The request context must outlive response headers: callers read the body after do returns.
type consumerWebCancelOnBodyDone struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *consumerWebCancelOnBodyDone) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.cancel)
	}
	return n, err
}

func (b *consumerWebCancelOnBodyDone) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

func (t *ConsumerWebHTTPTransport) redirectPolicy(next *http.Request, via []*http.Request) error {
	if next == nil || next.URL == nil {
		return ErrConsumerWebRedirectRejected
	}
	host := strings.ToLower(next.URL.Hostname())
	if !strings.EqualFold(next.URL.Scheme, "https") || (host != "gemini.google.com" && host != "accounts.google.com" && host != "www.google.com" && host != "google.com") {
		return ErrConsumerWebRedirectRejected
	}
	if len(via) > 0 {
		for key := range next.Header {
			if strings.EqualFold(key, "Cookie") || strings.EqualFold(key, "Authorization") {
				next.Header.Del(key)
			}
		}
	}
	return nil
}

func mapConsumerWebStatus(code int) error {
	switch {
	case code == 401 || code == 403:
		return ErrConsumerWebAuthLost
	case code == 429:
		return ErrConsumerWebRateLimited
	case code >= 500:
		return ErrConsumerWebTransportTransient
	default:
		return fmt.Errorf("%w: status %d", ErrConsumerWebProtocolDrift, code)
	}
}
func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

type consumerWebRuntimeFields struct{ AuthToken, PushID, BuildLabel, SessionID, Language string }

func parseConsumerWebRuntime(body string) consumerWebRuntimeFields {
	// These patterns mirror the frozen reference's explicit regex declarations.
	value := func(key string) string {
		re := regexp.MustCompile(`(?:"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]*)"|\["` + regexp.QuoteMeta(key) + `"\s*,\s*"([^"]*)")`)
		m := re.FindStringSubmatch(body)
		if len(m) == 0 {
			return ""
		}
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return consumerWebRuntimeFields{AuthToken: value("SNlM0e"), PushID: defaultString(value("qKIAYe"), "feeds/mcudyrk2a4khkz"), BuildLabel: value("cfb2h"), SessionID: value("FdrFJe"), Language: defaultString(value("TuX5cc"), "en")}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parseConsumerWebOTAQ(body string) (int, []ConsumerWebModel, error) {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, ")]}'") {
		body = strings.TrimSpace(strings.TrimPrefix(body, ")]}'"))
	}
	var status int
	var registry []ConsumerWebModel
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var root []any
		if _, err := strconv.Atoi(line); err == nil {
			continue
		}
		if json.Unmarshal([]byte(line), &root) != nil {
			continue
		}
		var frames [][]any
		if len(root) > 0 {
			if kind, ok := root[0].(string); ok && kind == "wrb.fr" {
				// Historical direct-frame responses remain supported.
				frames = append(frames, root)
			} else {
				// Batchexecute line root contains immediate child frames.
				for _, item := range root {
					if frame, ok := item.([]any); ok {
						frames = append(frames, frame)
					}
				}
			}
		}
		for _, frame := range frames {
			if len(frame) < 3 || frame[0] != "wrb.fr" || frame[1] != "otAQ7b" {
				continue
			}
			payload, ok := frame[2].(string)
			if !ok || payload == "" {
				return 0, nil, ErrConsumerWebProtocolDrift
			}
			var inner []any
			if json.Unmarshal([]byte(payload), &inner) != nil {
				return 0, nil, ErrConsumerWebProtocolDrift
			}
			if len(inner) <= 14 {
				return 0, nil, ErrConsumerWebProtocolDrift
			}
			switch v := inner[14].(type) {
			case nil:
				// Primary reference uses nil at [14] for an available session.
				status = 1000
			case float64:
				status = int(v)
				if status != 1000 && status != 1016 {
					return 0, nil, ErrConsumerWebProtocolDrift
				}
			default:
				return 0, nil, ErrConsumerWebProtocolDrift
			}
			if len(inner) > 15 {
				capacity, capacityField, profilePresent := consumerWebModelCapacity(inner)
				if arr, ok := inner[15].([]any); ok {
					for _, v := range arr {
						if model, ok := consumerWebRegistryModel(v); ok {
							if profilePresent {
								model.Capacity, model.CapacityField, model.ProfilePresent = capacity, capacityField, true
							}
							registry = append(registry, model)
						}
					}
				}
			}
			return status, registry, nil
		}
	}
	return 0, nil, ErrConsumerWebProtocolDrift
}

// The authenticated otAQ payload carries account tier and capability flags
// at [16] and [17]. This priority is the frozen Consumer Web source contract.
func consumerWebModelCapacity(inner []any) (capacity, field int, present bool) {
	if len(inner) <= 17 {
		return 0, 0, false
	}
	tiers, tiersOK := inner[16].([]any)
	capabilities, capabilitiesOK := inner[17].([]any)
	if !tiersOK || !capabilitiesOK {
		return 0, 0, false
	}
	has := func(items []any, want int) bool {
		for _, item := range items {
			if number, ok := item.(float64); ok && int(number) == want && number == float64(want) {
				return true
			}
		}
		return false
	}
	switch {
	case has(tiers, 21):
		return 1, 13, true
	case has(tiers, 22):
		return 2, 13, true
	case has(capabilities, 115):
		return 4, 12, true
	case has(tiers, 16) || has(capabilities, 106):
		return 3, 12, true
	case has(tiers, 8) || has(capabilities, 19):
		return 2, 12, true
	default:
		return 1, 12, true
	}
}

func consumerWebRegistryModel(value any) (ConsumerWebModel, bool) {
	if entry, ok := value.([]any); ok {
		// Authenticated otAQ entries use positional metadata: ID [0],
		// primary name [1], tier [4], and display name [11].
		if len(entry) <= 11 {
			return ConsumerWebModel{}, false
		}
		id, _ := entry[0].(string)
		display, _ := entry[11].(string)
		if display == "" {
			display, _ = entry[1].(string)
		}
		if id == "" || display == "" {
			return ConsumerWebModel{}, false
		}
		tier := ""
		switch typed := entry[4].(type) {
		case string:
			tier = typed
		case float64:
			tier = strconv.FormatFloat(typed, 'f', -1, 64)
		}
		modelNumber := 1
		for _, index := range []int{17, 9} {
			if len(entry) > index {
				if number, ok := entry[index].(float64); ok && number >= 0 && number == float64(int(number)) {
					modelNumber = int(number)
					break
				}
			}
		}
		return ConsumerWebModel{ID: id, Display: display, Tier: tier, ModelNumber: modelNumber}, true
	}
	if entry, ok := value.(map[string]any); ok {
		id, _ := entry["id"].(string)
		display, _ := entry["display"].(string)
		tier, _ := entry["tier"].(string)
		if id != "" && display != "" {
			return ConsumerWebModel{ID: id, Display: display, Tier: tier}, true
		}
	}
	return ConsumerWebModel{}, false
}
