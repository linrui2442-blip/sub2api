package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type consumerWebClientUpstream struct{ directHits atomic.Int32 }

func (u *consumerWebClientUpstream) client(proxy string, policy func(*http.Request, []*http.Request) error) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		p, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(p)
	}
	return &http.Client{Transport: tr, CheckRedirect: policy}, nil
}
func (u *consumerWebClientUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	c, err := u.client(proxy, nil)
	if err != nil {
		return nil, err
	}
	if proxy == "" {
		u.directHits.Add(1)
	}
	return c.Do(r)
}
func (u *consumerWebClientUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}
func (u *consumerWebClientUpstream) DoWithRedirectPolicy(r *http.Request, p string, _ int64, _ int, policy func(*http.Request, []*http.Request) error) (*http.Response, error) {
	c, err := u.client(p, policy)
	if err != nil {
		return nil, err
	}
	if p == "" {
		u.directHits.Add(1)
	}
	return c.Do(r)
}

func TestConsumerWebTransportDoesNotFallbackDirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetHits.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	u := &consumerWebClientUpstream{}
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Timeout: time.Second, Endpoints: &consumerWebEndpointSet{app: target.URL, otaq: target.URL, rotate: target.URL}})
	acct := consumerWebTestAccount()
	acct.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: addr.Port, Status: StatusActive}
	_, err = tx.Bootstrap(context.Background(), acct, consumerWebTestCreds(), nil)
	if err == nil || !errors.Is(err, ErrConsumerWebTransportTransient) || targetHits.Load() != 0 || u.directHits.Load() != 0 {
		t.Fatal("proxy failure used direct fallback or lost typed error")
	}
}

func TestConsumerWebTransportRejectsUntrustedRedirect(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var targetHits atomic.Int32
			b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetHits.Add(1); w.WriteHeader(200) }))
			defer b.Close()
			a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Location", b.URL); w.WriteHeader(code) }))
			defer a.Close()
			u := &consumerWebClientUpstream{}
			tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Endpoints: &consumerWebEndpointSet{app: a.URL, otaq: a.URL, rotate: a.URL}})
			_, err := tx.Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
			if !errors.Is(err, ErrConsumerWebRedirectRejected) || targetHits.Load() != 0 {
				t.Fatal("untrusted redirect reached target")
			}
		})
	}
}

func TestConsumerWebTransportContextCancellation(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer s.Close()
	u := &consumerWebClientUpstream{}
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Timeout: time.Second, Endpoints: &consumerWebEndpointSet{app: s.URL, otaq: s.URL, rotate: s.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tx.Bootstrap(ctx, consumerWebTestAccount(), consumerWebTestCreds(), nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || u.directHits.Load() != 1 {
		t.Fatal("cancellation not propagated or request retried")
	}
}

func TestConsumerWebTransportTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	u := &consumerWebClientUpstream{}
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Timeout: 20 * time.Millisecond, Endpoints: &consumerWebEndpointSet{app: s.URL, otaq: s.URL, rotate: s.URL}})
	_, err := tx.Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
	if !errors.Is(err, context.DeadlineExceeded) || u.directHits.Load() != 1 {
		t.Fatal("timeout not propagated or request retried")
	}
}

func TestConsumerWebTransportSecretSafeErrors(t *testing.T) {
	markers := []string{"cw-secret-psid", "cw-secret-psidts", "cw-secret-at", "cw-secret-runtime", "cw-secret-cookie"}
	errs := []error{ErrConsumerWebAuthLost, ErrConsumerWebProtocolDrift, ErrConsumerWebRateLimited, ErrConsumerWebForbidden, ErrConsumerWebTransportTransient, ErrConsumerWebTransportClosed, ErrConsumerWebRedirectRejected, context.Canceled, context.DeadlineExceeded}
	for _, err := range errs {
		for e := err; e != nil; e = errors.Unwrap(e) {
			for _, marker := range markers {
				if strings.Contains(e.Error(), marker) {
					t.Fatal("unexpected secret marker found")
				}
			}
		}
	}
}

func TestConsumerWebTransportEmitsNoLogs(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(strings.TrimSuffix(file, "_security_test.go") + ".go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, token := range []string{"log.", "slog.", "logger."} {
		if strings.Contains(s, token) {
			t.Fatal("transport logging path added")
		}
	}
}

var errRealGoogleNetworkBlocked = errors.New("real google network blocked")

type consumerWebGoogleGuard struct{ networkAttempts atomic.Int32 }

func (g *consumerWebGoogleGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	h := strings.ToLower(r.URL.Hostname())
	if h == "gemini.google.com" || h == "accounts.google.com" || h == "www.google.com" {
		return nil, errRealGoogleNetworkBlocked
	}
	g.networkAttempts.Add(1)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
}
func TestConsumerWebTestNetworkGuardBlocksGoogle(t *testing.T) {
	g := &consumerWebGoogleGuard{}
	c := &http.Client{Transport: g}
	for _, host := range []string{"gemini.google.com", "accounts.google.com", "www.google.com"} {
		req, _ := http.NewRequest(http.MethodGet, "https://"+host+"/", nil)
		_, err := c.Do(req)
		if !errors.Is(err, errRealGoogleNetworkBlocked) {
			t.Fatal("guard did not block Google host")
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/test", nil)
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 || g.networkAttempts.Load() != 1 {
		t.Fatal("guard blocked localhost or attempted Google network")
	}
}
