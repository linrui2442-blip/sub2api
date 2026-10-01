package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type consumerWebTransportEncryptor struct{}

func (consumerWebTransportEncryptor) Encrypt(v string) (string, error) { return "enc:" + v, nil }
func (consumerWebTransportEncryptor) Decrypt(v string) (string, error) {
	return strings.TrimPrefix(v, "enc:"), nil
}

type consumerWebTestUpstream struct {
	responses []*http.Response
	requests  []*http.Request
	proxies   []string
}

type consumerWebTLSFailureUpstream struct{ err error }

func (u consumerWebTLSFailureUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return nil, u.err
}
func (u consumerWebTLSFailureUpstream) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, c)
}

func TestConsumerWebTLSCauseSurvivesManagerWrapping(t *testing.T) {
	const secret = "synthetic-cookie-and-proxy-marker"
	root := fmt.Errorf("%s: %w", secret, io.EOF)
	d := HTTPTransportDiagnostic{TLSStarted: true, TLSCompleted: false, TLSCauseCode: ClassifyTLSCause(root)}
	FinalizeHTTPTransportDiagnostic(&d, root, false)
	upstream := consumerWebTLSFailureUpstream{err: NewHTTPTransportDiagnosticError(d, root)}
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{
		HTTPUpstream: upstream, Encryptor: consumerWebTransportEncryptor{},
		Endpoints: &consumerWebEndpointSet{app: "http://local.invalid/app", otaq: "http://local.invalid/otaq", rotate: "http://local.invalid/rotate"},
	})
	creds := &sessionFakeCredentials{values: map[string]any{ConsumerWebCredentialNamespace: map[string]any{
		ConsumerWebPSIDCiphertextKey: "enc:synthetic-psid", ConsumerWebPSIDTSCiphertextKey: "enc:synthetic-psidts",
	}}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, RefreshInterval: time.Hour})
	defer m.Close()
	_, err := m.AcquireSession(context.Background(), testSessionAccount(9753))
	var carrier *TLSCauseDiagnosticError
	var diagnostic *HTTPTransportDiagnosticError
	if err == nil || !errors.As(err, &carrier) || carrier.Code != TLSCauseEOF || !errors.As(err, &diagnostic) {
		t.Fatal("safe TLS cause did not survive Consumer Web and manager wrapping")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprint(errors.Unwrap(err)), secret) {
		t.Fatal("raw error leaked through manager wrapping")
	}
}

func (u *consumerWebTestUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, r)
	u.proxies = append(u.proxies, proxy)
	x := u.responses[0]
	u.responses = u.responses[1:]
	return x, nil
}
func (u *consumerWebTestUpstream) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, c)
}

func consumerWebResp(status int, body string, cookies ...*http.Cookie) *http.Response {
	h := make(http.Header)
	for _, c := range cookies {
		h.Add("Set-Cookie", c.String())
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestConsumerWebTransportProductionEndpointsAndOTAQParser(t *testing.T) {
	if consumerWebAppURL != "https://gemini.google.com/app" || consumerWebOTAQURL != "https://gemini.google.com/_/BardChatUi/data/batchexecute" || consumerWebRotateURL != "https://accounts.google.com/RotateCookies" {
		t.Fatal("production endpoint changed")
	}
	inner := make([]any, 16)
	inner[14] = float64(1000)
	inner[15] = []any{map[string]any{"id": "m", "display": "Model", "tier": "pro"}}
	b, _ := jsonMarshal(inner)
	frame, _ := jsonMarshal([]any{"wrb.fr", "otAQ7b", string(b)})
	status, registry, err := parseConsumerWebOTAQ(")]}'\n" + string(frame) + "\n")
	if err != nil || status != 1000 || len(registry) != 1 || registry[0].ID != "m" {
		t.Fatalf("parser: status=%d registry=%v err=%v", status, registry, err)
	}
}

func TestConsumerWebOTAQPositionalAuthenticatedRegistry(t *testing.T) {
	entry := make([]any, 20)
	entry[0] = "current-pro-selector"
	entry[1] = "Pro"
	entry[4] = 2
	entry[11] = "3.1 Pro"
	inner := make([]any, 16)
	inner[14] = nil
	inner[15] = []any{entry}
	payload, _ := json.Marshal(inner)
	frame, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(payload)})
	status, registry, err := parseConsumerWebOTAQ(")]}'\n" + string(frame) + "\n")
	require.NoError(t, err)
	require.Equal(t, 1000, status)
	require.Equal(t, []ConsumerWebModel{{ID: "current-pro-selector", Display: "3.1 Pro", Tier: "2"}}, registry)
	selected, err := resolveConsumerWebVideoModel(registry)
	require.NoError(t, err)
	require.Equal(t, "current-pro-selector", selected)
}

