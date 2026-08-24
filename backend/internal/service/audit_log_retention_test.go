package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type auditRetentionRepo struct {
	mu          sync.Mutex
	logs        []*AuditLog
	deleteErr   error
	deleteCalls int
}

func (r *auditRetentionRepo) BatchInsert(_ context.Context, logs []*AuditLog) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, logs...)
	return int64(len(logs)), nil
}
func (r *auditRetentionRepo) Insert(_ context.Context, log *AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, log)
	return nil
}
func (r *auditRetentionRepo) List(_ context.Context, filter *AuditLogFilter) (*AuditLogList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &AuditLogList{Logs: append([]*AuditLog(nil), r.logs...), Total: len(r.logs), Page: filter.Page, PageSize: filter.PageSize}, nil
}
func (r *auditRetentionRepo) GetByID(_ context.Context, id int64) (*AuditLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.logs {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, ErrAuditLogNotFound
}
func (r *auditRetentionRepo) Count(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.logs)), nil
}
func (r *auditRetentionRepo) TruncateAll(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = nil
	return nil
}
func (r *auditRetentionRepo) DeleteBefore(_ context.Context, cutoff time.Time, batchSize int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	if r.deleteErr != nil {
		return 0, r.deleteErr
	}
	kept := make([]*AuditLog, 0, len(r.logs))
	var deleted int64
	for _, item := range r.logs {
		if item.CreatedAt.Before(cutoff) && deleted < int64(batchSize) {
			deleted++
			continue
		}
		kept = append(kept, item)
	}
	r.logs = kept
	return deleted, nil
}

func TestAuditRetentionDeletesOnlyLogsOlderThanSevenDays(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-7 * 24 * time.Hour)
	repo := &auditRetentionRepo{logs: []*AuditLog{
		{ID: 1, CreatedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: 2, CreatedAt: now.Add(-6 * 24 * time.Hour)},
		{ID: 3, CreatedAt: cutoff},
	}}
	svc := NewAuditLogService(repo)
	svc.now = func() time.Time { return now }

	svc.runRetentionOnce()

	require.Len(t, repo.logs, 2)
	require.Equal(t, int64(2), repo.logs[0].ID)
	require.Equal(t, int64(3), repo.logs[1].ID, "a record exactly at the cutoff must be retained")
}

func TestAuditRetentionErrorDoesNotStopService(t *testing.T) {
	repo := &auditRetentionRepo{deleteErr: errors.New("temporary sqlite error")}
	svc := NewAuditLogService(repo)
	svc.retentionInterval = 10 * time.Millisecond
	svc.Start()
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.deleteCalls > 0
	}, time.Second, 10*time.Millisecond)

	done := make(chan struct{})
	go func() {
		svc.Stop()
		close(done)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestAuditClearAllLeavesNoQueuedAuditEntry(t *testing.T) {
	repo := &auditRetentionRepo{logs: []*AuditLog{{ID: 1, CreatedAt: time.Now().UTC()}}}
	svc := NewAuditLogService(repo)
	svc.Start()
	defer svc.Stop()
	svc.Record(&AuditLog{ID: 2, CreatedAt: time.Now().UTC()})

	deleted, err := svc.ClearAll(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1))
	count, err := repo.Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}
