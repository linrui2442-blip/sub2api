package service

import (
	"context"
	"testing"
	"time"
)

type lifecycleVideoTransport struct {
	videoFailoverTransport
	read  func(int) (consumerWebVideoRecoveryResult, error)
	reads int
}

func (f *lifecycleVideoTransport) ReadVideoConversation(_ context.Context, _ *Account, _ ConsumerWebSessionHandle, _ consumerWebVideoIdentity) (consumerWebVideoRecoveryResult, error) {
	f.reads++
	return f.read(f.reads)
}

func TestConsumerWebVideoPollingLifecycle(t *testing.T) {
	for _, name := range []string{"fourth_poll_video", "no_video_timeout", "terminal_error", "text_ack_then_video", "terminal_refusal"} {
		t.Run(name, func(t *testing.T) {
			id := consumerWebVideoIdentity{CID: "cid", RID: "rid", RCID: "choice"}
			pending := consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryPending, TargetFound: true, CandidateCount: 1}
			fixture := &lifecycleVideoTransport{}
			fixture.read = func(n int) (consumerWebVideoRecoveryResult, error) {
				if name == "terminal_error" {
					return consumerWebVideoRecoveryResult{}, ErrConsumerWebProtocolDrift
				}
				if name == "terminal_refusal" {
					return consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryRefusal}, nil
				}
				limit := 4
				if name == "text_ack_then_video" {
					limit = 2
				}
				if name != "no_video_timeout" && n == limit {
					return consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryVideo, Videos: []consumerWebGeneratedVideo{{URL: "https://contribution.usercontent.google.com/synthetic", MIME: "video/mp4"}}}, nil
				}
				return pending, nil
			}
			manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: fixture, Credentials: &sessionFakeCredentials{values: map[string]any{}}})
			defer manager.Close()
			tasks := NewConsumerWebVideoTaskService(manager, fixture)
			task := &consumerWebVideoTask{snapshot: ConsumerWebVideoTaskSnapshot{Status: ConsumerWebVideoRunning}}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			tasks.recoverPendingVideoWithInterval(ctx, task, testSessionAccount(94), id, time.Millisecond)
			switch name {
			case "fourth_poll_video", "text_ack_then_video":
				want := 4
				if name == "text_ack_then_video" {
					want = 2
				}
				if fixture.reads != want || !task.snapshot.RecoveryVideoFound || task.snapshot.Status == ConsumerWebVideoFailed {
					t.Fatal("eventual video not recovered")
				}
			case "no_video_timeout":
				if fixture.reads < 2 || task.snapshot.AttemptDiagnostic.Cause != ConsumerWebVideoRecoveryTimeout {
					t.Fatal("missing bounded timeout")
				}
			default:
				if fixture.reads != 1 || task.snapshot.Status != ConsumerWebVideoFailed {
					t.Fatal("terminal result did not stop immediately")
				}
			}
		})
	}
}
