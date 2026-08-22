package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalApplicationBootSmoke(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", filepath.Join(t.TempDir(), "personal-app.db"))
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "18080")
	t.Setenv("SKIP_SETUP", "1")

	if !personal.PrepareEnvironment("personal") {
		t.Fatal("Personal runtime must be enabled")
	}
	repository.ClosePersonalEmbeddedRedis()
	defer repository.ClosePersonalEmbeddedRedis()

	app, err := initializePersonalApplication(handler.BuildInfo{
		Version:   "personal-smoke",
		BuildType: "personal",
	})
	if err != nil {
		t.Fatalf("initialize dedicated Personal Edition without external PostgreSQL/Redis: %v", err)
	}
	if app == nil || app.Server == nil {
		t.Fatal("Personal Edition application/server must be initialized")
	}
	for _, path := range []string{
		"/api/v1/admin/tls-fingerprint-profiles",
		"/api/v1/admin/error-passthrough-rules",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		app.Server.Handler.ServeHTTP(recorder, request)
		if recorder.Code == http.StatusNotFound {
			t.Fatalf("required Personal Gateway admin route is not registered: %s", path)
		}
	}
	app.Cleanup()
}

func TestPersonalGatewayControlHandlersFreshSQLiteCRUD(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", filepath.Join(t.TempDir(), "personal-controls.db"))
	t.Setenv("SKIP_SETUP", "1")
	repository.ClosePersonalEmbeddedRedis()
	defer repository.ClosePersonalEmbeddedRedis()

	app, err := initializePersonalApplication(handler.BuildInfo{Version: "personal-controls", BuildType: "personal"})
	require.NoError(t, err)
	require.NotNil(t, app.Handlers)
	require.NotNil(t, app.Handlers.Admin)
	require.NotNil(t, app.Handlers.Admin.TLSFingerprintProfile)
	require.NotNil(t, app.Handlers.Admin.ErrorPassthrough)
	defer app.Cleanup()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	tls := router.Group("/tls")
	tls.GET("", app.Handlers.Admin.TLSFingerprintProfile.List)
	tls.POST("", app.Handlers.Admin.TLSFingerprintProfile.Create)
	tls.PUT("/:id", app.Handlers.Admin.TLSFingerprintProfile.Update)
	tls.DELETE("/:id", app.Handlers.Admin.TLSFingerprintProfile.Delete)
	rules := router.Group("/rules")
	rules.GET("", app.Handlers.Admin.ErrorPassthrough.List)
	rules.POST("", app.Handlers.Admin.ErrorPassthrough.Create)
	rules.PUT("/:id", app.Handlers.Admin.ErrorPassthrough.Update)
	rules.DELETE("/:id", app.Handlers.Admin.ErrorPassthrough.Delete)

	tlsList := personalControlRequest(t, router, http.MethodGet, "/tls", nil)
	require.Equal(t, http.StatusOK, tlsList.Code)
	personalControlRequireEmptyList(t, tlsList)
	tlsCreate := personalControlRequest(t, router, http.MethodPost, "/tls", []byte(`{"name":"windows-live"}`))
	require.Equal(t, http.StatusOK, tlsCreate.Code, tlsCreate.Body.String())
	tlsID := personalControlResponseID(t, tlsCreate)
	require.Equal(t, http.StatusOK, personalControlRequest(t, router, http.MethodPut, fmt.Sprintf("/tls/%d", tlsID), []byte(`{"name":"windows-live-updated"}`)).Code)
	require.Equal(t, http.StatusOK, personalControlRequest(t, router, http.MethodDelete, fmt.Sprintf("/tls/%d", tlsID), nil).Code)

	ruleList := personalControlRequest(t, router, http.MethodGet, "/rules", nil)
	require.Equal(t, http.StatusOK, ruleList.Code)
	personalControlRequireEmptyList(t, ruleList)
	ruleCreate := personalControlRequest(t, router, http.MethodPost, "/rules", []byte(`{"name":"gateway-rule","error_codes":[429]}`))
	require.Equal(t, http.StatusOK, ruleCreate.Code, ruleCreate.Body.String())
	ruleID := personalControlResponseID(t, ruleCreate)
	require.Equal(t, http.StatusOK, personalControlRequest(t, router, http.MethodPut, fmt.Sprintf("/rules/%d", ruleID), []byte(`{"priority":10}`)).Code)
	require.Equal(t, http.StatusOK, personalControlRequest(t, router, http.MethodDelete, fmt.Sprintf("/rules/%d", ruleID), nil).Code)
}

func personalControlRequest(t *testing.T, router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func personalControlResponseID(t *testing.T, recorder *httptest.ResponseRecorder) int64 {
	t.Helper()
	var payload struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Positive(t, payload.Data.ID, recorder.Body.String())
	return payload.Data.ID
}

func personalControlRequireEmptyList(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.NotNil(t, payload.Data, recorder.Body.String())
	require.Empty(t, payload.Data, recorder.Body.String())
}
