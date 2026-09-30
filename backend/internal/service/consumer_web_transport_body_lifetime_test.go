package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

func TestConsumerWebResponseBodyOwnsContextUntilEOFOrClose(t *testing.T) {
	for _, mode := range []string{"eof", "close"} {
		t.Run(mode, func(t *testing.T) {
			u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, "synthetic")}}
			tr := consumerWebTestTransport(u)
			jar, _ := cookiejar.New(nil)
			h := &consumerWebHTTPHandle{jar: jar}
			resp, err := tr.do(context.Background(), consumerWebTestAccount(), h, "GET", "http://app.test/app", nil, "")
			if err != nil {
				t.Fatal("request failed")
			}
			ctx := u.requests[0].Context()
			if ctx.Err() != nil {
				t.Fatal("context canceled before body consumption")
			}
			if mode == "eof" {
				if _, err := io.ReadAll(resp.Body); err != nil {
					t.Fatal("body read failed")
				}
			} else if err := resp.Body.Close(); err != nil {
				t.Fatal("body close failed")
			}
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("context not released")
			}
			_ = resp.Body.Close()
		})
	}
}

func TestConsumerWebResponseBodyCallerCancelAndDeadline(t *testing.T) {
	for _, mode := range []string{"caller", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, "synthetic")}}
			tr := consumerWebTestTransport(u)
			tr.timeout = 15 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			jar, _ := cookiejar.New(nil)
			resp, err := tr.do(ctx, consumerWebTestAccount(), &consumerWebHTTPHandle{jar: jar}, "GET", "http://app.test/app", strings.NewReader(""), "")
			if err != nil {
				t.Fatal("request failed")
			}
			defer resp.Body.Close()
			if mode == "caller" {
				cancel()
			} else {
				<-u.requests[0].Context().Done()
			}
			if u.requests[0].Context().Err() == nil {
				t.Fatal("request context not canceled")
			}
		})
	}
}

func TestConsumerWebResponseWithoutBodyCancelsImmediately(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{nil}}
	tr := consumerWebTestTransport(u)
	jar, _ := cookiejar.New(nil)
	_, err := tr.do(context.Background(), consumerWebTestAccount(), &consumerWebHTTPHandle{jar: jar}, "GET", "http://app.test/app", nil, "")
	if !errors.Is(err, ErrConsumerWebTransportTransient) {
		t.Fatal("missing body not rejected")
	}
	if !errors.Is(u.requests[0].Context().Err(), context.Canceled) {
		t.Fatal("request context not canceled")
	}
}
