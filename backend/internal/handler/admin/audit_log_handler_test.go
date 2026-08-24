package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type auditClearRepo struct{ logs int64 }

func (r *auditClearRepo) BatchInsert(context.Context, []*service.AuditLog) (int64, error) {
	return 0, nil
}
func (r *auditClearRepo) Insert(context.Context, *service.AuditLog) error { return nil }
func (r *auditClearRepo) List(context.Context, *service.AuditLogFilter) (*service.AuditLogList, error) {
	return &service.AuditLogList{}, nil
}
func (r *auditClearRepo) GetByID(context.Context, int64) (*service.AuditLog, error) {
	return nil, service.ErrAuditLogNotFound
}
func (r *auditClearRepo) Count(context.Context) (int64, error) { return r.logs, nil }
func (r *auditClearRepo) TruncateAll(context.Context) error {
	r.logs = 0
	return nil
}
func (r *auditClearRepo) DeleteBefore(context.Context, time.Time, int) (int64, error) { return 0, nil }

func auditClearRouter(repo *auditClearRepo, authenticated bool, role, authMethod string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			c.Set(string(middleware.ContextKeyUserRole), role)
			c.Set("auth_method", authMethod)
			c.Next()
		})
	}
	handler := NewAuditLogHandler(service.NewAuditLogService(repo))
	router.POST("/api/v1/admin/audit-logs/clear", handler.Clear)
	return router
}

func TestAuditLogClearDoesNotRequireTOTP(t *testing.T) {
	repo := &auditClearRepo{logs: 3}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil)
	auditClearRouter(repo, true, "admin", service.AuditAuthMethodJWT).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Zero(t, repo.logs)
	require.NotContains(t, recorder.Body.String(), "TOTP")
}

func TestAuditLogClearStillRequiresAuthenticationAndAdminRole(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authenticated bool
		role          string
		want          int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "non-admin", authenticated: true, role: "user", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &auditClearRepo{logs: 2}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil)
			auditClearRouter(repo, tc.authenticated, tc.role, service.AuditAuthMethodJWT).ServeHTTP(recorder, request)
			require.Equal(t, tc.want, recorder.Code)
			require.Equal(t, int64(2), repo.logs)
		})
	}
}

func TestAuditLogClearRejectsAdminAPIKey(t *testing.T) {
	repo := &auditClearRepo{logs: 2}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil)
	auditClearRouter(repo, true, "admin", service.AuditAuthMethodAdminAPIKey).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, int64(2), repo.logs)
}
