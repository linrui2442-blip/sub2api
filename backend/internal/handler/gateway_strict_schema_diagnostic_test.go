package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type genericSanitizedForwardError struct {
	diagnostic error
}

func (e *genericSanitizedForwardError) Error() string {
	return "Upstream response did not satisfy requested strict JSON schema"
}
func (e *genericSanitizedForwardError) Unwrap() error { return e.diagnostic }

func TestGatewayForwardBoundaryKeepsStrictSchemaDiagnosticInternal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Header("Content-Type", "application/json")
	c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
		"type":    "structured_output_validation_error",
		"message": "Upstream response did not satisfy requested strict JSON schema",
	}})

	diagnostic := &service.StrictJSONValidationError{
		Path: "$.nested.field", Keyword: "enum", Expected: "allowed enum member", ActualType: "string", Stage: "validation",
	}
	err := &genericSanitizedForwardError{diagnostic: diagnostic}

	var preserved *service.StrictJSONValidationError
	require.True(t, errors.As(err, &preserved))
	require.Equal(t, diagnostic, preserved)
	require.True(t, gatewayForwardErrorAlreadyCommunicated(c, 0, err))
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "structured_output_validation_error")
	require.NotContains(t, recorder.Body.String(), diagnostic.Path)
	require.NotContains(t, recorder.Body.String(), `"path"`)
	require.NotContains(t, recorder.Body.String(), `"keyword"`)
	require.NotContains(t, recorder.Body.String(), `"expected"`)
	require.NotContains(t, recorder.Body.String(), `"actual_type"`)
	require.NotContains(t, recorder.Body.String(), `"stage"`)
}
