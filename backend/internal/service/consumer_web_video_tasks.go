package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type ConsumerWebVideoTaskState string

const (
	ConsumerWebVideoQueued    ConsumerWebVideoTaskState = "queued"
	ConsumerWebVideoRunning   ConsumerWebVideoTaskState = "processing"
	ConsumerWebVideoSucceeded ConsumerWebVideoTaskState = "completed"
	ConsumerWebVideoFailed    ConsumerWebVideoTaskState = "failed"
)

type ConsumerWebVideoTaskSnapshot struct {
	ID                  string                            `json:"id"`
	Status              ConsumerWebVideoTaskState         `json:"status"`
	Model               string                            `json:"model"`
	AccountID           int64                             `json:"-"`
	MIME                string                            `json:"mime_type,omitempty"`
	Error               string                            `json:"error,omitempty"`
	AcquireSessionCause ConsumerWebAcquireCause           `json:"-"`
	AcquireSessionError error                             `json:"-"`
	AttemptDiagnostic   ConsumerWebVideoAttemptDiagnostic `json:"-"`
	RecoveryStarted     bool                              `json:"-"`
	RecoveryReadCount   int                               `json:"-"`
	RecoveryTargetFound []bool                            `json:"-"`
	RecoveryVideoFound  bool                              `json:"-"`
}

type consumerWebVideoTask struct {
	trace      ConsumerWebVideoTrace
	mu         sync.Mutex
	snapshot   ConsumerWebVideoTaskSnapshot
	ownerID    int64
	account    *Account
	identity   consumerWebVideoIdentity
	first      *AccountSelectionResult
	selectNext ConsumerWebVideoAccountSelector
	video      consumerWebGeneratedVideo
	content    []byte
	created    time.Time
}

// ConsumerWebVideoAccountSelector reuses the gateway scheduler with a
// request-scoped exclusion set. It must not select the same failed account.
type ConsumerWebVideoAccountSelector func(context.Context, map[int64]struct{}) (*AccountSelectionResult, error)

var (
	ErrConsumerWebVideoTaskNotFound = errors.New("video task not found")
	ErrConsumerWebVideoNotReady     = errors.New("video content not ready")
)

type ConsumerWebVideoDriver interface {
	GenerateVideo(context.Context, *Account, ConsumerWebSessionHandle, string, string, *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, error)
	DownloadVideo(context.Context, *Account, ConsumerWebSessionHandle, string) ([]byte, bool, error)
}

type ConsumerWebVideoTaskService struct {
	traceStore *ConsumerWebVideoTraceStore
	mu         sync.Mutex
	tasks      map[string]*consumerWebVideoTask
	manager    *ConsumerWebSessionManager
	driver     ConsumerWebVideoDriver
}

func NewConsumerWebVideoTaskService(manager *ConsumerWebSessionManager, transport ConsumerWebSessionTransport) *ConsumerWebVideoTaskService {
	driver, _ := transport.(ConsumerWebVideoDriver)
	return &ConsumerWebVideoTaskService{manager: manager, driver: driver, tasks: make(map[string]*consumerWebVideoTask)}
}

func resolveConsumerWebVideoModel(registry []ConsumerWebModel) (string, error) {
	selected := ""
	for _, model := range registry {
		name := strings.ToLower(strings.TrimSpace(model.Display))
		if !strings.Contains(name, "pro") || strings.Contains(name, "flash") || strings.Contains(name, "lite") || strings.Contains(name, "experimental") {
			continue
		}
		if strings.TrimSpace(model.ID) == "" || selected != "" {
			return "", ErrConsumerWebProtocolDrift
		}
		selected = model.ID
	}
	if selected == "" {
		return "", ErrConsumerWebProtocolDrift
	}
	return selected, nil
}

func (s *ConsumerWebVideoTaskService) Start(ownerID int64, account *Account, publicModel, prompt string, image *ConsumerWebVideoUpload, release func()) (ConsumerWebVideoTaskSnapshot, error) {
	return s.StartWithSelector(ownerID, &AccountSelectionResult{Account: account, ReleaseFunc: release}, nil, publicModel, prompt, image)
}

