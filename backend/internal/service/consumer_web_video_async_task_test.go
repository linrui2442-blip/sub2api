package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type pendingVideoFixtureTransport struct {
	videoFailoverTransport
	readStarted chan struct{}
	allowRead   chan struct{}
	reads       atomic.Int32
	imageSeen   atomic.Bool
}

func (f *pendingVideoFixtureTransport) GenerateVideoAttempt(_ context.Context, _ *Account, _ ConsumerWebSessionHandle, _, _ string, image *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, consumerWebVideoIdentity, error) {
	f.imageSeen.Store(image != nil)
	return nil, ConsumerWebVideoAttemptDiagnostic{Cause: ConsumerWebVideoAcceptedPending, ConversationIDPresent: true, ResponseIDPresent: true, CandidateCount: 5, TextPresent: true, ParsePass: true}, consumerWebVideoIdentity{CID: "cid", RID: "rid"}, nil
}

func (f *pendingVideoFixtureTransport) ReadVideoConversation(ctx context.Context, _ *Account, _ ConsumerWebSessionHandle, _ consumerWebVideoIdentity) (consumerWebVideoRecoveryResult, error) {
	f.reads.Add(1)
	select {
	case f.readStarted <- struct{}{}:
	default:
	}
	select {
	case <-f.allowRead:
		return consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryVideo, TargetFound: true, Videos: []consumerWebGeneratedVideo{{URL: "https://contribution.usercontent.google.com/video", MIME: "video/mp4"}}}, nil
	case <-ctx.Done():
		return consumerWebVideoRecoveryResult{}, ctx.Err()
	}
}

func TestConsumerWebVideoAcceptedPendingPreservesBindingAndContentGate(t *testing.T) {
	fixture := &pendingVideoFixtureTransport{readStarted: make(chan struct{}, 1), allowRead: make(chan struct{})}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: fixture, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, fixture)
	var reschedules atomic.Int32
	created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: testSessionAccount(93)}, func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error) {
		reschedules.Add(1)
		return nil, ErrNoAvailableAccounts
	}, "gemini-3.1-pro-high", "synthetic", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fixture.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery never started")
	}
	status, err := tasks.Status(context.Background(), created.ID, 3)
	if err != nil || status.Status != ConsumerWebVideoRunning || status.AccountID != 93 || !status.RecoveryStarted || reschedules.Load() != 0 {
		t.Fatalf("pending account/status mismatch: %+v err=%v", status, err)
	}
	if _, _, err := tasks.Content(created.ID, 3); err != ErrConsumerWebVideoNotReady {
		t.Fatal("content exposed before video URL")
	}
	close(fixture.allowRead)
	status = waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.RecoveryVideoFound })
	if status.AccountID != 93 || reschedules.Load() != 0 || fixture.reads.Load() != 1 {
		t.Fatal("post-accept account switched or prompt retried")
	}
}

func TestConsumerWebImageVideoAcceptedPendingUsesSameRecovery(t *testing.T) {
	fixture := &pendingVideoFixtureTransport{readStarted: make(chan struct{}, 1), allowRead: make(chan struct{})}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: fixture, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, fixture)
	var reschedules atomic.Int32
	image := &ConsumerWebVideoUpload{Name: "synthetic.jpg", MIME: "image/jpeg", Data: []byte("synthetic")}
	created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: testSessionAccount(93)}, func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error) {
		reschedules.Add(1)
		return nil, ErrNoAvailableAccounts
	}, "gemini-3.1-pro-high", "synthetic", image)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fixture.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("image recovery never started")
	}
	status, err := tasks.Status(context.Background(), created.ID, 3)
	if err != nil || status.Status != ConsumerWebVideoRunning || !status.RecoveryStarted || status.AccountID != 93 || !fixture.imageSeen.Load() || reschedules.Load() != 0 {
		t.Fatalf("image pending/recovery mismatch: %+v err=%v", status, err)
	}
	close(fixture.allowRead)
	status = waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.RecoveryVideoFound })
	if status.AccountID != 93 || reschedules.Load() != 0 || fixture.reads.Load() != 1 {
		t.Fatal("image recovery switched account or retried generation")
	}
}

func TestConsumerWebVideoRecoveryTimeoutCause(t *testing.T) {
	fixture := &pendingVideoFixtureTransport{readStarted: make(chan struct{}, 1), allowRead: make(chan struct{})}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: fixture, Credentials: &sessionFakeCredentials{values: map[string]any{}}})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, fixture)
	task := &consumerWebVideoTask{snapshot: ConsumerWebVideoTaskSnapshot{Status: ConsumerWebVideoRunning, AccountID: 94}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tasks.recoverPendingVideo(ctx, task, testSessionAccount(94), consumerWebVideoIdentity{CID: "cid", RID: "rid"})
	if task.snapshot.Status != ConsumerWebVideoFailed || task.snapshot.AttemptDiagnostic.Cause != ConsumerWebVideoRecoveryTimeout {
		t.Fatal("timeout cause not preserved")
	}
}
