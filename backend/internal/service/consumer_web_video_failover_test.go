package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type videoFailoverTransport struct {
	videoTaskFixtureTransport
	acquireFailureID int64
	uploadFailureID  int64
	modelSeen        string
	imageSeen        []byte
	downloadFailure  bool
}

func (f *videoFailoverTransport) Bootstrap(_ context.Context, account *Account, _ map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	if account.ID == f.acquireFailureID {
		return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireCredentialNamespaceMissing, Err: ErrConsumerWebAuthLost}
	}
	return ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session, Registry: []ConsumerWebModel{{ID: "registry-model-for-account-b", Display: "3.1 Pro"}}}, nil
}
func (f *videoFailoverTransport) GenerateVideo(_ context.Context, account *Account, _ ConsumerWebSessionHandle, model, _ string, image *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, error) {
	if account.ID == f.uploadFailureID {
		return nil, &ConsumerWebVideoUploadError{Err: ErrConsumerWebForbidden}
	}
	f.generatedAccount.Store(account.ID)
	f.modelSeen = model
	if image != nil {
		f.imageSeen = append([]byte(nil), image.Data...)
	}
	return []consumerWebGeneratedVideo{{URL: "https://contribution.usercontent.google.com/video", MIME: "video/mp4"}}, nil
}
func (f *videoFailoverTransport) DownloadVideo(_ context.Context, account *Account, _ ConsumerWebSessionHandle, _ string) ([]byte, bool, error) {
	f.downloadAccount.Store(account.ID)
	if f.downloadFailure {
		return nil, false, ErrConsumerWebTransportTransient
	}
	return []byte{0, 0, 0, 16, 'f', 't', 'y', 'p'}, true, nil
}

func waitVideoTask(t *testing.T, tasks *ConsumerWebVideoTaskService, id string, done func(ConsumerWebVideoTaskSnapshot) bool) ConsumerWebVideoTaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := tasks.Status(context.Background(), id, 3)
		if err != nil {
			t.Fatal(err)
		}
		if done(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("video task did not reach expected state")
	return ConsumerWebVideoTaskSnapshot{}
}

func TestConsumerWebVideoPreAcceptFailoverAndRemap(t *testing.T) {
	for _, tc := range []struct {
		name            string
		acquire, upload bool
	}{
		{"acquire_failure", true, false},
		{"image_upload_failure", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &videoFailoverTransport{}
			if tc.acquire {
				transport.acquireFailureID = 71
			}
			if tc.upload {
				transport.uploadFailureID = 71
			}
			manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: transport, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
			defer manager.Close()
			tasks := NewConsumerWebVideoTaskService(manager, transport)
			first := testSessionAccount(71)
			second := testSessionAccount(72)
			first.Credentials = map[string]any{"model_mapping": map[string]any{"gemini-3.1-pro-high": "a-mapped"}}
			second.Credentials = map[string]any{"model_mapping": map[string]any{"gemini-3.1-pro-high": "b-mapped"}}
			var released atomic.Int32
			var selected atomic.Int32
			selector := func(_ context.Context, excluded map[int64]struct{}) (*AccountSelectionResult, error) {
				selected.Add(1)
				if _, ok := excluded[71]; !ok {
					t.Fatal("failed account not excluded")
				}
				return &AccountSelectionResult{Account: second, ReleaseFunc: func() { released.Add(1) }}, nil
			}
			image := &ConsumerWebVideoUpload{MIME: "image/jpeg", Data: []byte("synthetic image")}
			created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: first, ReleaseFunc: func() { released.Add(1) }}, selector, "gemini-3.1-pro-high", "synthetic", image)
			if err != nil {
				t.Fatal(err)
			}
			if created.AccountID != 0 {
				t.Fatal("account bound before provider acceptance")
			}
			status := waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool {
				return s.AccountID == 72 || s.Status == ConsumerWebVideoFailed
			})
			if status.AccountID != 72 || status.Status == ConsumerWebVideoFailed || selected.Load() != 1 || released.Load() != 2 {
				t.Fatalf("failover/binding mismatch: account=%d status=%s selections=%d releases=%d", status.AccountID, status.Status, selected.Load(), released.Load())
			}
			if transport.generatedAccount.Load() != 72 || transport.modelSeen != "registry-model-for-account-b" || second.GetMappedModel("gemini-3.1-pro-high") != "b-mapped" {
				t.Fatal("account B model/session not used")
			}
			if string(transport.imageSeen) != string(image.Data) {
				t.Fatal("image was not prepared for account B")
			}
		})
	}
}

func TestConsumerWebVideoSingleAccountExhaustion(t *testing.T) {
	transport := &videoFailoverTransport{acquireFailureID: 81}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: transport, Credentials: &sessionFakeCredentials{values: map[string]any{}}})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, transport)
	var selects atomic.Int32
	created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: testSessionAccount(81)}, func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error) {
		selects.Add(1)
		return nil, ErrNoAvailableAccounts
	}, "gemini-3.1-pro-high", "synthetic", nil)
	if err != nil {
		t.Fatal(err)
	}
	status := waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.Status == ConsumerWebVideoFailed })
	if status.AccountID != 0 || status.Error != "AUTH_REQUIRED" || status.AcquireSessionCause != ConsumerWebAcquireCredentialNamespaceMissing || selects.Load() != 1 || transport.generatedAccount.Load() != 0 {
		t.Fatal("single-account exhaustion violated")
	}
}

func TestConsumerWebVideoPostAcceptNeverReschedules(t *testing.T) {
	transport := &videoFailoverTransport{downloadFailure: true}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: transport, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, transport)
	var selects atomic.Int32
	created, err := tasks.StartWithSelector(3, &AccountSelectionResult{Account: testSessionAccount(92)}, func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error) {
		selects.Add(1)
		return nil, errors.New("must not select")
	}, "gemini-3.1-pro-high", "synthetic", nil)
	if err != nil {
		t.Fatal(err)
	}
	status := waitVideoTask(t, tasks, created.ID, func(s ConsumerWebVideoTaskSnapshot) bool { return s.Status == ConsumerWebVideoFailed })
	if status.AccountID != 92 || status.Error != "MEDIA_UNAVAILABLE" || selects.Load() != 0 || transport.downloadAccount.Load() != 92 {
		t.Fatal("post-accept account binding or no-reschedule violated")
	}
}