// StartWithSelector keeps the first scheduler result provisional. A stable
// provider identity or a usable video result creates the immutable binding.
func (s *ConsumerWebVideoTaskService) StartWithSelector(ownerID int64, first *AccountSelectionResult, selectNext ConsumerWebVideoAccountSelector, publicModel, prompt string, image *ConsumerWebVideoUpload) (ConsumerWebVideoTaskSnapshot, error) {
	if s == nil || s.manager == nil || s.driver == nil || first == nil || first.Account == nil || ownerID <= 0 || strings.TrimSpace(prompt) == "" {
		if first != nil && first.ReleaseFunc != nil {
			first.ReleaseFunc()
		}
		return ConsumerWebVideoTaskSnapshot{}, ErrConsumerWebProtocolDrift
	}
	s.mu.Lock()
	for id, task := range s.tasks {
		if time.Since(task.created) > time.Hour {
			delete(s.tasks, id)
		}
	}
	if len(s.tasks) >= 4 {
		s.mu.Unlock()
		if first.ReleaseFunc != nil {
			first.ReleaseFunc()
		}
		return ConsumerWebVideoTaskSnapshot{}, ErrConsumerWebRateLimited
	}
	id := "video_" + consumerWebVideoRequestID()
	task := &consumerWebVideoTask{ownerID: ownerID, first: first, selectNext: selectNext, created: time.Now(), snapshot: ConsumerWebVideoTaskSnapshot{ID: id, Status: ConsumerWebVideoQueued, Model: publicModel}}
	s.tasks[id] = task
	s.mu.Unlock()
	go s.run(task, prompt, image)
	return task.snapshot, nil
}

