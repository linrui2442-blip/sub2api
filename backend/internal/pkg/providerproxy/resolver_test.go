package providerproxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(name, "")
	}
}

func TestResolvePriority(t *testing.T) {
	clearProxyEnvironment(t)
	t.Setenv("HTTPS_PROXY", "http://environment.test:8080")
	original := systemProxyResolver
	t.Cleanup(func() { systemProxyResolver = original })

	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", nil)
	require.NoError(t, err)
	systemProxyResolver = func(*url.URL) (*url.URL, bool) {
		proxy, parseErr := url.Parse("http://windows.test:9090")
		require.NoError(t, parseErr)
		return proxy, true
	}

	decision, err := Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "windows_system", decision.Source)
	require.Equal(t, "http://windows.test:9090", decision.URL.String())

	systemProxyResolver = func(*url.URL) (*url.URL, bool) { return nil, false }
	decision, err = Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "environment", decision.Source)
	require.Equal(t, "http://environment.test:8080", decision.URL.String())

	t.Setenv("HTTPS_PROXY", "")
	decision, err = Resolve(req)
	require.NoError(t, err)
	require.Equal(t, "direct", decision.Source)
	require.Nil(t, decision.URL)
}

func TestResolveWindowsProxyConfig(t *testing.T) {
	target, err := url.Parse("https://oauth2.googleapis.com/token")
	require.NoError(t, err)

	proxy, ok := ResolveWindowsProxyConfig(target, WindowsProxyConfig{
		Enabled: true,
		Server:  "http=proxy.test:80;https=secure.test:443",
	})
	require.True(t, ok)
	require.Equal(t, "http://secure.test:443", proxy.String())

	proxy, ok = ResolveWindowsProxyConfig(target, WindowsProxyConfig{
		Enabled:  true,
		Server:   "127.0.0.1:7890",
		Override: "oauth2.googleapis.com",
	})
	require.False(t, ok)
	require.Nil(t, proxy)
}
