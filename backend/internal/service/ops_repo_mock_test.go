package service

import (
	"context"
	"time"
)

// opsRepoMock is a test-only OpsRepository implementation with optional function hooks.
type opsRepoMock struct {
	InsertErrorLogFn              func(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error)
	BatchInsertErrorLogsFn        func(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error)
	BatchInsertSystemLogsFn       func(ctx context.Context, inputs []*OpsInsertSystemLogInput) (int64, error)
	ListSystemLogsFn              func(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error)
	DeleteSystemLogsFn            func(ctx context.Context, filter *OpsSystemLogCleanupFilter) (int64, error)
	InsertSystemLogCleanupAuditFn func(ctx context.Context, input *OpsSystemLogCleanupAudit) error
}

func (m *opsRepoMock) InsertErrorLog(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error) {
	if m.InsertErrorLogFn != nil {
		return m.InsertErrorLogFn(ctx, input)
	}
	return 0, nil
}

func (m *opsRepoMock) BatchInsertErrorLogs(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error) {
	if m.BatchInsertErrorLogsFn != nil {
		return m.BatchInsertErrorLogsFn(ctx, inputs)
	}
	return int64(len(inputs)), nil
}

func (m *opsRepoMock) ListErrorLogs(ctx context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	return &OpsErrorLogList{Errors: []*OpsErrorLog{}, Page: 1, PageSize: 20}, nil
}

func (m *opsRepoMock) GetErrorLogByID(ctx context.Context, id int64) (*OpsErrorLogDetail, error) {
	return &OpsErrorLogDetail{}, nil
}

func (m *opsRepoMock) ListRequestDetails(ctx context.Context, filter *OpsRequestDetailFilter) ([]*OpsRequestDetail, int64, error) {
	return []*OpsRequestDetail{}, 0, nil
}

func (m *opsRepoMock) BatchInsertSystemLogs(ctx context.Context, inputs []*OpsInsertSystemLogInput) (int64, error) {
	if m.BatchInsertSystemLogsFn != nil {
		return m.BatchInsertSystemLogsFn(ctx, inputs)
	}
	return int64(len(inputs)), nil
}

func (m *opsRepoMock) ListSystemLogs(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error) {
	if m.ListSystemLogsFn != nil {
		return m.ListSystemLogsFn(ctx, filter)
	}
	return &OpsSystemLogList{Logs: []*OpsSystemLog{}, Total: 0, Page: 1, PageSize: 50}, nil
}

func (m *opsRepoMock) DeleteSystemLogs(ctx context.Context, filter *OpsSystemLogCleanupFilter) (int64, error) {
	if m.DeleteSystemLogsFn != nil {
		return m.DeleteSystemLogsFn(ctx, filter)
	}
	return 0, nil
}

func (m *opsRepoMock) InsertSystemLogCleanupAudit(ctx context.Context, input *OpsSystemLogCleanupAudit) error {
	if m.InsertSystemLogCleanupAuditFn != nil {
		return m.InsertSystemLogCleanupAuditFn(ctx, input)
	}
	return nil
}

func (m *opsRepoMock) UpdateErrorResolution(ctx context.Context, errorID int64, resolved bool, resolvedByUserID *int64, resolvedAt *time.Time) error {
	return nil
}

var _ OpsRepository = (*opsRepoMock)(nil)