func TestConsumerWebOTAQStatusNormalizationAndProtocolDrift(t *testing.T) {
	makeBody := func(v any) string {
		inner := make([]any, 16)
		inner[14] = v
		b, _ := json.Marshal(inner)
		f, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
		return ")]}'\n7\n" + string(f) + "\n"
	}
	for _, tc := range []struct {
		name string
		v    any
		want int
	}{
		{"primary nil", nil, 1000},
		{"secondary available", float64(1000), 1000},
		{"auth lost", float64(1016), 1016},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := parseConsumerWebOTAQ(makeBody(tc.v))
			if err != nil || got != tc.want {
				t.Fatalf("status=%d err=%v want=%d", got, err, tc.want)
			}
		})
	}
	for _, v := range []any{float64(42), "bad"} {
		if _, _, err := parseConsumerWebOTAQ(makeBody(v)); !errors.Is(err, ErrConsumerWebProtocolDrift) {
			t.Fatalf("unknown status should fail closed: %v", err)
		}
	}
}

func TestConsumerWebOTAQMultiframeAndMalformedNoise(t *testing.T) {
	inner := make([]any, 16)
	inner[14] = nil
	b, _ := json.Marshal(inner)
	f, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
	body := "noise\n12\n[malformed\n" + string(f) + "\n"
	status, _, err := parseConsumerWebOTAQ(body)
	if err != nil || status != 1000 {
		t.Fatalf("status=%d err=%v", status, err)
	}
}

func consumerWebFullEnvelopeBody(status any, extraFrames ...[]any) string {
	inner := make([]any, 16)
	inner[14] = status
	inner[15] = []any{map[string]any{"id": "synthetic-model", "display": "Synthetic", "tier": "pro"}}
	payload, _ := json.Marshal(inner)
	frames := append(extraFrames, []any{"wrb.fr", "otAQ7b", string(payload), nil, nil, nil, "generic"})
	outer, _ := json.Marshal(frames)
	return ")]}'\n" + strconv.Itoa(len(outer)) + "\n" + string(outer) + "\n"
}

func TestConsumerWebOTAQFullEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  int
	}{
		{"available_null", nil, 1000},
		{"available_numeric", float64(1000), 1000},
		{"unauthenticated", float64(1016), 1016},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, registry, err := parseConsumerWebOTAQ(consumerWebFullEnvelopeBody(tc.value))
			if err != nil || got != tc.want || len(registry) != 1 {
				t.Fatal("full-envelope normalization failed")
			}
		})
	}
	got, _, err := parseConsumerWebOTAQ(consumerWebFullEnvelopeBody(nil, []any{"wrb.fr", "other", "[]"}, []any{"di", 123}))
	if err != nil || got != 1000 {
		t.Fatal("matching frame after unrelated frames was not selected")
	}
	got, _, err = parseConsumerWebOTAQ("noise\n[malformed\n" + consumerWebFullEnvelopeBody(nil))
	if err != nil || got != 1000 {
		t.Fatal("malformed unrelated line was not ignored")
	}
	for _, body := range []string{
		")]}'\n[[\"wrb.fr\",\"otAQ7b\"]]",
		")]}'\n[[\"wrb.fr\",\"otAQ7b\",{}]]",
		")]}'\n[[\"other\",\"otAQ7b\",\"[]\"]]",
		")]}'\n[[\"wrb.fr\",\"wrong\",\"[]\"]]",
		")]}'\n[[[\"wrb.fr\",\"otAQ7b\",\"[]\"]]]",
	} {
		if _, _, err := parseConsumerWebOTAQ(body); !errors.Is(err, ErrConsumerWebProtocolDrift) {
			t.Fatal("malformed or unsupported envelope must fail closed")
		}
	}
}

