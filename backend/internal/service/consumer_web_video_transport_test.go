package service

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type videoUpstreamFixture struct {
	responses []*http.Response
	requests  []*http.Request
	proxies   []string
}

func (f *videoUpstreamFixture) Do(r *http.Request, p string, _ int64, _ int) (*http.Response, error) {
	f.requests = append(f.requests, r)
	f.proxies = append(f.proxies, p)
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}
func (f *videoUpstreamFixture) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return f.Do(r, p, id, n)
}
func (f *videoUpstreamFixture) DoWithRedirectPolicy(r *http.Request, p string, id int64, n int, _ func(*http.Request, []*http.Request) error) (*http.Response, error) {
	return f.Do(r, p, id, n)
}
func videoFixtureResponse(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}
func videoFixtureHandle(t *testing.T) *consumerWebHTTPHandle {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(consumerWebAppURL)
	jar.SetCookies(u, []*http.Cookie{{Name: "__Secure-1PSID", Value: "synthetic", Domain: ".google.com", Path: "/", Secure: true}})
	return &consumerWebHTTPHandle{jar: jar, snlm: "synthetic-at", language: "en", clientSessionID: "synthetic-session", registry: []ConsumerWebModel{{ID: "current-pro", Display: "Pro", Capacity: 2, CapacityField: 12, ModelNumber: 3, ProfilePresent: true}, {ID: "synthetic-model", Display: "Pro", Capacity: 2, CapacityField: 12, ModelNumber: 3, ProfilePresent: true}}}
}

func TestConsumerWebVideoGenerateAndMediaReadinessTransport(t *testing.T) {
	meta := make([]any, 12)
	meta[2], meta[7], meta[11] = "video.mp4", []any{"https://contribution.usercontent.google.com/test"}, "video/mp4"
	group := []any{[]any{[]any{[]any{meta}}}}
	body := string(consumerWebVideoFixture(t, []any{map[string]any{"60": group}}))
	upstream := &videoUpstreamFixture{responses: []*http.Response{
		videoFixtureResponse(200, "text/plain", body),
		videoFixtureResponse(206, "application/octet-stream", ""),
		videoFixtureResponse(200, "video/mp4", "\x00\x00\x00\x10ftyp0000"),
	}}
	transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: upstream})
	account := testSessionAccount(37)
	handle := videoFixtureHandle(t)
	videos, err := transport.GenerateVideo(context.Background(), account, handle, "current-pro", "test", nil)
	if err != nil || len(videos) != 1 {
		t.Fatalf("generation videos=%d err=%v", len(videos), err)
	}
	if upstream.requests[0].URL.Path != "/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate" {
		t.Fatal("wrong generation path")
	}
	if upstream.requests[0].Header.Get("x-goog-ext-525001261-jspb") == "" || upstream.requests[0].Header.Get("Cookie") == "" {
		t.Fatal("missing source-aligned generation headers")
	}
	if _, ready, err := transport.DownloadVideo(context.Background(), account, handle, videos[0].URL); err != nil || ready {
		t.Fatalf("206 ready=%v err=%v", ready, err)
	}
	content, ready, err := transport.DownloadVideo(context.Background(), account, handle, videos[0].URL)
	if err != nil || !ready || string(content[4:8]) != "ftyp" {
		t.Fatalf("200 ready=%v err=%v", ready, err)
	}
	if len(upstream.requests) != 3 {
		t.Fatalf("calls=%d", len(upstream.requests))
	}
}

func TestConsumerWebVideoDownloadRejectsUntrustedURL(t *testing.T) {
	upstream := &videoUpstreamFixture{}
	transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: upstream})
	_, _, err := transport.DownloadVideo(context.Background(), testSessionAccount(38), videoFixtureHandle(t), "https://evil.example/video.mp4")
	if err == nil || len(upstream.requests) != 0 {
		t.Fatal("untrusted URL dispatched")
	}
}
