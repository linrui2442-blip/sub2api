package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// VideoTraceSchema lives in the existing Personal database, not a sidecar.
const VideoTraceSchema = `CREATE TABLE IF NOT EXISTS consumer_web_video_traces (task_id TEXT PRIMARY KEY, owner_id BIGINT NOT NULL, encrypted_record TEXT NOT NULL)`

// No bodies, text, URLs, cookies or tokens belong in this record.
type ConsumerWebVideoTrace struct {
	TaskID                        string
	OwnerID, AccountID            int64
	Identity                      consumerWebVideoIdentity
	Initial                       ConsumerWebVideoInitialTrace
	Polls                         []ConsumerWebVideoPollTrace
	Status                        ConsumerWebVideoTaskState
	RPC, SourcePath, PayloadShape string
}
type ConsumerWebVideoInitialTrace struct {
	CIDPresent, RIDPresent, RCIDPresent, TextPresent, FinalVideoPresent bool
	MediaKey60Present, MediaKey65Present                                bool
	// Structural presence is not evidence of a job. UNKNOWN stays explicit.
	TaskMarkerClass, ProgressMarkerClass string
}
type ConsumerWebVideoPollTrace struct {
	Index                                                 int
	HTTPClass                                             string
	Parsed, VideoPresent, TextPresent, MediaKey65Present  bool
	CandidateCount                                        int
	CompletionClass, TaskMarkerClass, ProgressMarkerClass string
}

type ConsumerWebVideoTraceStore struct {
	db        *sql.DB
	encryptor SecretEncryptor
}

var errVideoTrace = errors.New("video trace persistence unavailable")

func NewConsumerWebVideoTraceStore(db *sql.DB, encryptor SecretEncryptor) *ConsumerWebVideoTraceStore {
	return &ConsumerWebVideoTraceStore{db: db, encryptor: encryptor}
}
func (s *ConsumerWebVideoTraceStore) Save(ctx context.Context, r ConsumerWebVideoTrace) error {
	if s == nil || s.db == nil || s.encryptor == nil || r.TaskID == "" || r.OwnerID <= 0 {
		return errVideoTrace
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return errVideoTrace
	}
	cipher, err := s.encryptor.Encrypt(string(raw))
	if err != nil {
		return errVideoTrace
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO consumer_web_video_traces(task_id,owner_id,encrypted_record) VALUES(?,?,?) ON CONFLICT(task_id) DO UPDATE SET encrypted_record=excluded.encrypted_record WHERE owner_id=excluded.owner_id`, r.TaskID, r.OwnerID, cipher)
	if err != nil {
		return errVideoTrace
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return errVideoTrace
	}
	return nil
}

// Load is an internal diagnostic accessor; it never initiates provider work.
// Its result must not be serialized to a public status response or logger.
func (s *ConsumerWebVideoTraceStore) Load(ctx context.Context, id string, owner int64) (ConsumerWebVideoTrace, error) {
	var r ConsumerWebVideoTrace
	var cipher string
	if s == nil || s.db == nil || s.encryptor == nil {
		return r, errVideoTrace
	}
	if err := s.db.QueryRowContext(ctx, `SELECT encrypted_record FROM consumer_web_video_traces WHERE task_id=? AND owner_id=?`, id, owner).Scan(&cipher); err != nil {
		return r, errVideoTrace
	}
	plain, err := s.encryptor.Decrypt(cipher)
	if err != nil {
		return r, errVideoTrace
	}
	if json.Unmarshal([]byte(plain), &r) != nil || r.TaskID != id || r.OwnerID != owner {
		return ConsumerWebVideoTrace{}, errVideoTrace
	}
	return r, nil
}
func (s *ConsumerWebVideoTaskService) SetTraceStore(store *ConsumerWebVideoTraceStore) {
	s.traceStore = store
}

// RecoverProviderTrace is offline-only: no account/session acquisition, media
// downloads or generation are triggered by reconstruction. Callers must enforce
// the same API-key owner scope as the original task. Never expose this via JSON.
func (s *ConsumerWebVideoTaskService) RecoverProviderTrace(ctx context.Context, id string, ownerID int64) (ConsumerWebVideoTrace, error) {
	if s == nil || s.traceStore == nil {
		return ConsumerWebVideoTrace{}, errVideoTrace
	}
	return s.traceStore.Load(ctx, id, ownerID)
}
func (s *ConsumerWebVideoTaskService) persistTrace(task *consumerWebVideoTask) {
	if s.traceStore == nil {
		return
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	task.trace.TaskID = task.snapshot.ID
	task.trace.OwnerID = task.ownerID
	task.trace.AccountID = task.snapshot.AccountID
	task.trace.Identity = task.identity
	task.trace.Status = task.snapshot.Status
	task.trace.RPC = "hNvQHb"
	task.trace.SourcePath = "/app"
	task.trace.PayloadShape = "CID,10,null,1,[1],[4],null,1"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.traceStore.Save(ctx, task.trace) != nil {
		slog.Warn("consumer_web_video_trace_persist_failed", "task_id", task.snapshot.ID)
	}
}