func TestConsumerWebOTAQA3GSafeShapeReconstruction(t *testing.T) {
	inner := make([]any, 16)
	inner[14] = nil
	payload, _ := json.Marshal(inner)
	line1, _ := json.Marshal([]any{[]any{"wrb.fr", "otAQ7b", string(payload), nil, nil, nil, "generic"}})
	line2, _ := json.Marshal([]any{[]any{"di", 1}, []any{"af.httprm", 1, "synthetic", 0}})
	line3, _ := json.Marshal([]any{[]any{"e", 4, nil, nil, 0}})
	body := ")]}'\n" + strconv.Itoa(len(line1)) + "\n" + string(line1) + "\n" + strconv.Itoa(len(line2)) + "\n" + string(line2) + "\n" + strconv.Itoa(len(line3)) + "\n" + string(line3)
	status, _, err := parseConsumerWebOTAQ(body)
	if err != nil || status != 1000 {
		t.Fatal("A3G safe-shape synthetic envelope was not normalized")
	}
}

func TestConsumerWebTransportAppAuthGateAndCookieJar(t *testing.T) {
	appURL := "http://app.test/app"
	otaqURL := "http://app.test/otaq"
	rotateURL := "http://app.test/rotate"
	inner := make([]any, 16)
	inner[14] = float64(1000)
	b, _ := jsonMarshal(inner)
	frame, _ := jsonMarshal([]any{"wrb.fr", "otAQ7b", string(b)})
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, `"SNlM0e":"token" "cfb2h":"b" "FdrFJe":"s" "TuX5cc":"en"`), consumerWebResp(200, string(frame))}}
	tx := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Endpoints: &consumerWebEndpointSet{app: appURL, otaq: otaqURL, rotate: rotateURL}})
	acct := &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
	creds := map[string]any{ConsumerWebCredentialNamespace: map[string]any{ConsumerWebPSIDCiphertextKey: "enc:psid", ConsumerWebPSIDTSCiphertextKey: "enc:psidts"}}
	res, err := tx.Bootstrap(context.Background(), acct, creds, 1)
	if err != nil || !res.Authenticated {
		t.Fatalf("bootstrap err=%v result=%+v", err, res)
	}
	if len(u.requests) != 2 || u.requests[0].Method != http.MethodGet || u.requests[1].Method != http.MethodPost {
		t.Fatalf("unexpected requests=%d", len(u.requests))
	}
}

func consumerWebTestTransport(u *consumerWebTestUpstream) *ConsumerWebHTTPTransport {
	return NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{HTTPUpstream: u, Encryptor: consumerWebTransportEncryptor{}, Endpoints: &consumerWebEndpointSet{app: "http://app.test/app", otaq: "http://app.test/otaq", rotate: "http://accounts.test/rotate"}})
}

func consumerWebTestAccount() *Account {
	return &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
}
func consumerWebTestCreds() map[string]any {
	return map[string]any{ConsumerWebCredentialNamespace: map[string]any{ConsumerWebPSIDCiphertextKey: "enc:psid", ConsumerWebPSIDTSCiphertextKey: "enc:psidts"}}
}

func TestConsumerWebBootstrapMissingSNlM0eDoesNotCallOTAQ(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, `"qKIAYe":"p" "cfb2h":"b"`)}}
	_, err := consumerWebTestTransport(u).Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
	if !errors.Is(err, ErrConsumerWebAPPTokenMissing) || len(u.requests) != 1 {
		t.Fatalf("missing auth token must stop before otaq")
	}
}

func TestConsumerWebTransportCloseStopsRequests(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, "")}}
	tx := consumerWebTestTransport(u)
	h := &consumerWebHTTPHandle{jar: mustCookieJar(), endpoints: tx.endpoints}
	if err := tx.Close(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.do(context.Background(), consumerWebTestAccount(), h, http.MethodGet, tx.endpoints.app, nil, ""); !errors.Is(err, ErrConsumerWebTransportClosed) || len(u.requests) != 0 {
		t.Fatal("closed handle made request")
	}
}

func TestConsumerWebHTTPErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		code   int
		target error
	}{{401, ErrConsumerWebAuthLost}, {403, ErrConsumerWebAuthLost}, {429, ErrConsumerWebRateLimited}, {500, ErrConsumerWebTransportTransient}, {502, ErrConsumerWebTransportTransient}, {503, ErrConsumerWebTransportTransient}} {
		if !errors.Is(mapConsumerWebStatus(tc.code), tc.target) {
			t.Fatalf("status %d classification mismatch", tc.code)
		}
	}
	if !errors.Is(mapConsumerWebStatus(200), ErrConsumerWebProtocolDrift) {
		t.Fatal("unexpected 200 classification")
	}
}

func TestConsumerWebRotateRequiresReplacementPSIDTS(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, "", &http.Cookie{Name: "__Secure-1PSIDTS", Value: "replacement"})}}
	tx := consumerWebTestTransport(u)
	h := &consumerWebHTTPHandle{jar: mustCookieJar(), endpoints: tx.endpoints}
	res, err := tx.RotatePSIDTS(context.Background(), consumerWebTestAccount(), h)
	if err != nil || res.PSIDTS != "replacement" {
		t.Fatalf("rotation failed: %v", err)
	}
	if got := h.jar.Cookies(mustURL(tx.endpoints.app)); len(got) != 1 || got[0].Value != "replacement" {
		t.Fatal("replacement not retained in session jar")
	}
	u.responses = []*http.Response{consumerWebResp(200, "", &http.Cookie{Name: "PSIDCC", Value: "only"})}
	if _, err := tx.RotatePSIDTS(context.Background(), consumerWebTestAccount(), h); !errors.Is(err, ErrConsumerWebProtocolDrift) {
		t.Fatal("PSIDCC-only rotation accepted")
	}
}

func TestConsumerWebPreAuthRotateUsesDurableCookiesAndAccountProxy(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, "")}}
	tx := consumerWebTestTransport(u)
	acct := consumerWebTestAccount()
	acct.Proxy = &Proxy{Protocol: "http", Host: "proxy.test", Port: 8080, Status: StatusActive}
	result, err := tx.PreAuthRotatePSIDTS(context.Background(), acct, consumerWebTestCreds())
	if err != nil || !result.Authenticated || result.PSIDTS != "" || len(u.requests) != 1 {
		t.Fatalf("preauth no-replacement rotation: %v", err)
	}
	if u.requests[0].URL.String() != tx.endpoints.rotate || u.requests[0].Method != http.MethodPost || u.proxies[0] != acct.Proxy.URL() {
		t.Fatal("preauth rotation changed endpoint, method, or account proxy")
	}
	for _, name := range []string{"__Secure-1PSID", "__Secure-1PSIDTS"} {
		if _, err := u.requests[0].Cookie(name); err != nil {
			t.Fatalf("durable cookie %s absent from Rotate request", name)
		}
	}
}

func TestConsumerWebPreAuthRotateCrossAccountIsolation(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, ""), consumerWebResp(200, "")}}
	tx := consumerWebTestTransport(u)
	a := consumerWebTestAccount()
	a.ID = 1
	a.Proxy = &Proxy{Protocol: "http", Host: "proxy-a.test", Port: 8080, Status: StatusActive}
	b := consumerWebTestAccount()
	b.ID = 2
	b.Proxy = &Proxy{Protocol: "http", Host: "proxy-b.test", Port: 8080, Status: StatusActive}
	ac := map[string]any{ConsumerWebCredentialNamespace: map[string]any{ConsumerWebPSIDCiphertextKey: "enc:account-a", ConsumerWebPSIDTSCiphertextKey: "enc:ts-a"}}
	bc := map[string]any{ConsumerWebCredentialNamespace: map[string]any{ConsumerWebPSIDCiphertextKey: "enc:account-b", ConsumerWebPSIDTSCiphertextKey: "enc:ts-b"}}
	if _, err := tx.PreAuthRotatePSIDTS(context.Background(), a, ac); err != nil {
		t.Fatal("first rotation failed")
	}
	if _, err := tx.PreAuthRotatePSIDTS(context.Background(), b, bc); err != nil {
		t.Fatal("second rotation failed")
	}
	if len(u.requests) != 2 || u.proxies[0] != a.Proxy.URL() || u.proxies[1] != b.Proxy.URL() || u.proxies[0] == u.proxies[1] {
		t.Fatal("proxy affinity/isolation failed")
	}
	ca, ea := u.requests[0].Cookie("__Secure-1PSID")
	cb, eb := u.requests[1].Cookie("__Secure-1PSID")
	if ea != nil || eb != nil || ca.Value != "account-a" || cb.Value != "account-b" {
		t.Fatal("cookie isolation failed")
	}
}

