package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type consumerWebBootstrapStep struct {
	response *http.Response
	err      error
}
type consumerWebBootstrapScript struct {
	steps    []consumerWebBootstrapStep
	requests []*http.Request
	proxies  []string
}

func (s *consumerWebBootstrapScript) Do(r *http.Request, p string, _ int64, _ int) (*http.Response, error) {
	s.requests = append(s.requests, r)
	s.proxies = append(s.proxies, p)
	if len(s.steps) == 0 {
		return nil, errors.New("unexpected dispatch")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return step.response, step.err
}
func (s *consumerWebBootstrapScript) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(r, p, id, n)
}
func consumerWebBootstrapFrame() string {
	inner := make([]any, 16)
	inner[14] = float64(1000)
	b, _ := json.Marshal(inner)
	f, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
	return string(f)
}
func consumerWebBootstrapURLs() *consumerWebEndpointSet {
	return &consumerWebEndpointSet{googlePreflight: "https://www.google.com/", geminiPrime: "https://gemini.google.com/?hl=en", app: "https://gemini.google.com/app", otaq: "https://gemini.google.com/_/BardChatUi/data/batchexecute", rotate: "https://accounts.google.com/RotateCookies"}
}
func consumerWebHasCookie(r *http.Request, name, value string) bool {
	for _, c := range r.Cookies() {
		if c.Name == name && c.Value == value {
			return true
		}
	}
	return false
}

func TestConsumerWebSourceAlignedCookieCompletion(t *testing.T) {
	script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{
		{response: consumerWebResp(200, "", &http.Cookie{Name: "NID", Value: "aux", Domain: ".google.com", Path: "/", Secure: true}, &http.Cookie{Name: "__Secure-1PSID", Value: "wrong", Domain: ".google.com", Path: "/", Secure: true}, &http.Cookie{Name: "__Secure-1PSIDTS", Value: "wrong", Domain: ".google.com", Path: "/", Secure: true})},
		{response: consumerWebResp(200, "", &http.Cookie{Name: "COMPASS", Value: "prime", Domain: ".google.com", Path: "/", Secure: true})},
		{response: consumerWebResp(200, `{"SNlM0e":"token"}`)},
		{response: consumerWebResp(200, consumerWebBootstrapFrame())},
	}}
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	account := consumerWebTestAccount()
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 1234, Status: StatusActive}
	result, err := tx.Bootstrap(context.Background(), account, consumerWebTestCreds(), nil)
	if err != nil || !result.Authenticated || len(script.requests) != 4 {
		t.Fatal("complete bootstrap failed")
	}
	for i, r := range script.requests {
		if script.proxies[i] != account.Proxy.URL() {
			t.Fatal("proxy changed across bootstrap")
		}
		if !consumerWebHasCookie(r, "__Secure-1PSID", "psid") || !consumerWebHasCookie(r, "__Secure-1PSIDTS", "psidts") {
			t.Fatal("authenticated cookie overwritten or missing")
		}
	}
	if !consumerWebHasCookie(script.requests[1], "NID", "aux") || !consumerWebHasCookie(script.requests[2], "NID", "aux") || !consumerWebHasCookie(script.requests[2], "COMPASS", "prime") {
		t.Fatal("auxiliary cookie did not reach later navigation")
	}
	if script.requests[0].URL.Hostname() != "www.google.com" || script.requests[1].URL.Hostname() != "gemini.google.com" || script.requests[2].URL.Path != "/app" {
		t.Fatal("bootstrap navigation order changed")
	}
	for _, r := range script.requests[:3] {
		if r.Header.Get("Accept-Language") == "" || r.Header.Get("Sec-Fetch-Mode") != "navigate" || r.Header.Get("Upgrade-Insecure-Requests") != "1" {
			t.Fatal("navigation headers missing")
		}
	}
}

func TestConsumerWebSourceAlignedEmptyAndFailurePriming(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second consumerWebBootstrapStep
	}{
		{"no_cookies", consumerWebBootstrapStep{response: consumerWebResp(200, "")}, consumerWebBootstrapStep{response: consumerWebResp(200, "")}},
		{"preflight_failure", consumerWebBootstrapStep{err: errors.New("synthetic preflight failure")}, consumerWebBootstrapStep{response: consumerWebResp(200, "")}},
		{"prime_failure", consumerWebBootstrapStep{response: consumerWebResp(200, "")}, consumerWebBootstrapStep{err: errors.New("synthetic prime failure")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{tc.first, tc.second, {response: consumerWebResp(200, `["SNlM0e","token"]`)}, {response: consumerWebResp(200, consumerWebBootstrapFrame())}}}
			tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
			res, err := tx.Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
			if err != nil || !res.Authenticated || len(script.requests) != 4 {
				t.Fatal("bounded priming failed")
			}
		})
	}
}

