package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type probeFailureManager struct{ err error }

func (m probeFailureManager) AcquireSession(context.Context, *service.Account) (service.ConsumerWebSessionHandle, error) {
	return nil, m.err
}

type probeFailWriter struct{ *httptest.ResponseRecorder }

func (w probeFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("secret-cookie upstream-url token")
}

func TestConsumerWebAuthProbeTraceResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *service.Account
		err     error
		result  string
		status  int
	}{
		{"no_account", nil, nil, "NO_ELIGIBLE_ACCOUNT", 503},
		{"acquire_failure", &service.Account{ID: 7}, errors.New("secret-cookie"), "SESSION_ACQUIRE_FAILED", 503},
		{"success", &service.Account{ID: 7}, nil, "SUCCESS", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newAuthProbeTrace()
			tr.PlatformOK = true
			_, _, _ = acquireConsumerWebForProbe(context.Background(), &authProbeSchedulerFixture{account: tc.account}, probeFailureManager{tc.err}, nil, "synthetic", tr)
			if tr.Result != tc.result || tr.Selected != (tc.account != nil) || tr.Attempted != (tc.account != nil) || tr.Succeeded != (tc.result == "SUCCESS") {
				t.Fatalf("unexpected trace: %+v", tr)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			writeAuthProbeJSON(c, tr, tc.status, gin.H{"code": tr.Result}, func(*authProbeTrace) {})
			if w.Code != tc.status || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || !tr.WriteSucceeded || tr.WriteError != "NONE" {
				t.Fatal("JSON/write contract failed")
			}
		})
	}
}

func TestConsumerWebAuthProbeTraceSurvivesWriteFailure(t *testing.T) {
	tr := newAuthProbeTrace()
	tr.Result = "SESSION_ACQUIRE_FAILED"
	tr.Attempted = true
	tr.Selected = true
	path := filepath.Join(t.TempDir(), "trace.json")
	save := func(tr *authProbeTrace) {
		b, err := json.Marshal(tr)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := gin.CreateTestContext(probeFailWriter{httptest.NewRecorder()})
	writeAuthProbeJSON(c, tr, 503, gin.H{"code": tr.Result}, save)
	before, _ := os.ReadFile(path)
	if !strings.Contains(string(before), "SESSION_ACQUIRE_FAILED") {
		t.Fatal("decision not persisted before write")
	}
	tr.Completed = true
	save(tr)
	b, _ := os.ReadFile(path)
	var recovered authProbeTrace
	if err := json.Unmarshal(b, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Result != "SESSION_ACQUIRE_FAILED" || recovered.WriteSucceeded || recovered.WriteError != "WRITE_FAILED" || !recovered.Completed {
		t.Fatal("outcome lost")
	}
	for _, secret := range []string{"secret-cookie", "upstream-url", "token"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("raw error leaked")
		}
	}
}

func TestConsumerWebAuthProbeTraceGateJSON(t *testing.T) {
	for _, remote := range []string{"192.0.2.1:1234", "127.0.0.1:1234"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/_acceptance/consumer-web/auth-probe", nil)
		c.Request.RemoteAddr = remote
		(&GatewayHandler{}).ConsumerWebAuthProbe(c)
		if !json.Valid(w.Body.Bytes()) || !c.IsAborted() {
			t.Fatal("gate contract changed")
		}
		want := 403
		if strings.HasPrefix(remote, "127.") {
			want = 401
		}
		if w.Code != want {
			t.Fatal("status changed")
		}
	}
}
