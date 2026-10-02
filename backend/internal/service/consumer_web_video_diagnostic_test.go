package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type videoTransportFailureFixture struct{ videoUpstreamFixture }

func (*videoTransportFailureFixture) DoWithRedirectPolicy(*http.Request, string, int64, int, func(*http.Request, []*http.Request) error) (*http.Response, error) {
	return nil, errors.New("synthetic transport failure")
}

func videoDiagnosticFixture(t *testing.T, candidate []any, identity bool) []byte {
	t.Helper()
	payload := make([]any, 5)
	if identity {
		payload[1] = []any{"synthetic-conversation", "synthetic-response"}
	}
	payload[4] = []any{candidate}
	inner, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal([]any{[]any{"wrb.fr", "", string(inner)}})
	if err != nil {
		t.Fatal(err)
	}
	return outer
}

func TestConsumerWebVideoDiagnosticParserStages(t *testing.T) {
	meta := make([]any, 12)
	meta[7] = []any{"https://lh3.googleusercontent.com/gg-dl/a", "https://contribution.usercontent.google.com/a"}
	valid := []any{[]any{[]any{[]any{meta}}}}
	cases := []struct {
		name            string
		media           []any
		want            ConsumerWebVideoFailureCause
		slot, extracted bool
	}{
		{"valid", []any{[]any{"other"}, map[string]any{"60": valid}}, ConsumerWebVideoFailureNone, true, true},
		{"moved slot", []any{map[string]any{"60": valid}, []any{"other"}}, ConsumerWebVideoFailureNone, true, true},
		{"no slot", []any{map[string]any{"other": true}}, ConsumerWebVideoFailureNoVideo, false, false},
		{"malformed slot", []any{map[string]any{"60": []any{}}}, ConsumerWebVideoFailureSlotParse, true, false},
		{"missing URL", []any{map[string]any{"60": []any{[]any{[]any{[]any{make([]any, 8)}}}}}}, ConsumerWebVideoFailureURLMissing, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := make([]any, 13)
			candidate[12] = tc.media
			videos, d, err := parseConsumerWebGeneratedVideosDetailed(videoDiagnosticFixture(t, candidate, true))
			if d.Cause != tc.want || d.VideoKey60Present != tc.slot || (len(videos) > 0) != tc.extracted || d.ConversationIDPresent != true || d.ResponseIDPresent != true {
				t.Fatalf("cause=%s slot=%t extracted=%d err=%v", d.Cause, d.VideoKey60Present, len(videos), err)
			}
			if tc.extracted && (d.VideoURLVariantCount != 2 || videos[0].URL != "https://contribution.usercontent.google.com/a") {
				t.Fatal("preferred trusted URL not selected")
			}
		})
	}
}

func TestConsumerWebVideoDiagnosticTextOnlyAndProviderBlock(t *testing.T) {
	candidate := make([]any, 13)
	candidate[1] = []any{"Synthetic provider text; do not log this value."}
	_, d, err := parseConsumerWebGeneratedVideosDetailed(videoDiagnosticFixture(t, candidate, true))
	if err == nil || d.Cause != ConsumerWebVideoFailureNoVideo || !d.TextPresent || d.TextLengthBucket == "NONE" || !d.ParsePass || d.VideoKey60Present {
		t.Fatalf("text-only diagnostic mismatch: %+v", d)
	}
	_, d, err = parseConsumerWebGeneratedVideosDetailed([]byte("BardErrorInfo [synthetic]"))
	if err == nil || d.Cause != ConsumerWebVideoFailureProviderBlock || !d.ProviderErrorBlock {
		t.Fatal("provider block not classified")
	}
}

func TestConsumerWebVideoDiagnosticHTTPAndRequest(t *testing.T) {
	for _, status := range []int{200, 403} {
		body := "BardErrorInfo [synthetic]"
		upstream := &videoUpstreamFixture{responses: []*http.Response{videoFixtureResponse(status, "text/plain", body)}}
		transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: upstream})
		_, d, _ := transport.GenerateVideoDetailed(context.Background(), testSessionAccount(61), videoFixtureHandle(t), "synthetic-model", "synthetic prompt", nil)
		if !d.RequestBuilt || !d.Sent || !d.ResponseReceived || d.HTTPStatus != status || len(upstream.requests) != 1 {
			t.Fatalf("request diagnostic mismatch: %+v", d)
		}
		if upstream.requests[0].URL.Path != "/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate" {
			t.Fatal("not Consumer Web StreamGenerate")
		}
		if status == 403 && d.Cause != ConsumerWebVideoFailureHTTP {
			t.Fatal("HTTP failure collapsed")
		}
		if status == 200 && d.Cause != ConsumerWebVideoFailureProviderBlock {
			t.Fatal("provider block collapsed")
		}
	}
}

func TestConsumerWebVideoDiagnosticTransportFailure(t *testing.T) {
	transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: &videoTransportFailureFixture{}})
	_, d, err := transport.GenerateVideoDetailed(context.Background(), testSessionAccount(61), videoFixtureHandle(t), "synthetic-model", "synthetic prompt", nil)
	if err == nil || d.Cause != ConsumerWebVideoFailureTransport || !d.RequestBuilt || !d.Sent || d.ResponseReceived || d.HTTPStatus != 0 {
		t.Fatalf("transport classification mismatch: %+v", d)
	}
}

type videoIdentityFailureTransport struct {
	videoTaskFixtureTransport
	selected atomic.Int32
}

func (f *videoIdentityFailureTransport) GenerateVideoDetailed(context.Context, *Account, ConsumerWebSessionHandle, string, string, *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, error) {
	return nil, ConsumerWebVideoAttemptDiagnostic{Cause: ConsumerWebVideoFailureNoVideo, RequestBuilt: true, Sent: true, ResponseReceived: true, HTTPStatus: 200, ParsePass: true, ConversationIDPresent: true, ResponseIDPresent: true, CandidateCount: 1}, errConsumerWebVideoResponse
}
func TestConsumerWebVideoProviderIdentityBindsOnNoVideo(t *testing.T) {
	f := &videoIdentityFailureTransport{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: f, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	defer m.Close()
	tasks := NewConsumerWebVideoTaskService(m, f)
	created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: testSessionAccount(61)}, func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error) {
		f.selected.Add(1)
		return nil, errors.New("reschedule forbidden")
	}, "gemini-3.1-pro-high", "synthetic", nil)
	if err != nil {
		t.Fatal(err)
	}
	status := waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.Status == ConsumerWebVideoFailed })
	if status.AccountID != 61 || status.AttemptDiagnostic.Cause != ConsumerWebVideoFailureNoVideo || f.selected.Load() != 0 {
		t.Fatal("provider identity did not bind or task rescheduled")
	}
}