func TestConsumerWebSourceAlignedMissingTokenAndCompleteRecovery(t *testing.T) {
	for _, secondToken := range []bool{false, true} {
		secondBody := "no token"
		if secondToken {
			secondBody = `{"SNlM0e":"token"}`
		}
		script := &consumerWebBootstrapScript{steps: []consumerWebBootstrapStep{
			{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "no token")},
			{response: consumerWebResp(200, "")},
			{response: consumerWebResp(200, "")}, {response: consumerWebResp(200, "")}, {response: consumerWebResp(200, secondBody)},
		}}
		if secondToken {
			script.steps = append(script.steps, consumerWebBootstrapStep{response: consumerWebResp(200, consumerWebBootstrapFrame())})
		}
		tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: script, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
		creds := &sessionFakeCredentials{values: consumerWebTestCreds()}
		manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tx, Credentials: creds, RefreshInterval: time.Hour})
		_, err := manager.AcquireSession(context.Background(), consumerWebTestAccount())
		manager.Close()
		if secondToken && err != nil {
			t.Fatal("complete second bootstrap did not authenticate")
		}
		if !secondToken && !errors.Is(err, ErrConsumerWebAPPTokenMissing) {
			t.Fatal("missing token did not remain typed")
		}
		expected := 7
		if secondToken {
			expected = 8
		}
		if len(script.requests) != expected || len(script.steps) != 0 {
			t.Fatal("recovery did not repeat exactly one complete bootstrap")
		}
		paths := []string{"/", "/", "/app", "/RotateCookies", "/", "/", "/app"}
		for i, p := range paths {
			if script.requests[i].URL.Path != p {
				t.Fatal("recovery order mismatch")
			}
		}
	}
}

func TestConsumerWebSourceAlignedCookieIsolationAndInvalidation(t *testing.T) {
	urls := consumerWebBootstrapURLs()
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: &consumerWebBootstrapScript{}, Encryptor: consumerWebTransportEncryptor{}, Endpoints: urls})
	a := tx.newCredentialHandle("a-psid", "a-ts")
	b := tx.newCredentialHandle("b-psid", "b-ts")
	mergeConsumerWebAuxiliaryCookies(a.jar, urls.googlePreflight, []*http.Cookie{{Name: "NID", Value: "a-only", Domain: ".google.com", Path: "/", Secure: true}})
	if consumerWebHasCookie(&http.Request{Header: cookieHeaderFromJar(a.jar, urls.app)}, "NID", "a-only") == false {
		t.Fatal("same account auxiliary cookie missing")
	}
	if consumerWebHasCookie(&http.Request{Header: cookieHeaderFromJar(b.jar, urls.app)}, "NID", "a-only") {
		t.Fatal("cross-account cookie leak")
	}
	if err := tx.Close(context.Background(), a); err != nil || !a.closed.Load() {
		t.Fatal("session invalidation failed")
	}
	c := tx.newCredentialHandle("a-psid", "a-ts")
	if consumerWebHasCookie(&http.Request{Header: cookieHeaderFromJar(c.jar, urls.app)}, "NID", "a-only") {
		t.Fatal("auxiliary jar survived fresh session")
	}
}
func cookieHeaderFromJar(jar http.CookieJar, raw string) http.Header {
	h := http.Header{}
	u, _ := url.Parse(raw)
	for _, c := range jar.Cookies(u) {
		h.Add("Cookie", c.String())
	}
	return h
}

func TestConsumerWebSourceAlignedRedirectAndSecretSafety(t *testing.T) {
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: &consumerWebBootstrapScript{}, Encryptor: consumerWebTransportEncryptor{}, Endpoints: consumerWebBootstrapURLs()})
	for _, host := range []string{"www.google.com", "gemini.google.com", "accounts.google.com"} {
		u, _ := url.Parse("https://" + host + "/")
		if tx.redirectPolicy(&http.Request{URL: u, Header: http.Header{}}, nil) != nil {
			t.Fatal("trusted redirect rejected")
		}
	}
	u, _ := url.Parse("https://attacker.example/")
	if tx.redirectPolicy(&http.Request{URL: u, Header: http.Header{}}, nil) == nil {
		t.Fatal("untrusted redirect accepted")
	}
	if strings.Contains(fmt.Sprint(tx), "psid") {
		t.Fatal("transport contains credential")
	}
}