func (s *ConsumerWebVideoTaskService) run(task *consumerWebVideoTask, prompt string, image *ConsumerWebVideoUpload) {
	task.mu.Lock()
	task.snapshot.Status = ConsumerWebVideoRunning
	task.mu.Unlock()
	// Allow the bounded generation phase (up to nine minutes) and the separate
	// eight-minute accepted-turn recovery window to both complete.
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	selection := task.first
	excluded := make(map[int64]struct{})
	for selection != nil && selection.Account != nil {
		account := selection.Account
		attempt := len(excluded) + 1
		slog.Info("consumer_web_video_candidate", "task_id", task.snapshot.ID, "account_id", account.ID, "attempt", attempt)
		if account.Platform != PlatformAntigravity || account.GetMappedModel(task.snapshot.Model) == "" {
			releaseVideoSelection(selection)
			if !s.nextVideoCandidate(ctx, task, account.ID, excluded, &selection, "MODEL_NOT_AVAILABLE") {
				return
			}
			continue
		}
		session, err := s.manager.AcquireSession(ctx, account)
		if err != nil {
			cause := consumerWebAcquireCause(err)
			task.mu.Lock()
			task.snapshot.AcquireSessionCause = cause
			task.snapshot.AcquireSessionError = wrapConsumerWebAcquireError(err)
			task.mu.Unlock()
			slog.Info("consumer_web_video_acquire_failed", "task_id", task.snapshot.ID, "account_id", account.ID, "attempt", attempt, "cause", cause)
			releaseVideoSelection(selection)
			if !videoAcquireCauseAccountScoped(cause) || !s.nextVideoCandidate(ctx, task, account.ID, excluded, &selection, "AUTH_REQUIRED") {
				if !videoAcquireCauseAccountScoped(cause) {
					s.fail(task, "AUTH_REQUIRED")
				}
				return
			}
			continue
		}
		registry := s.manager.Registry(account.ID)
		model, err := resolveConsumerWebVideoModel(registry)
		if err != nil {
			releaseVideoSelection(selection)
			if !s.nextVideoCandidate(ctx, task, account.ID, excluded, &selection, "MODEL_NOT_AVAILABLE") {
				return
			}
			continue
		}
		runtimeDisplay := ""
		for _, entry := range registry {
			if entry.ID == model {
				runtimeDisplay = entry.Display
				break
			}
		}
		var videos []consumerWebGeneratedVideo
		var diagnostic ConsumerWebVideoAttemptDiagnostic
		var identity consumerWebVideoIdentity
		if detailed, ok := s.driver.(interface {
			GenerateVideoAttempt(context.Context, *Account, ConsumerWebSessionHandle, string, string, *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, consumerWebVideoIdentity, error)
		}); ok {
			videos, diagnostic, identity, err = detailed.GenerateVideoAttempt(ctx, account, session, model, prompt, image)
		} else if detailed, ok := s.driver.(interface {
			GenerateVideoDetailed(context.Context, *Account, ConsumerWebSessionHandle, string, string, *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, error)
		}); ok {
			videos, diagnostic, err = detailed.GenerateVideoDetailed(ctx, account, session, model, prompt, image)
		} else {
			videos, err = s.driver.GenerateVideo(ctx, account, session, model, prompt, image)
			diagnostic.Cause = ConsumerWebVideoFailureUnknown
		}
		task.mu.Lock()
		task.snapshot.AttemptDiagnostic = diagnostic
		task.trace.Initial = ConsumerWebVideoInitialTrace{CIDPresent: identity.CID != "", RIDPresent: identity.RID != "", RCIDPresent: identity.RCID != "", TextPresent: diagnostic.TextPresent, FinalVideoPresent: len(videos) > 0, MediaKey60Present: diagnostic.VideoKey60Present, MediaKey65Present: diagnostic.VideoKey65Present, TaskMarkerClass: "UNKNOWN", ProgressMarkerClass: "UNKNOWN"}
		if diagnostic.ConversationIDPresent || diagnostic.ResponseIDPresent {
			// The provider established a stable conversation identity even if it
			// returned no usable video. This account is now permanently bound.
			task.account = account
			task.identity = identity
			task.snapshot.AccountID = account.ID
		}
		task.mu.Unlock()
		s.persistTrace(task)
		slog.Info("consumer_web_video_attempt_diagnostic",
			"task_id", task.snapshot.ID, "account_id", account.ID,
			"public_model", task.snapshot.Model, "mapped_model", account.GetMappedModel(task.snapshot.Model),
			"runtime_model_id", model, "runtime_model_display", runtimeDisplay, "cause", diagnostic.Cause,
			"request_built", diagnostic.RequestBuilt, "sent", diagnostic.Sent,
			"http_status", diagnostic.HTTPStatus, "response_received", diagnostic.ResponseReceived,
			"body_nonempty", diagnostic.BodyNonempty, "parse_pass", diagnostic.ParsePass,
			"provider_error_block", diagnostic.ProviderErrorBlock,
			"conversation_id_present", diagnostic.ConversationIDPresent,
			"response_id_present", diagnostic.ResponseIDPresent, "choice_id_present", diagnostic.ChoiceIDPresent,
			"text_present", diagnostic.TextPresent, "text_length_bucket", diagnostic.TextLengthBucket,
			"candidate_count", diagnostic.CandidateCount,
			"media_container_present", diagnostic.MediaContainerPresent,
			"video_key_60_present", diagnostic.VideoKey60Present,
			"video_slot_root_shape_valid", diagnostic.SlotRootShapeValid,
			"video_metadata_count", diagnostic.VideoMetadataCount,
			"video_url_variant_count", diagnostic.VideoURLVariantCount,
			"video_url_present", diagnostic.VideoExtractedCount > 0,
			"video_extracted_count", diagnostic.VideoExtractedCount)
		releaseVideoSelection(selection)
		if diagnostic.Cause == ConsumerWebVideoAcceptedPending && err == nil && identity.CID != "" && identity.RID != "" {
			s.recoverPendingVideo(ctx, task, account, identity)
			return
		}
		var uploadError *ConsumerWebVideoUploadError
		if errors.As(err, &uploadError) && (errors.Is(err, ErrConsumerWebAuthLost) || errors.Is(err, ErrConsumerWebForbidden)) {
			if !s.nextVideoCandidate(ctx, task, account.ID, excluded, &selection, "UPSTREAM_VIDEO_UNAVAILABLE") {
				return
			}
			continue
		}
		if err != nil || len(videos) == 0 {
			s.fail(task, "UPSTREAM_VIDEO_UNAVAILABLE")
			return
		}
		task.mu.Lock()
		task.account = account
		task.snapshot.AccountID = account.ID
		task.video = videos[0]
		task.snapshot.MIME = videos[0].MIME
		task.mu.Unlock()
		slog.Info("consumer_web_video_provider_accepted", "task_id", task.snapshot.ID, "account_id", account.ID, "attempt", attempt, "mapped_model", account.GetMappedModel(task.snapshot.Model))
		return
	}
	s.fail(task, "AUTH_REQUIRED")
}