func TestConsumerWebOTAQFailuresDoNotTriggerAPPRecovery(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"drift", ")]}'\n[\"wrb.fr\",\"otAQ7b\",\"not-json\"]\n"},
		{"unauthenticated", consumerWebFullEnvelopeBody(float64(1016))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, `"SNlM0e":"fixture-token"`), consumerWebResp(200, tc.body)}}
			_, err := consumerWebTestTransport(u).Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
			if err == nil || errors.Is(err, ErrConsumerWebAPPTokenMissing) || len(u.requests) != 2 {
				t.Fatal("otAQ failure was misclassified as APP token missing")
			}
		})
	}
}

func TestConsumerWebPreAuthRecoveryErrorDoesNotExposeCookies(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(403, "")}}
	tx := consumerWebTestTransport(u)
	creds := map[string]any{ConsumerWebCredentialNamespace: map[string]any{
		ConsumerWebPSIDCiphertextKey:   "enc:synthetic-private-psid-marker",
		ConsumerWebPSIDTSCiphertextKey: "enc:synthetic-private-psidts-marker",
	}}
	_, err := tx.PreAuthRotatePSIDTS(context.Background(), consumerWebTestAccount(), creds)
	if err == nil || strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "Cookie") {
		t.Fatal("secret-bearing recovery error")
	}
}

func mustCookieJar() http.CookieJar { j, _ := cookiejar.New(nil); return j }

func TestConsumerWebTransportUsesConfiguredProxy(t *testing.T) {
	u := &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, `"SNlM0e":"token"`), consumerWebResp(200, func() string {
		in := make([]any, 16)
		in[14] = nil
		b, _ := json.Marshal(in)
		f, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
		return string(f)
	}())}}
	tx := consumerWebTestTransport(u)
	proxy, _ := url.Parse("http://proxy.test:8080")
	acct := consumerWebTestAccount()
	acct.Proxy = &Proxy{Protocol: proxy.Scheme, Host: proxy.Hostname(), Port: 8080, Status: StatusActive}
	if _, err := tx.Bootstrap(context.Background(), acct, consumerWebTestCreds(), nil); err != nil || len(u.requests) != 2 || u.proxies[0] == "" || u.proxies[1] == "" {
		t.Fatalf("proxy transport failed: %v", err)
	}
}

func TestConsumerWebTransportCookieIsolation(t *testing.T) {
	makeUpstream := func(marker string) *consumerWebTestUpstream {
		in := make([]any, 16)
		in[14] = nil
		b, _ := json.Marshal(in)
		f, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
		return &consumerWebTestUpstream{responses: []*http.Response{consumerWebResp(200, `"SNlM0e":"token"`, &http.Cookie{Name: "runtime", Value: marker, Path: "/"}), consumerWebResp(200, string(f))}}
	}
	a, b := makeUpstream("A"), makeUpstream("B")
	_, errA := consumerWebTestTransport(a).Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
	_, errB := consumerWebTestTransport(b).Bootstrap(context.Background(), consumerWebTestAccount(), consumerWebTestCreds(), nil)
	if errA != nil || errB != nil || len(a.requests) != 2 || len(b.requests) != 2 {
		t.Fatal("isolated bootstrap failed")
	}
	if strings.Contains(a.requests[1].Header.Get("Cookie"), "B") || strings.Contains(b.requests[1].Header.Get("Cookie"), "A") {
		t.Fatal("cross-session cookie leak")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
