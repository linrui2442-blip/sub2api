package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func recoveryFixture(t *testing.T, turns ...any) []byte {
	t.Helper()
	inner, err := json.Marshal([]any{turns})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal([]any{[]any{"wrb.fr", "hNvQHb", string(inner)}})
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(")]}'\n123\n"), outer...)
}

func recoveryTurn(rid, rcid string, candidateStatus any, media any) []any {
	turn := make([]any, 4)
	turn[0] = []any{"cid", rid}
	if rcid != "" {
		candidate := make([]any, 13)
		candidate[0], candidate[8], candidate[12] = rcid, []any{candidateStatus}, media
		turn[3] = []any{[]any{candidate}}
	}
	return turn
}

func TestConsumerWebVideoRecoveryTargetMatching(t *testing.T) {
	id := consumerWebVideoIdentity{CID: "cid", RID: "target", RCID: "choice"}
	for _, tc := range []struct {
		name  string
		turns []any
		found bool
		state consumerWebVideoRecoveryState
	}{
		{"absent", nil, false, consumerWebVideoRecoveryPending},
		{"old turn", []any{recoveryTurn("old", "choice", 2, nil)}, false, consumerWebVideoRecoveryPending},
		{"old choice", []any{recoveryTurn("target", "old-choice", 2, nil)}, false, consumerWebVideoRecoveryPending},
		{"target incomplete", []any{recoveryTurn("target", "choice", 1, nil)}, true, consumerWebVideoRecoveryPending},
		{"target terminal text", []any{recoveryTurn("target", "choice", 2, nil)}, true, consumerWebVideoRecoveryNoVideo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConsumerWebVideoConversation(recoveryFixture(t, tc.turns...), id)
			if err != nil || got.TargetFound != tc.found || got.State != tc.state {
				t.Fatalf("state=%s target=%t err=%v", got.State, got.TargetFound, err)
			}
		})
	}
	meta := make([]any, 12)
	meta[7] = []any{"https://contribution.usercontent.google.com/recovered"}
	media := []any{map[string]any{"60": []any{[]any{[]any{[]any{meta}}}}}}
	got, err := parseConsumerWebVideoConversation(recoveryFixture(t, recoveryTurn("old", "choice", 2, nil), recoveryTurn("target", "choice", 2, media)), id)
	if err != nil || got.State != consumerWebVideoRecoveryVideo || !got.TargetFound || len(got.Videos) != 1 {
		t.Fatalf("recovered state=%s videos=%d err=%v", got.State, len(got.Videos), err)
	}
}

func TestConsumerWebVideoConversationReadRequest(t *testing.T) {
	upstream := &videoUpstreamFixture{responses: []*http.Response{videoFixtureResponse(200, "application/json", string(recoveryFixture(t)))}}
	transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: upstream})
	handle := videoFixtureHandle(t)
	handle.endpoints = transport.endpoints
	account := testSessionAccount(37)
	got, err := transport.ReadVideoConversation(context.Background(), account, handle, consumerWebVideoIdentity{CID: "cid", RID: "target"})
	if err != nil || got.State != consumerWebVideoRecoveryPending || len(upstream.requests) != 1 {
		t.Fatalf("read state=%s calls=%d err=%v", got.State, len(upstream.requests), err)
	}
	req := upstream.requests[0]
	if req.Method != http.MethodPost || req.URL.Path != "/_/BardChatUi/data/batchexecute" || req.URL.Host != "gemini.google.com" {
		t.Fatal("wrong read-conversation endpoint")
	}
	body := make([]byte, req.ContentLength)
	_, _ = req.Body.Read(body)
	form, err := url.ParseQuery(string(body))
	if err != nil || form.Get("rpcids") != "hNvQHb" || form.Get("source-path") != "/app" || !strings.Contains(form.Get("f.req"), "hNvQHb") {
		t.Fatal("wrong read-conversation RPC")
	}
}

func TestConsumerWebVideoStreamAcceptedPending(t *testing.T) {
	fixture := videoDiagnosticFixture(t, nil, true)
	videos, diag, identity, err := parseConsumerWebGeneratedVideoAttempt(fixture)
	if err != nil || len(videos) != 0 || diag.Cause != ConsumerWebVideoAcceptedPending || identity.CID == "" || identity.RID == "" {
		t.Fatalf("cause=%s err=%v", diag.Cause, err)
	}
}