func (s *ConsumerWebVideoTaskService) recoverPendingVideo(parent context.Context, task *consumerWebVideoTask, account *Account, identity consumerWebVideoIdentity) {
	s.recoverPendingVideoWithInterval(parent, task, account, identity, 10*time.Second)
}

func (s *ConsumerWebVideoTaskService) recoverPendingVideoWithInterval(parent context.Context, task *consumerWebVideoTask, account *Account, identity consumerWebVideoIdentity, interval time.Duration) {
	reader, ok := s.driver.(interface {
		ReadVideoConversation(context.Context, *Account, ConsumerWebSessionHandle, consumerWebVideoIdentity) (consumerWebVideoRecoveryResult, error)
	})
	if !ok {
		s.failPendingVideo(task, ConsumerWebVideoRecoveryUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	task.mu.Lock()
	task.snapshot.RecoveryStarted = true
	task.mu.Unlock()
	for ctx.Err() == nil {
		session, err := s.manager.AcquireSession(ctx, account)
		if err != nil {
			s.failPendingVideo(task, ConsumerWebVideoRecoveryUnavailable)
			return
		}
		result, err := reader.ReadVideoConversation(ctx, account, session, identity)
		task.mu.Lock()
		task.snapshot.RecoveryReadCount++
		task.snapshot.RecoveryTargetFound = append(task.snapshot.RecoveryTargetFound, result.TargetFound)
		readCount := task.snapshot.RecoveryReadCount
		task.trace.Polls = append(task.trace.Polls, ConsumerWebVideoPollTrace{Index: readCount, HTTPClass: result.HTTPClass, Parsed: result.Parsed, VideoPresent: len(result.Videos) > 0, TextPresent: result.TextPresent, MediaKey65Present: result.MediaKey65Present, CandidateCount: result.CandidateCount, CompletionClass: string(result.State), TaskMarkerClass: "UNKNOWN", ProgressMarkerClass: "UNKNOWN"})
		task.mu.Unlock()
		s.persistTrace(task)
		slog.Info("consumer_web_video_recovery_read", "task_id", task.snapshot.ID, "account_id", account.ID,
			"read_count", readCount, "target_found", result.TargetFound, "candidate_count", result.CandidateCount,
			"video_found", len(result.Videos) > 0, "state", result.State, "error_class", consumerWebVideoRecoveryErrorClass(err))
		if err != nil {
			s.failPendingVideo(task, ConsumerWebVideoRecoveryUnavailable)
			return
		}
		switch result.State {
		case consumerWebVideoRecoveryVideo:
			if len(result.Videos) == 0 {
				s.failPendingVideo(task, ConsumerWebVideoRecoveryUnavailable)
				return
			}
			task.mu.Lock()
			task.video = result.Videos[0]
			task.snapshot.MIME = result.Videos[0].MIME
			task.snapshot.RecoveryVideoFound = true
			task.snapshot.AttemptDiagnostic.Cause = ConsumerWebVideoFailureNone
			task.mu.Unlock()
			return
		case consumerWebVideoRecoveryNoVideo:
			// A completed text candidate is not proof that asynchronous video
			// generation is terminal. Keep the existing bounded recovery window.
		case consumerWebVideoRecoveryRefusal:
			s.failPendingVideo(task, ConsumerWebVideoFailureProviderBlock)
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	s.failPendingVideo(task, ConsumerWebVideoRecoveryTimeout)
}

func consumerWebVideoRecoveryErrorClass(err error) string {
	if err == nil {
		return "NONE"
	}
	if errors.Is(err, ErrConsumerWebAuthLost) {
		return "AUTH"
	}
	if errors.Is(err, ErrConsumerWebTransportTransient) {
		return "TRANSPORT"
	}
	return "PROTOCOL"
}

func (s *ConsumerWebVideoTaskService) failPendingVideo(task *consumerWebVideoTask, cause ConsumerWebVideoFailureCause) {
	task.mu.Lock()
	task.snapshot.AttemptDiagnostic.Cause = cause
	task.mu.Unlock()
	s.fail(task, "UPSTREAM_VIDEO_UNAVAILABLE")
}

func releaseVideoSelection(selection *AccountSelectionResult) {
	if selection != nil && selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func videoAcquireCauseAccountScoped(cause ConsumerWebAcquireCause) bool {
	switch cause {
	case ConsumerWebAcquireCredentialNamespaceMissing, ConsumerWebAcquireCredentialDecryptFailed,
		ConsumerWebAcquireAccountNotReady, ConsumerWebAcquireAPPTokenMissing, ConsumerWebAcquireProxyFailure:
		return true
	default:
		return false
	}
}

func (s *ConsumerWebVideoTaskService) nextVideoCandidate(ctx context.Context, task *consumerWebVideoTask, failedID int64, excluded map[int64]struct{}, next **AccountSelectionResult, failure string) bool {
	excluded[failedID] = struct{}{}
	if task.selectNext == nil || ctx.Err() != nil {
		s.fail(task, failure)
		return false
	}
	selection, err := task.selectNext(ctx, excluded)
	if err != nil || selection == nil || selection.Account == nil {
		s.fail(task, failure)
		return false
	}
	if _, repeated := excluded[selection.Account.ID]; repeated {
		releaseVideoSelection(selection)
		s.fail(task, failure)
		return false
	}
	*next = selection
	return true
}

func (s *ConsumerWebVideoTaskService) fail(task *consumerWebVideoTask, class string) {
	task.mu.Lock()
	task.snapshot.Status = ConsumerWebVideoFailed
	task.snapshot.Error = class
	task.mu.Unlock()
	s.persistTrace(task)
}

func (s *ConsumerWebVideoTaskService) get(id string, ownerID int64) (*consumerWebVideoTask, error) {
	if s == nil || ownerID <= 0 {
		return nil, ErrConsumerWebProtocolDrift
	}
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil || task.ownerID != ownerID {
		return nil, ErrConsumerWebVideoTaskNotFound
	}
	return task, nil
}

func (s *ConsumerWebVideoTaskService) Status(ctx context.Context, id string, ownerID int64) (ConsumerWebVideoTaskSnapshot, error) {
	task, err := s.get(id, ownerID)
	if err != nil {
		return ConsumerWebVideoTaskSnapshot{}, err
	}
	defer s.persistTrace(task)
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.snapshot.Status != ConsumerWebVideoRunning || task.video.URL == "" {
		return task.snapshot, nil
	}
	session, err := s.manager.AcquireSession(ctx, task.account)
	if err != nil {
		task.snapshot.Status = ConsumerWebVideoFailed
		task.snapshot.Error = "AUTH_REQUIRED"
		return task.snapshot, nil
	}
	content, ready, err := s.driver.DownloadVideo(ctx, task.account, session, task.video.URL)
	if err != nil {
		task.snapshot.Status = ConsumerWebVideoFailed
		task.snapshot.Error = "MEDIA_UNAVAILABLE"
		return task.snapshot, nil
	}
	if ready {
		task.content = content
		task.snapshot.Status = ConsumerWebVideoSucceeded
	}
	return task.snapshot, nil
}

func (s *ConsumerWebVideoTaskService) Content(id string, ownerID int64) ([]byte, string, error) {
	task, err := s.get(id, ownerID)
	if err != nil {
		return nil, "", err
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.snapshot.Status != ConsumerWebVideoSucceeded {
		return nil, "", ErrConsumerWebVideoNotReady
	}
	return append([]byte(nil), task.content...), task.snapshot.MIME, nil
}
