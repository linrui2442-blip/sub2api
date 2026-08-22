package repository

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"

	"github.com/imroc/req/v3"
	"golang.org/x/net/http/httpproxy"
)

// reqClientOptions 定义 req 客户端的构建参数
type reqClientOptions struct {
	ProxyURL    string        // 代理 URL（支持 http/https/socks5）
	Timeout     time.Duration // 请求超时时间
	Impersonate bool          // 是否模拟 Chrome 浏览器指纹
	ForceHTTP2  bool          // 是否强制使用 HTTP/2
}

// sharedReqClients 存储按配置参数缓存的 req 客户端实例
//
// 性能优化说明：
// 原实现在每次 OAuth 刷新时都创建新的 req.Client：
// 1. claude_oauth_service.go: 每次刷新创建新客户端
// 2. openai_oauth_service.go: 每次刷新创建新客户端
// 3. gemini_oauth_client.go: 每次刷新创建新客户端
//
// 新实现使用 sync.Map 缓存客户端：
// 1. 相同配置（代理+超时+模拟设置）复用同一客户端
// 2. 复用底层连接池，减少 TLS 握手开销
// 3. LoadOrStore 保证并发安全，避免重复创建
var sharedReqClients sync.Map

// getSharedReqClient 获取共享的 req 客户端实例
// 性能优化：相同配置复用同一客户端，避免重复创建
func getSharedReqClient(opts reqClientOptions) (*req.Client, error) {
	key := buildReqClientKey(opts)
	if cached, ok := sharedReqClients.Load(key); ok {
		if c, ok := cached.(*req.Client); ok {
			return c, nil
		}
	}

	client := req.C().SetTimeout(opts.Timeout)
	if opts.ForceHTTP2 {
		client = client.EnableForceHTTP2()
	}
	if opts.Impersonate {
		client = client.ImpersonateChrome()
	}
	trimmed, _, err := proxyurl.Parse(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
		slog.Info("provider auth proxy resolved", "proxy_source", "account", "proxy_enabled", true)
	} else {
		// OAuth account creation happens before an account-specific proxy can
		// exist. Resolve the standard environment on every request so Personal
		// runtime changes to HTTP(S)_PROXY/NO_PROXY take effect without creating
		// another provider-specific transport. ALL_PROXY is the final fallback.
		client.GetTransport().SetProxy(providerAuthProxy)
	}
	client = instrumentReqClient(client)

	actual, _ := sharedReqClients.LoadOrStore(key, client)
	if c, ok := actual.(*req.Client); ok {
		return c, nil
	}
	return client, nil
}

func providerAuthProxyFromEnvironment(req *http.Request) (*url.URL, error) {
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

type providerProxyDecision struct {
	URL    *url.URL
	Source string
}

var systemProxyResolver = windowsSystemProxy

// providerAuthProxy resolves the Personal authentication egress policy. An
// explicit account proxy is installed by getSharedReqClient before this
// function is used. Personal Edition currently has no separate default proxy,
// so Windows system settings precede the standard environment fallback.
func providerAuthProxy(req *http.Request) (*url.URL, error) {
	decision, err := resolveProviderAuthProxy(req)
	if err == nil {
		slog.Info("provider auth proxy resolved", "proxy_source", decision.Source, "proxy_enabled", decision.URL != nil)
	}
	return decision.URL, err
}

func resolveProviderAuthProxy(req *http.Request) (providerProxyDecision, error) {
	if req == nil || req.URL == nil {
		return providerProxyDecision{Source: "direct"}, nil
	}
	if proxy, ok := systemProxyResolver(req.URL); ok {
		return providerProxyDecision{URL: proxy, Source: "windows_system"}, nil
	}
	proxy, err := providerAuthProxyFromEnvironment(req)
	if err != nil {
		return providerProxyDecision{}, err
	}
	if proxy != nil {
		return providerProxyDecision{URL: proxy, Source: "environment"}, nil
	}
	return providerProxyDecision{Source: "direct"}, nil
}

type windowsProxyConfig struct {
	Enabled  bool
	Server   string
	Override string
}

func resolveWindowsProxyConfig(target *url.URL, cfg windowsProxyConfig) (*url.URL, bool) {
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

func instrumentReqClient(client *req.Client) *req.Client {
	if client == nil {
		return nil
	}
	client.GetTransport().WrapRoundTripFunc(func(rt http.RoundTripper) req.HttpRoundTripFunc {
		timed := servertiming.WrapRoundTripper(rt)
		return timed.RoundTrip
	})
	return client
}

func buildReqClientKey(opts reqClientOptions) string {
	return fmt.Sprintf("%s|%s|%t|%t",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.Impersonate,
		opts.ForceHTTP2,
	)
}

// CreatePrivacyReqClient creates an HTTP client for OpenAI privacy settings API
// This is exported for use by OpenAIPrivacyService
// Uses Chrome TLS fingerprint impersonation to bypass Cloudflare checks
func CreatePrivacyReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL:    proxyURL,
		Timeout:     30 * time.Second,
		Impersonate: true, // Enable Chrome TLS fingerprint impersonation
	})
}
