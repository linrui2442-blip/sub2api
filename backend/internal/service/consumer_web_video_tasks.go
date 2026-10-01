package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type ConsumerWebVideoTaskState string

const (
	ConsumerWebVideoQueued    ConsumerWebVideoTaskState = "queued"
	ConsumerWebVideoRunning   ConsumerWebVideoTaskState = "running"
	ConsumerWebVideoSucceeded ConsumerWebVideoTaskState = "succeeded"
	ConsumerWebVideoFailed    ConsumerWebVideoTaskState = "failed"
)

type ConsumerWebVideoTaskSnapshot struct {
	ID        string                    `json:"id"`
	Status    ConsumerWebVideoTaskState `json:"status"`
	Model     string                    `json:"model"`
	AccountID int64                     `json:"-"`
	MIME      string                    `json:"mime_type,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

type consumerWebVideoTask struct {
	mu       sync.Mutex
	snapshot ConsumerWebVideoTaskSnapshot
	ownerID  int64
	account  *Account
	video    consumerWebGeneratedVideo
	content  []byte
	created  time.Time
}

var (
	ErrConsumerWebVideoTaskNotFound = errors.New("video task not found")
	ErrConsumerWebVideoNotReady     = errors.New("video content not ready")
)

type ConsumerWebVideoDriver interface {
	GenerateVideo(context.Context, *Account, ConsumerWebSessionHandle, string, string, *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, error)
	DownloadVideo(context.Context, *Account, ConsumerWebSessionHandle, string) ([]byte, bool, error)
}

type ConsumerWebVideoTaskService struct {
	mu      sync.Mutex
	tasks   map[string]*consumerWebVideoTask
	manager *ConsumerWebSessionManager
	driver  ConsumerWebVideoDriver
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

// Start binds the naturally selected account once. The worker never schedules
// another account, including for status or content retrieval.
func (s *ConsumerWebVideoTaskService) Start(ownerID int64, account *Account, publicModel, prompt string, image *ConsumerWebVideoUpload, release func()) (ConsumerWebVideoTaskSnapshot, error) {
	if s == nil || s.manager == nil || s.driver == nil || account == nil || ownerID <= 0 || strings.TrimSpace(prompt) == "" {
		if release != nil {
			release()
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
		if release != nil {
			release()
		}
		return ConsumerWebVideoTaskSnapshot{}, ErrConsumerWebRateLimited
	}
	id := "video_" + consumerWebVideoRequestID()
	task := &consumerWebVideoTask{ownerID: ownerID, account: account, created: time.Now(), snapshot: ConsumerWebVideoTaskSnapshot{ID: id, Status: ConsumerWebVideoQueued, Model: publicModel, AccountID: account.ID}}
	s.tasks[id] = task
	s.mu.Unlock()
	go s.run(task, prompt, image, release)
	return task.snapshot, nil
}

func (s *ConsumerWebVideoTaskService) run(task *consumerWebVideoTask, prompt string, image *ConsumerWebVideoUpload, release func()) {
	if release != nil {
		defer release()
	}
	task.mu.Lock()
	task.snapshot.Status = ConsumerWebVideoRunning
	task.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	session, err := s.manager.AcquireSession(ctx, task.account)
	if err != nil {
		s.fail(task, "AUTH_REQUIRED")
		return
	}
	model, err := resolveConsumerWebVideoModel(s.manager.Registry(task.account.ID))
	if err != nil {
		s.fail(task, "MODEL_NOT_AVAILABLE")
		return
	}
	videos, err := s.driver.GenerateVideo(ctx, task.account, session, model, prompt, image)
	if err != nil || len(videos) == 0 {
		s.fail(task, "UPSTREAM_VIDEO_UNAVAILABLE")
		return
	}
	task.mu.Lock()
	task.video = videos[0]
	task.snapshot.MIME = videos[0].MIME
	task.mu.Unlock()
}

func (s *ConsumerWebVideoTaskService) fail(task *consumerWebVideoTask, class string) {
	task.mu.Lock()
	task.snapshot.Status = ConsumerWebVideoFailed
	task.snapshot.Error = class
	task.mu.Unlock()
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
