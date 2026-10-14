package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type traceTestCipher struct{ cipher.AEAD }

func (e traceTestCipher) Encrypt(s string) (string, error) {
	n := make([]byte, e.NonceSize())
	_, err := rand.Read(n)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(e.Seal(n, n, []byte(s), nil)), nil
}
func (e traceTestCipher) Decrypt(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	p, err := e.Open(nil, b[:e.NonceSize()], b[e.NonceSize():], nil)
	return string(p), err
}

func TestConsumerWebVideoDurableTraceRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tasks.db")
	block, _ := aes.NewCipher(make([]byte, 32))
	aead, _ := cipher.NewGCM(block)
	enc := traceTestCipher{aead}
	open := func() *sql.DB {
		d, e := sql.Open("sqlite", path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = d.Exec(VideoTraceSchema); e != nil {
			t.Fatal(e)
		}
		return d
	}
	db := open()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	fixture := &lifecycleVideoTransport{}
	fixture.read = func(n int) (consumerWebVideoRecoveryResult, error) {
		r := consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryNoVideo, HTTPClass: "HTTP_200", Parsed: true, TextPresent: true, MediaKey65Present: true, CandidateCount: 1}
		if n == 3 {
			r.State = consumerWebVideoRecoveryVideo
			r.Videos = []consumerWebGeneratedVideo{{URL: "https://contribution.usercontent.google.com/synthetic", MIME: "video/mp4"}}
		}
		return r, nil
	}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: fixture, Credentials: &sessionFakeCredentials{values: map[string]any{}}})
	defer manager.Close()
	svc := NewConsumerWebVideoTaskService(manager, fixture)
	svc.SetTraceStore(NewConsumerWebVideoTraceStore(db, enc))
	id := consumerWebVideoIdentity{"private-cid-canary", "private-rid-canary", "private-rcid-canary"}
	task := &consumerWebVideoTask{ownerID: 9, identity: id, snapshot: ConsumerWebVideoTaskSnapshot{ID: "video_synthetic", AccountID: 94, Status: ConsumerWebVideoRunning}, trace: ConsumerWebVideoTrace{Initial: ConsumerWebVideoInitialTrace{CIDPresent: true, RIDPresent: true, RCIDPresent: true, MediaKey65Present: true, TaskMarkerClass: "UNKNOWN", ProgressMarkerClass: "UNKNOWN"}}}
	svc.persistTrace(task)
	svc.recoverPendingVideoWithInterval(ctx, task, testSessionAccount(94), id, time.Millisecond)
	db.Close()
	db = open()
	defer db.Close()
	recreated := NewConsumerWebVideoTaskService(manager, fixture)
	recreated.SetTraceStore(NewConsumerWebVideoTraceStore(db, enc))
	got, err := recreated.traceStore.Load(ctx, "video_synthetic", 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity != id || !got.Initial.MediaKey65Present || len(got.Polls) != 3 || !got.Polls[2].VideoPresent || got.Polls[0].VideoPresent {
		t.Fatal("durable trace mismatch")
	}
	if got.SourcePath != "/app" || got.Polls[0].CompletionClass != "TERMINAL_NO_VIDEO" {
		t.Fatal("request/trace mismatch")
	}
	if _, err = recreated.traceStore.Load(ctx, "video_synthetic", 10); err == nil {
		t.Fatal("owner boundary")
	}
	var stored string
	if err = db.QueryRow(`SELECT encrypted_record FROM consumer_web_video_traces`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{id.CID, id.RID, id.RCID, "synthetic", "https://"} {
		if strings.Contains(stored, secret) || strings.Contains(logs.String(), secret) && secret != "synthetic" {
			t.Fatal("plaintext leaked")
		}
	}
}

func TestConsumerWebVideoTraceMarkerObservation(t *testing.T) {
	c := make([]any, 13)
	c[0] = "synthetic-choice"
	c[12] = []any{map[string]any{"65": []any{"opaque-not-persisted"}}}
	_, d, _, err := parseConsumerWebGeneratedVideoAttempt(videoDiagnosticFixture(t, c, true))
	_ = err
	if !d.VideoKey65Present || d.VideoKey60Present {
		t.Fatal("structural key observation")
	}
}

func TestConsumerWebVideoTraceInitialProductionHook(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "initial.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(VideoTraceSchema); err != nil {
		t.Fatal(err)
	}
	b, _ := aes.NewCipher(make([]byte, 32))
	g, _ := cipher.NewGCM(b)
	f := &pendingVideoFixtureTransport{readStarted: make(chan struct{}, 1), allowRead: make(chan struct{})}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: f, Credentials: &sessionFakeCredentials{values: map[string]any{}}})
	defer m.Close()
	s := NewConsumerWebVideoTaskService(m, f)
	s.SetTraceStore(NewConsumerWebVideoTraceStore(db, traceTestCipher{g}))
	created, err := s.Start(3, testSessionAccount(93), "gemini-3.1-pro-high", "RAW_PROMPT_MUST_NOT_PERSIST", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.readStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("initial persistence gate timeout")
	}
	r, err := s.RecoverProviderTrace(context.Background(), created.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Initial.CIDPresent || !r.Initial.RIDPresent || !r.Initial.TextPresent || r.Initial.FinalVideoPresent || r.Identity.CID != "cid" {
		t.Fatal("initial trace missing")
	}
	plain, _ := json.Marshal(r)
	if bytes.Contains(plain, []byte("RAW_PROMPT_MUST_NOT_PERSIST")) {
		t.Fatal("prompt persisted")
	}
	close(f.allowRead)
	waitVideoTask(t, s, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.RecoveryVideoFound })
}
