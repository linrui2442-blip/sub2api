package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/imroc/req/v3"
)

type geminiCliCodeAssistClient struct {
	baseURL string
}

func NewGeminiCliCodeAssistClient() service.GeminiCliCodeAssistClient {
	return &geminiCliCodeAssistClient{baseURL: geminicli.GeminiCliBaseURL}
}

func (c *geminiCliCodeAssistClient) LoadCodeAssist(ctx context.Context, accessToken, proxyURL string, reqBody *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
	if reqBody == nil {
		reqBody = defaultLoadCodeAssistRequest()
	}

	var out geminicli.LoadCodeAssistResponse
	client, err := createGeminiCliReqClient(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", geminicli.GeminiCLIUserAgent).
		SetBody(reqBody).
		SetSuccessResult(&out).
		Post(c.baseURL + "/v1internal:loadCodeAssist")
	if err != nil {
		fmt.Printf("[CodeAssist] LoadCodeAssist request error: %v\n", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if !resp.IsSuccessState() {
		body := resp.String()
		sanitizedBody := geminicli.SanitizeBodyForLogs(body)
		fmt.Printf("[CodeAssist] LoadCodeAssist failed: status %d, body: %s\n", resp.StatusCode, sanitizedBody)

		// Check if this is a SERVICE_DISABLED error and extract activation URL
		if googleapi.IsServiceDisabledError(body) {
			activationURL := googleapi.ExtractActivationURL(body)
			if activationURL != "" {
				return nil, fmt.Errorf("gemini API not enabled for this project, please enable it by visiting: %s\n\nAfter enabling the API, wait a few minutes for the changes to propagate, then try again", activationURL)
			}
			return nil, fmt.Errorf("gemini API not enabled for this project, please enable it in the Google Cloud Console at: https://console.cloud.google.com/apis/library/cloudaicompanion.googleapis.com")
		}

		return nil, fmt.Errorf("loadCodeAssist failed: status %d, body: %s", resp.StatusCode, sanitizedBody)
	}
	fmt.Printf("[CodeAssist] LoadCodeAssist success: status %d, response: %+v\n", resp.StatusCode, out)
	return &out, nil
}

func (c *geminiCliCodeAssistClient) OnboardUser(ctx context.Context, accessToken, proxyURL string, reqBody *geminicli.OnboardUserRequest) (*geminicli.OnboardUserResponse, error) {
	if reqBody == nil {
		reqBody = defaultOnboardUserRequest()
	}

	fmt.Printf("[CodeAssist] OnboardUser request body: %+v\n", reqBody)

	var out geminicli.OnboardUserResponse
	client, err := createGeminiCliReqClient(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", geminicli.GeminiCLIUserAgent).
		SetBody(reqBody).
		SetSuccessResult(&out).
		Post(c.baseURL + "/v1internal:onboardUser")
	if err != nil {
		fmt.Printf("[CodeAssist] OnboardUser request error: %v\n", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if !resp.IsSuccessState() {
		body := resp.String()
		sanitizedBody := geminicli.SanitizeBodyForLogs(body)
		fmt.Printf("[CodeAssist] OnboardUser failed: status %d, body: %s\n", resp.StatusCode, sanitizedBody)

		// Check if this is a SERVICE_DISABLED error and extract activation URL
		if googleapi.IsServiceDisabledError(body) {
			activationURL := googleapi.ExtractActivationURL(body)
			if activationURL != "" {
				return nil, fmt.Errorf("gemini API not enabled for this project, please enable it by visiting: %s\n\nAfter enabling the API, wait a few minutes for the changes to propagate, then try again", activationURL)
			}
			return nil, fmt.Errorf("gemini API not enabled for this project, please enable it in the Google Cloud Console at: https://console.cloud.google.com/apis/library/cloudaicompanion.googleapis.com")
		}

		return nil, fmt.Errorf("onboardUser failed: status %d, body: %s", resp.StatusCode, sanitizedBody)
	}
	fmt.Printf("[CodeAssist] OnboardUser success: status %d, response: %+v\n", resp.StatusCode, out)
	return &out, nil
}

func (c *geminiCliCodeAssistClient) GetOperation(ctx context.Context, accessToken, proxyURL, name string) (*geminicli.OnboardUserResponse, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("operation name is required")
	}
	if strings.Contains(name, "://") || strings.Contains(name, "..") {
		return nil, errors.New("invalid operation name")
	}

	var out geminicli.OnboardUserResponse
	client, err := createGeminiCliReqClient(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", geminicli.GeminiCLIUserAgent).
		SetSuccessResult(&out).
		Get(c.baseURL + "/v1internal/" + strings.TrimLeft(name, "/"))
	if err != nil {
		return nil, fmt.Errorf("get operation request failed: %w", err)
	}
	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("get operation failed: status %d, body: %s", resp.StatusCode, geminicli.SanitizeBodyForLogs(resp.String()))
	}
	return &out, nil
}

func createGeminiCliReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL: proxyURL,
		Timeout:  30 * time.Second,
	})
}

func defaultLoadCodeAssistRequest() *geminicli.LoadCodeAssistRequest {
	return &geminicli.LoadCodeAssistRequest{
		Metadata: geminicli.LoadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}
}

func defaultOnboardUserRequest() *geminicli.OnboardUserRequest {
	return &geminicli.OnboardUserRequest{
		TierID: "LEGACY",
		Metadata: geminicli.LoadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}
}
