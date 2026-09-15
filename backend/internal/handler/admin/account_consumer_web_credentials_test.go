package admin

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerConsumerWebEncryptor struct{ fail bool }

func (e handlerConsumerWebEncryptor) Encrypt(v string) (string, error) {
	if e.fail {
		return "", errors.New("synthetic encryption failure")
	}
	return "cipher:" + v, nil
}
func (e handlerConsumerWebEncryptor) Decrypt(v string) (string, error) {
	return strings.TrimPrefix(v, "cipher:"), nil
}

func runConsumerWebCredentialHandler(t *testing.T, svc *stubAdminService, encryptor service.SecretEncryptor, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/2/consumer-web-credentials", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "2"}}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetConsumerWebCredentialEncryptor(encryptor)
	h.UpdateConsumerWebCredentials(c)
	return w
}

func TestUpdateConsumerWebCredentialsExistenceOnlyAndPartialMerge(t *testing.T) {
	svc := newStubAdminService()
	svc.getAccountResult = &service.Account{ID: 2, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "oauth", "project_id": "project", service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: "cipher:old-psid"},
	}}
	w := runConsumerWebCredentialHandler(t, svc, handlerConsumerWebEncryptor{}, `{"secure_1psidts":"test-psidts-secret"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "test-psidts-secret")
	require.NotContains(t, w.Body.String(), "cipher:")
	require.Contains(t, w.Body.String(), `"has_secure_1psid":true`)
	require.Contains(t, w.Body.String(), `"has_secure_1psidts":true`)
	creds := svc.lastUpdateAccountInput.Credentials
	require.Equal(t, "oauth", creds["access_token"])
	require.Equal(t, "project", creds["project_id"])
	ns := creds[service.ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "cipher:old-psid", ns[service.ConsumerWebPSIDCiphertextKey])
	require.NotEqual(t, "test-psidts-secret", ns[service.ConsumerWebPSIDTSCiphertextKey])
}

func TestUpdateConsumerWebCredentialsRejectsWrongAccountAndEncryptionFailureIsAtomic(t *testing.T) {
	svc := newStubAdminService()
	svc.getAccountResult = &service.Account{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	w := runConsumerWebCredentialHandler(t, svc, handlerConsumerWebEncryptor{}, `{"secure_1psid":"test-psid-secret"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, svc.updateAccountCalls)

	svc = newStubAdminService()
	svc.getAccountResult = &service.Account{ID: 2, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth}
	w = runConsumerWebCredentialHandler(t, svc, handlerConsumerWebEncryptor{fail: true}, `{"secure_1psid":"test-psid-secret","secure_1psidts":"test-psidts-secret"}`)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Zero(t, svc.updateAccountCalls)
}

func TestApplyOAuthCredentialsPreservesConsumerWebNamespaceAndProviderMetadata(t *testing.T) {
	svc := newStubAdminService()
	svc.getAccountResult = &service.Account{ID: 2, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Credentials: map[string]any{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
		"project_id":    "project",
		"paid_tier":     true,
		"plan_type":     "pro",
		"cookie":        "legacy-cookie-must-not-survive",
		"model_mapping": map[string]any{"public-model": "upstream-model"},
		service.ConsumerWebCredentialNamespace: map[string]any{
			service.ConsumerWebPSIDCiphertextKey:   "cipher:psid",
			service.ConsumerWebPSIDTSCiphertextKey: "cipher:psidts",
		},
	}}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/2/apply-oauth-credentials", bytes.NewBufferString(`{"type":"oauth","credentials":{"access_token":"new-access"}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "2"}}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.ApplyOAuthCredentials(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, svc.updateAccountCalls)
	creds := svc.lastUpdateAccountInput.Credentials
	require.Equal(t, "new-access", creds["access_token"])
	require.Equal(t, "old-refresh", creds["refresh_token"])
	require.Equal(t, "project", creds["project_id"])
	require.Equal(t, true, creds["paid_tier"])
	require.Equal(t, "pro", creds["plan_type"])
	require.Equal(t, map[string]any{"public-model": "upstream-model"}, creds["model_mapping"])
	require.NotContains(t, creds, "cookie")
	ns := creds[service.ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "cipher:psid", ns[service.ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "cipher:psidts", ns[service.ConsumerWebPSIDTSCiphertextKey])
}
