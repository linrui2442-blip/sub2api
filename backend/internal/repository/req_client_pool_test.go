package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func forceHTTPVersion(t *testing.T, client *req.Client) string {
	t.Helper()
	transport := client.GetTransport()
	field := reflect.ValueOf(transport).Elem().FieldByName("forceHttpVersion")
	require.True(t, field.IsValid(), "forceHttpVersion field not found")
	require.True(t, field.CanAddr(), "forceHttpVersion field not addressable")
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().String()
}

func TestGetSharedReqClient_ForceHTTP2SeparatesCache(t *testing.T) {
	sharedReqClients = sync.Map{}
	base := reqClientOptions{
		ProxyURL: "http://proxy.local:8080",
		Timeout:  time.Second,
	}
	clientDefault, err := getSharedReqClient(base)
	require.NoError(t, err)

	force := base
	force.ForceHTTP2 = true
	clientForce, err := getSharedReqClient(force)
	require.NoError(t, err)

	require.NotSame(t, clientDefault, clientForce)
	require.NotEqual(t, buildReqClientKey(base), buildReqClientKey(force))
}

func TestGetSharedReqClient_ReuseCachedClient(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "http://proxy.local:8080",
		Timeout:  2 * time.Second,
	}
	first, err := getSharedReqClient(opts)
	require.NoError(t, err)
	second, err := getSharedReqClient(opts)
	require.NoError(t, err)
	require.Same(t, first, second)
}

func TestGetSharedReqClient_IgnoresNonClientCache(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: " http://proxy.local:8080 ",
		Timeout:  3 * time.Second,
	}
	key := buildReqClientKey(opts)
	sharedReqClients.Store(key, "invalid")

	client, err := getSharedReqClient(opts)
	require.NoError(t, err)

	require.NotNil(t, client)
	loaded, ok := sharedReqClients.Load(key)
	require.True(t, ok)
	require.IsType(t, "invalid", loaded)
}

func TestGetSharedReqClient_ImpersonateAndProxy(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL:    "  http://proxy.local:8080  ",
		Timeout:     4 * time.Second,
		Impersonate: true,
	}
	client, err := getSharedReqClient(opts)
	require.NoError(t, err)

	require.NotNil(t, client)
	require.Equal(t, "http://proxy.local:8080|4s|true|false", buildReqClientKey(opts))
}

func TestGetSharedReqClient_InvalidProxyURL(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "://missing-scheme",
		Timeout:  time.Second,
	}
	_, err := getSharedReqClient(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid proxy URL")
}

func TestGetSharedReqClient_ProxyURLMissingHost(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "http://",
		Timeout:  time.Second,
	}
	_, err := getSharedReqClient(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy URL missing host")
}

func TestCreateOpenAIReqClient_Timeout120Seconds(t *testing.T) {
	sharedReqClients = sync.Map{}
	client, err := createOpenAIReqClient("http://proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, client.GetClient().Timeout)
}

func TestCreateGeminiReqClient_ForceHTTP2Disabled(t *testing.T) {
	sharedReqClients = sync.Map{}
	client, err := createGeminiReqClient("http://proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, "", forceHTTPVersion(t, client))
}

func clearProviderProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(name, "")
	}
}

func proxyForTestRequest(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	proxy, err := providerAuthProxyFromEnvironment(req)
	require.NoError(t, err)
	return proxy
}

func TestProviderAuthProxyFromEnvironment(t *testing.T) {
	t.Run("direct without proxy", func(t *testing.T) {
		clearProviderProxyEnvironment(t)
		require.Nil(t, proxyForTestRequest(t, "https://oauth.example.test/token"))
	})
	t.Run("HTTP_PROXY", func(t *testing.T) {
		clearProviderProxyEnvironment(t)
		t.Setenv("HTTP_PROXY", "http://http-proxy.test:8080")
		require.Equal(t, "http://http-proxy.test:8080", proxyForTestRequest(t, "http://provider.test/token").String())
	})
	t.Run("HTTPS_PROXY", func(t *testing.T) {
		clearProviderProxyEnvironment(t)
		t.Setenv("HTTPS_PROXY", "http://https-proxy.test:8080")
		require.Equal(t, "http://https-proxy.test:8080", proxyForTestRequest(t, "https://provider.test/token").String())
	})
	t.Run("ALL_PROXY fallback", func(t *testing.T) {
		clearProviderProxyEnvironment(t)
		t.Setenv("ALL_PROXY", "socks5://all-proxy.test:1080")
		require.Equal(t, "socks5://all-proxy.test:1080", proxyForTestRequest(t, "https://provider.test/token").String())
	})
	t.Run("NO_PROXY bypass", func(t *testing.T) {
		clearProviderProxyEnvironment(t)
		t.Setenv("HTTPS_PROXY", "http://https-proxy.test:8080")
		t.Setenv("NO_PROXY", "provider.test")
		require.Nil(t, proxyForTestRequest(t, "https://provider.test/token"))
	})
}

func TestProviderAuthClientsUseExplicitProxyAheadOfEnvironment(t *testing.T) {
	clearProviderProxyEnvironment(t)
	t.Setenv("HTTPS_PROXY", "http://environment-proxy.test:8080")
	sharedReqClients = sync.Map{}
	for name, factory := range map[string]func(string) (*req.Client, error){
		"openai": createOpenAIReqClient,
		"gemini": createGeminiReqClient,
		"claude": createReqClient,
	} {
		t.Run(name, func(t *testing.T) {
			client, err := factory("http://account-proxy.test:9090")
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPost, "https://provider.test/token", nil)
			require.NoError(t, err)
			proxy, err := client.GetTransport().Proxy(req)
			require.NoError(t, err)
			require.Equal(t, "http://account-proxy.test:9090", proxy.String())
		})
	}
}

func TestInstrumentReqClientRecordsDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	collector := servertiming.New(time.Now())
	ctx := servertiming.WithCollector(context.Background(), collector)
	client := instrumentReqClient(req.C())
	response, err := client.R().SetContext(ctx).Get(server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode)

	header := collector.HeaderValue(time.Now(), "bypass")
	require.True(t, strings.Contains(header, "dep_http;dur="), header)
}
