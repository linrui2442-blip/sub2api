package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func consumerWebVideoFixture(t *testing.T, media []any) []byte {
	t.Helper()
	candidate := make([]any, 13)
	candidate[12] = media
	payload := make([]any, 5)
	payload[4] = []any{candidate}
	inner, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal([]any{[]any{"wrb.fr", "", string(inner)}})
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(")]}'\n123\n"), outer...)
}

func TestConsumerWebGeneratedVideoParser(t *testing.T) {
	first := make([]any, 12)
	first[2] = "first.mp4"
	first[7] = []any{"https://lh3.googleusercontent.com/gg-dl/first", "https://contribution.usercontent.google.com/first"}
	first[11] = "video/mp4"
	second := make([]any, 12)
	second[2] = "second.mp4"
	second[7] = []any{"https://lh3.googleusercontent.com/gg-dl/second"}
	second[11] = "video/webm"
	group := []any{[]any{[]any{[]any{first, second, first}}}}
	body := consumerWebVideoFixture(t, []any{[]any{"image"}, map[string]any{"60": group}})
	videos, err := parseConsumerWebGeneratedVideos(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 {
		t.Fatalf("videos=%d, want 2", len(videos))
	}
	if videos[0].URL != "https://contribution.usercontent.google.com/first" || videos[0].Title != "first.mp4" || videos[0].MIME != "video/mp4" {
		t.Fatalf("first video metadata mismatch")
	}
	if videos[1].URL != "https://lh3.googleusercontent.com/gg-dl/second" || videos[1].MIME != "video/webm" {
		t.Fatalf("second video metadata mismatch")
	}
}

func TestConsumerWebGeneratedVideoParserRejectsInvalidMedia(t *testing.T) {
	badMeta := make([]any, 8)
	badMeta[7] = []any{"http://evil.example/x"}
	badSlot := []any{[]any{[]any{[]any{badMeta}}}}
	for _, media := range [][]any{
		nil,
		{map[string]any{"60": []any{}}},
		{map[string]any{"60": badSlot}},
	} {
		if _, err := parseConsumerWebGeneratedVideos(consumerWebVideoFixture(t, media)); err == nil {
			t.Fatal("invalid media accepted")
		}
	}
}

type videoTaskFixtureTransport struct {
	sessionFakeTransport
	generatedAccount atomic.Int64
	downloadAccount  atomic.Int64
	downloads        atomic.Int32
}

func (f *videoTaskFixtureTransport) Bootstrap(_ context.Context, _ *Account, _ map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	return ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session, Registry: []ConsumerWebModel{{ID: "flash", Display: "Flash"}, {ID: "current-pro-id", Display: "3.1 Pro", Tier: "2"}}}, nil
}
func (f *videoTaskFixtureTransport) GenerateVideo(_ context.Context, account *Account, _ ConsumerWebSessionHandle, model, _ string, _ *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, error) {
	f.generatedAccount.Store(account.ID)
	if model != "current-pro-id" {
		return nil, ErrConsumerWebProtocolDrift
	}
	return []consumerWebGeneratedVideo{{URL: "https://contribution.usercontent.google.com/video", MIME: "video/mp4"}}, nil
}
func (f *videoTaskFixtureTransport) DownloadVideo(_ context.Context, account *Account, _ ConsumerWebSessionHandle, _ string) ([]byte, bool, error) {
	f.downloadAccount.Store(account.ID)
	if f.downloads.Add(1) == 1 {
		return nil, false, nil
	}
	return []byte{0, 0, 0, 16, 'f', 't', 'y', 'p'}, true, nil
}

func TestConsumerWebVideoTaskBindsAccountThroughStatusAndContent(t *testing.T) {
	transport := &videoTaskFixtureTransport{}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: transport, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, transport)
	account := testSessionAccount(62)
	created, err := tasks.Start(3, account, "gemini-3.1-pro-high", "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != ConsumerWebVideoQueued {
		t.Fatalf("created status=%s", created.Status)
	}
	if _, err := tasks.Status(context.Background(), created.ID, 4); err == nil {
		t.Fatal("cross-key status allowed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for transport.generatedAccount.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if transport.generatedAccount.Load() != 62 {
		t.Fatalf("generation account=%d", transport.generatedAccount.Load())
	}
	first, err := tasks.Status(context.Background(), created.ID, 3)
	if err != nil || first.Status != ConsumerWebVideoRunning {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := tasks.Status(context.Background(), created.ID, 3)
	if err != nil || second.Status != ConsumerWebVideoSucceeded {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	content, mime, err := tasks.Content(created.ID, 3)
	if err != nil || mime != "video/mp4" || string(content[4:8]) != "ftyp" {
		t.Fatalf("content invalid: mime=%s err=%v", mime, err)
	}
	if transport.downloadAccount.Load() != 62 {
		t.Fatalf("download account=%d", transport.downloadAccount.Load())
	}
	if _, _, err := tasks.Content(created.ID, 4); err == nil {
		t.Fatal("cross-key content allowed")
	}
}
