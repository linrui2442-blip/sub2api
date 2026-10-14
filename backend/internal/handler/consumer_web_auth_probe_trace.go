package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// This deliberately contains no account identifiers, upstream data or error text.
// Three snapshots with one server-generated ID form one logical invocation record.
type authProbeTrace struct {
	CorrelationID    string `json:"request_correlation_id"`
	Timestamp        string `json:"timestamp"`
	Started          bool   `json:"probe_started"`
	PlatformOK       bool   `json:"platform_prepare_ok"`
	Selected         bool   `json:"eligible_account_selected"`
	AccountIDPresent bool   `json:"eligible_account_id_present"`
	Attempted        bool   `json:"acquire_session_attempted"`
	Succeeded        bool   `json:"acquire_session_succeeded"`
	Result           string `json:"safe_result_class"`
	Bootstrap        string `json:"bootstrap_reached_if_observable"`
	OTAQ             string `json:"otaq_reached_if_observable"`
	Rotate           string `json:"rotate_recovery_attempted_if_observable"`
	Status           int    `json:"http_status_selected"`
	JSONAttempted    bool   `json:"json_response_attempted"`
	WriteSucceeded   bool   `json:"json_response_write_succeeded"`
	WriteError       string `json:"json_response_write_error_class"`
	Completed        bool   `json:"probe_completed"`
}

func newAuthProbeTrace() *authProbeTrace {
	return &authProbeTrace{CorrelationID: uuid.NewString(), Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Started: true, Result: "OTHER_SAFE_ERROR", Bootstrap: "UNKNOWN", OTAQ: "UNKNOWN", Rotate: "UNKNOWN", WriteError: "NOT_ATTEMPTED"}
}

func persistAuthProbeTrace(t *authProbeTrace) {
	// Use the existing structured runtime file sink, without request-context fields.
	logger.L().Info("consumer_web_auth_probe_trace", zap.Any("auth_probe_trace", *t))
	logger.Sync()
}

type authProbeWriter struct {
	gin.ResponseWriter
	trace *authProbeTrace
}

func (w *authProbeWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	w.trace.WriteSucceeded = err == nil
	w.trace.WriteError = safeProbeWriteError(err)
	return n, err
}

func safeProbeWriteError(err error) string {
	if err == nil {
		return "NONE"
	}
	if errors.Is(err, context.Canceled) {
		return "CONTEXT_CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DEADLINE"
	}
	if errors.Is(err, io.ErrShortWrite) {
		return "SHORT_WRITE"
	}
	if errors.Is(err, net.ErrClosed) {
		return "CONNECTION_CLOSED"
	}
	return "WRITE_FAILED"
}

func writeAuthProbeJSON(c *gin.Context, t *authProbeTrace, status int, body any, save func(*authProbeTrace)) {
	t.Status = status
	t.JSONAttempted = true
	// Save the decision BEFORE writing: client loss must not erase the result.
	save(t)
	original := c.Writer
	c.Writer = &authProbeWriter{ResponseWriter: original, trace: t}
	defer func() { c.Writer = original }()
	c.JSON(status, body)
	t.Completed = true
}
