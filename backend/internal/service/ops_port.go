package service

import (
	"context"
	"time"
)

type OpsRepository interface {
	InsertErrorLog(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error)
	BatchInsertErrorLogs(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error)
	ListErrorLogs(ctx context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error)
	GetErrorLogByID(ctx context.Context, id int64) (*OpsErrorLogDetail, error)
	ListRequestDetails(ctx context.Context, filter *OpsRequestDetailFilter) ([]*OpsRequestDetail, int64, error)
	BatchInsertSystemLogs(ctx context.Context, inputs []*OpsInsertSystemLogInput) (int64, error)
	ListSystemLogs(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error)
	DeleteSystemLogs(ctx context.Context, filter *OpsSystemLogCleanupFilter) (int64, error)
	InsertSystemLogCleanupAudit(ctx context.Context, input *OpsSystemLogCleanupAudit) error

	UpdateErrorResolution(ctx context.Context, errorID int64, resolved bool, resolvedByUserID *int64, resolvedAt *time.Time) error
}

type OpsInsertErrorLogInput struct {
	RequestID       string
	ClientRequestID string

	UserID    *int64
	APIKeyID  *int64
	AccountID *int64
	GroupID   *int64
	ClientIP  *string

	Platform    string
	Model       string
	RequestPath string
	Stream      bool
	// InboundEndpoint is the normalized client-facing API endpoint path, e.g. /v1/chat/completions.
	InboundEndpoint string
	// UpstreamEndpoint is the normalized upstream endpoint path, e.g. /v1/responses.
	UpstreamEndpoint string
	// RequestedModel is the client-requested model name before mapping.
	RequestedModel string
	// UpstreamModel is the actual model sent to upstream after mapping. Empty means no mapping.
	UpstreamModel string
	// RequestType is the granular request type: 0=unknown, 1=sync, 2=stream, 3=ws_v2.
	// Matches service.RequestType enum semantics from usage_log.go.
	RequestType *int16
	UserAgent   string

	ErrorPhase        string
	ErrorType         string
	Severity          string
	StatusCode        int
	IsBusinessLimited bool
	IsCountTokens     bool // 是否为 count_tokens 请求

	ErrorMessage string
	ErrorBody    string

	ErrorSource string
	ErrorOwner  string

	UpstreamStatusCode   *int
	UpstreamErrorMessage *string
	UpstreamErrorDetail  *string
	// UpstreamErrors captures all upstream error attempts observed during handling this request.
	// It is populated during request processing (gin context) and sanitized+serialized by OpsService.
	UpstreamErrors []*OpsUpstreamErrorEvent
	// UpstreamErrorsJSON is the sanitized JSON string stored into ops_error_logs.upstream_errors.
	// It is set by OpsService.RecordError before persisting.
	UpstreamErrorsJSON *string

	AuthLatencyMs      *int64
	RoutingLatencyMs   *int64
	UpstreamLatencyMs  *int64
	ResponseLatencyMs  *int64
	TimeToFirstTokenMs *int64

	CreatedAt time.Time

	// 有效(未删除)key 报错时快照的 key 脱敏前缀(前 8 位)。
	// 落库快照而非读时 JOIN:key 之后被删(key 列被 tombstone 覆盖)仍保留当时前缀。
	APIKeyPrefix string
}

type OpsInsertSystemLogInput struct {
	CreatedAt       time.Time
	Host            string
	Level           string
	Component       string
	Message         string
	RequestID       string
	ClientRequestID string
	UserID          *int64
	APIKeyID        *int64
	AccountID       *int64
	Platform        string
	Model           string
	ExtraJSON       string
}

type OpsSystemLogFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	Host      string

	Level     string
	Component string

	RequestID       string
	ClientRequestID string
	UserID          *int64
	APIKeyID        *int64
	AccountID       *int64
	Platform        string
	Model           string
	Query           string

	Page     int
	PageSize int
}

type OpsSystemLogCleanupFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	Host      string

	Level     string
	Component string

	RequestID       string
	ClientRequestID string
	UserID          *int64
	APIKeyID        *int64
	AccountID       *int64
	Platform        string
	Model           string
	Query           string
}

type OpsSystemLogList struct {
	Logs     []*OpsSystemLog `json:"logs"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type OpsSystemLogCleanupAudit struct {
	CreatedAt   time.Time
	OperatorID  int64
	Conditions  string
	DeletedRows int64
}
