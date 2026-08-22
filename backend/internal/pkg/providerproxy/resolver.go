package providerproxy

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

type Decision struct {
	URL    *url.URL
	Source string
}

type WindowsProxyConfig struct {
	Enabled  bool
	Server   string
	Override string
}

var systemProxyResolver = WindowsSystemProxy

// Proxy resolves provider authentication egress in Personal priority order:
// Windows system proxy, standard proxy environment, then direct.
func Proxy(req *http.Request) (*url.URL, error) {
	decision, err := Resolve(req)
	if err == nil {
		slog.Info("provider auth proxy resolved", "proxy_source", decision.Source, "proxy_enabled", decision.URL != nil)
	}
	return decision.URL, err
}

func Resolve(req *http.Request) (Decision, error) {
	if req == nil || req.URL == nil {
		return Decision{Source: "direct"}, nil
	}
	if proxy, ok := systemProxyResolver(req.URL); ok {
		return Decision{URL: proxy, Source: "windows_system"}, nil
	}
	proxy, err := FromEnvironment(req)
	if err != nil {
		return Decision{}, err
	}
	if proxy != nil {
		return Decision{URL: proxy, Source: "environment"}, nil
	}
	return Decision{Source: "direct"}, nil
}

func FromEnvironment(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}
	cfg := httpproxy.FromEnvironment()
	if strings.TrimSpace(cfg.HTTPProxy) == "" && strings.TrimSpace(cfg.HTTPSProxy) == "" {
		allProxy := strings.TrimSpace(os.Getenv("ALL_PROXY"))
		if allProxy == "" {
			allProxy = strings.TrimSpace(os.Getenv("all_proxy"))
		}
		if allProxy != "" {
			cfg.HTTPProxy = allProxy
			cfg.HTTPSProxy = allProxy
		}
	}
	return cfg.ProxyFunc()(req.URL)
}

func ResolveWindowsProxyConfig(target *url.URL, cfg WindowsProxyConfig) (*url.URL, bool) {
	if target == nil || !cfg.Enabled || windowsProxyBypassed(target.Hostname(), cfg.Override) {
		return nil, false
	}
	raw := selectWindowsProxyServer(target.Scheme, cfg.Server)
	if raw == "" {
		return nil, false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, false
	}
	return parsed, true
}

func selectWindowsProxyServer(scheme, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return ""
	}
	if !strings.Contains(configured, "=") {
		return configured
	}
	values := make(map[string]string)
	for _, part := range strings.Split(configured, ";") {
		key, value, ok := strings.Cut(part, "=")
		if ok {
			values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	return values[strings.ToLower(strings.TrimSpace(scheme))]
}

func windowsProxyBypassed(host, configured string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	for _, part := range strings.Split(configured, ";") {
		pattern := strings.ToLower(strings.TrimSpace(part))
		if pattern == "" {
			continue
		}
		if pattern == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if matched, _ := filepath.Match(pattern, host); matched || pattern == host {
			return true
		}
	}
	return false
}
