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
	consumerWebAppURL    = "https://gemini.google.com/app"
	consumerWebOTAQURL   = "https://gemini.google.com/_/BardChatUi/data/batchexecute"
	consumerWebRotateURL = "https://accounts.google.com/RotateCookies"
)

var (
	ErrConsumerWebTransportTransient = errors.New("consumer web transport transient failure")
	ErrConsumerWebAuthLost           = errors.New("consumer web authentication lost")
	ErrConsumerWebProtocolDrift      = errors.New("consumer web protocol drift")
	ErrConsumerWebRateLimited        = errors.New("consumer web rate limited")
	ErrConsumerWebForbidden          = errors.New("consumer web forbidden")
	ErrConsumerWebTransportClosed    = errors.New("consumer web transport closed")
	ErrConsumerWebRedirectRejected   = errors.New("consumer web redirect rejected")
)

type consumerWebEndpointSet struct{ app, otaq, rotate string }

// ConsumerWebEndpointSet is an internal test seam for local integration tests.
// Production construction leaves it nil and uses the fixed Google endpoints.
type ConsumerWebEndpointSet struct{ App, OTAQ, Rotate string }
type consumerWebHTTPHandle struct {
	jar                        http.CookieJar
	endpoints                  consumerWebEndpointSet
	language, build, sid, snlm string
	closed                     atomic.Bool
	reqID                      atomic.Uint64
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
	e := consumerWebEndpointSet{app: consumerWebAppURL, otaq: consumerWebOTAQURL, rotate: consumerWebRotateURL}
	if o.Endpoints != nil {
		e = *o.Endpoints
	} else if o.TestEndpoints != nil {
		e = consumerWebEndpointSet{app: o.TestEndpoints.App, otaq: o.TestEndpoints.OTAQ, rotate: o.TestEndpoints.Rotate}
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	return &ConsumerWebHTTPTransport{upstream: o.HTTPUpstream, encryptor: o.Encryptor, timeout: o.Timeout, endpoints: e}
}

func (t *ConsumerWebHTTPTransport) Bootstrap(ctx context.Context, account *Account, credentials map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	if account == nil || t.upstream == nil {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebTransportTransient
	}
	psid, psidts, err := DecryptConsumerWebCredentials(t.encryptor, credentials)
	if err != nil || psid == "" || psidts == "" {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAuthLost
	}
	jar, _ := cookiejar.New(nil)
	h := &consumerWebHTTPHandle{jar: jar, endpoints: t.endpoints, language: "en"}
	jar.SetCookies(mustURL(t.endpoints.app), []*http.Cookie{{Name: "__Secure-1PSID", Value: psid, Domain: "gemini.google.com", Path: "/", Secure: true}, {Name: "__Secure-1PSIDTS", Value: psidts, Domain: "gemini.google.com", Path: "/", Secure: true}})
	resp, err := t.do(ctx, account, h, http.MethodGet, t.endpoints.app, nil, "text/html,application/xhtml+xml")
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return ConsumerWebBootstrapResult{}, fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)
	}
	if resp.StatusCode != http.StatusOK {
		return ConsumerWebBootstrapResult{}, mapConsumerWebStatus(resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		jar.SetCookies(mustURL(t.endpoints.app), []*http.Cookie{c})
	}
	runtime := parseConsumerWebRuntime(string(body))
	h.snlm, h.build, h.sid, h.language = runtime.AuthToken, runtime.BuildLabel, runtime.SessionID, runtime.Language
	if h.snlm == "" {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAuthLost
	}
	q, err := t.otAQ(ctx, account, h)
	if err != nil {
		return ConsumerWebBootstrapResult{}, err
	}
	if !q.Authenticated {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAuthLost
	}
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
		return ConsumerWebBootstrapResult{}, mapConsumerWebStatus(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return ConsumerWebBootstrapResult{}, fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)
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
	resp, err := t.do(ctx, account, h, http.MethodPost, h.endpoints.rotate, strings.NewReader(`[000,"-0000000000000000000"]`), "application/json")
	if err != nil {
		return ConsumerWebRotationResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ConsumerWebRotationResult{}, mapConsumerWebStatus(resp.StatusCode)
	}
	var psidts string
	for _, c := range resp.Cookies() {
		if c.Name == "__Secure-1PSIDTS" && c.Value != "" {
			psidts = c.Value
			h.jar.SetCookies(mustURL(h.endpoints.app), []*http.Cookie{c})
		}
	}
	if psidts == "" {
		return ConsumerWebRotationResult{}, ErrConsumerWebProtocolDrift
	}
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: psidts, Session: h}, nil
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
		switch raw {
		case t.endpoints.app:
			reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "APP")
		case t.endpoints.otaq:
			reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "OTAQ")
		case t.endpoints.rotate:
			reqCtx = WithHTTPUpstreamDispatchRole(reqCtx, "ROTATE")
		}
	}
	req, err := http.NewRequestWithContext(reqCtx, method, raw, body)
	if err != nil {
		cancel()
		return nil, ErrConsumerWebProtocolDrift
	}
	diagnostic := &HTTPTransportDiagnostic{}
	req = WithHTTPTransportDiagnostic(req, diagnostic)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Referer", "https://gemini.google.com/")
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
		if diagnostic.RedirectRejected || errors.Is(err, ErrConsumerWebRedirectRejected) {
			return nil, ErrConsumerWebRedirectRejected
		}
		return nil, fmt.Errorf("%w: %w", ErrConsumerWebTransportTransient, err)
	}
	if resp == nil || resp.Body == nil {
		cancel()
		return nil, ErrConsumerWebTransportTransient
	}
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
	if !strings.EqualFold(next.URL.Scheme, "https") || (host != "gemini.google.com" && host != "accounts.google.com") {
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
				if arr, ok := inner[15].([]any); ok {
					for _, v := range arr {
						if m, ok := v.(map[string]any); ok {
							id, _ := m["id"].(string)
							display, _ := m["display"].(string)
							tier, _ := m["tier"].(string)
							if id != "" {
								registry = append(registry, ConsumerWebModel{ID: id, Display: display, Tier: tier})
							}
						}
					}
				}
			}
			return status, registry, nil
		}
	}
	return 0, nil, ErrConsumerWebProtocolDrift
}
