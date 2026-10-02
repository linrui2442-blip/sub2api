package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	consumerWebGenerateURL = "https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"
	consumerWebUploadURL   = "https://content-push.googleapis.com/upload"
	consumerWebVideoLimit  = 200 << 20
)

type ConsumerWebVideoUpload struct {
	Name string
	MIME string
	Data []byte
}

type consumerWebUploadedFile struct{ ID, Name string }

// ConsumerWebVideoUploadError marks a failure before StreamGenerate. It does
// not imply the provider accepted a video task or established a conversation.
type ConsumerWebVideoUploadError struct{ Err error }

type consumerWebVideoAttemptDiagnosticKey struct{}

func (e *ConsumerWebVideoUploadError) Error() string { return "consumer web video upload failed" }
func (e *ConsumerWebVideoUploadError) Unwrap() error { return e.Err }

func consumerWebVideoRequestID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strings.ToUpper(hex.EncodeToString(data[:]))
}

// videoRequest stays on the existing account proxy and authenticated in-memory
// cookie jar. It intentionally does not reuse the short APP/otAQ timeout.
func (t *ConsumerWebHTTPTransport) videoRequest(ctx context.Context, account *Account, h *consumerWebHTTPHandle, method, raw string, body io.Reader, contentType string) (*http.Response, error) {
	if t == nil || t.upstream == nil || account == nil || h == nil || h.closed.Load() {
		return nil, ErrConsumerWebTransportClosed
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return nil, ErrConsumerWebProtocolDrift
	}
	host := strings.ToLower(u.Hostname())
	if host != "gemini.google.com" && host != "content-push.googleapis.com" && !consumerWebTrustedVideoURL(raw) {
		return nil, ErrConsumerWebProtocolDrift
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, ErrConsumerWebProtocolDrift
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://gemini.google.com/")
	if method == http.MethodPost {
		req.Header.Set("Origin", "https://gemini.google.com")
	}
	if host == "gemini.google.com" && method == http.MethodPost {
		req.Header.Set("X-Same-Domain", "1")
	}
	if host == "content-push.googleapis.com" {
		req.Header.Set("X-Tenant-Id", "bard-storage")
		push := h.push
		if push == "" {
			push = "feeds/mcudyrk2a4khkz"
		}
		req.Header.Set("Push-ID", push)
	}
	if metadata, ok := ctx.Value(consumerWebVideoHeaderKey{}).(consumerWebVideoHeaders); ok && host == "gemini.google.com" {
		req.Header.Set("x-goog-ext-525001261-jspb", metadata.extra)
		if metadata.hasUpload {
			req.Header.Set("x-goog-ext-525005358-jspb", fmt.Sprintf(`["%s",1]`, metadata.requestID))
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// The frozen upload/download source sends the authenticated Gemini cookies
	// explicitly even when the destination uses another trusted Google host.
	geminiURL, _ := url.Parse(consumerWebAppURL)
	for _, cookie := range h.jar.Cookies(geminiURL) {
		req.AddCookie(cookie)
	}
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	policy := func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || next == nil || next.URL == nil || !strings.EqualFold(next.URL.Scheme, "https") {
			return ErrConsumerWebRedirectRejected
		}
		nextHost := strings.ToLower(next.URL.Hostname())
		if nextHost != "gemini.google.com" && nextHost != "content-push.googleapis.com" && !consumerWebTrustedVideoURL(next.URL.String()) {
			return ErrConsumerWebRedirectRejected
		}
		if len(via) > 0 {
			next.Header.Del("Cookie")
			for _, cookie := range h.jar.Cookies(geminiURL) {
				next.AddCookie(cookie)
			}
		}
		return nil
	}
	redirector, ok := t.upstream.(HTTPUpstreamRedirector)
	if !ok {
		return nil, ErrConsumerWebRedirectRejected
	}
	if diagnostic, ok := ctx.Value(consumerWebVideoAttemptDiagnosticKey{}).(*ConsumerWebVideoAttemptDiagnostic); ok {
		diagnostic.Sent = true
	}
	resp, err := redirector.DoWithRedirectPolicy(req, proxy, account.ID, account.Concurrency, policy)
	if err != nil || resp == nil || resp.Body == nil {
		return nil, ErrConsumerWebTransportTransient
	}
	return resp, nil
}

func consumerWebVideoGenerateInner(prompt, model, language, requestID string, file *consumerWebUploadedFile) []any {
	message := []any{prompt}
	if file != nil {
		message = []any{prompt, 0, nil, []any{[]any{[]any{file.ID}, file.Name}}, nil, nil, 0}
	}
	inner := make([]any, 69)
	inner[0] = message
	inner[1] = []any{language}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[3] = model
	inner[6] = []any{1}
	inner[7], inner[10], inner[11] = 1, 1, 0
	inner[17], inner[18], inner[27], inner[30] = []any{[]any{0}}, 0, 1, []any{4}
	inner[41], inner[53], inner[59], inner[61], inner[68] = []any{1}, 0, requestID, []any{}, 2
	return inner
}

func (t *ConsumerWebHTTPTransport) uploadVideoInput(ctx context.Context, account *Account, h *consumerWebHTTPHandle, file ConsumerWebVideoUpload) (*consumerWebUploadedFile, error) {
	if len(file.Data) == 0 || len(file.Data) > 20<<20 || !strings.HasPrefix(file.MIME, "image/") {
		return nil, ErrConsumerWebProtocolDrift
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = "image"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="image"`)
	header.Set("Content-Type", file.MIME)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, ErrConsumerWebProtocolDrift
	}
	if _, err = part.Write(file.Data); err != nil {
		return nil, ErrConsumerWebProtocolDrift
	}
	if err = writer.Close(); err != nil {
		return nil, ErrConsumerWebProtocolDrift
	}
	resp, err := t.videoRequest(ctx, account, h, http.MethodPost, consumerWebUploadURL, &body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapConsumerWebStatus(resp.StatusCode)
	}
	id, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(id) == 0 || len(id) > 4096 {
		return nil, ErrConsumerWebProtocolDrift
	}
	fileID := strings.TrimSpace(string(id))
	if fileID == "" {
		return nil, ErrConsumerWebProtocolDrift
	}
	return &consumerWebUploadedFile{ID: fileID, Name: name}, nil
}

func (t *ConsumerWebHTTPTransport) GenerateVideo(ctx context.Context, account *Account, session ConsumerWebSessionHandle, model, prompt string, image *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, error) {
	videos, _, err := t.GenerateVideoDetailed(ctx, account, session, model, prompt, image)
	return videos, err
}

// GenerateVideoDetailed follows the same wire path as GenerateVideo while
// returning bounded stage metadata for the task worker.
func (t *ConsumerWebHTTPTransport) GenerateVideoDetailed(ctx context.Context, account *Account, session ConsumerWebSessionHandle, model, prompt string, image *ConsumerWebVideoUpload) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, error) {
	d := ConsumerWebVideoAttemptDiagnostic{Cause: ConsumerWebVideoFailurePrepare, TextLengthBucket: "NONE"}
	h, ok := session.(*consumerWebHTTPHandle)
	if !ok || h.closed.Load() || strings.TrimSpace(model) == "" || strings.TrimSpace(prompt) == "" {
		return nil, d, ErrConsumerWebProtocolDrift
	}
	ctx, cancel := context.WithTimeout(ctx, 9*time.Minute)
	defer cancel()
	var uploaded *consumerWebUploadedFile
	var err error
	if image != nil {
		slog.Info("consumer_web_video_upload_attempt", "account_id", account.ID)
		uploaded, err = t.uploadVideoInput(ctx, account, h, *image)
		if err != nil {
			return nil, d, &ConsumerWebVideoUploadError{Err: err}
		}
	}
	requestID := consumerWebVideoRequestID()
	inner, err := json.Marshal(consumerWebVideoGenerateInner(prompt, model, h.language, requestID, uploaded))
	if err != nil {
		d.Cause = ConsumerWebVideoFailureRequestBuild
		return nil, d, ErrConsumerWebProtocolDrift
	}
	outer, err := json.Marshal([]any{nil, string(inner)})
	if err != nil {
		d.Cause = ConsumerWebVideoFailureRequestBuild
		return nil, d, ErrConsumerWebProtocolDrift
	}
	form := url.Values{"at": {h.snlm}, "f.req": {string(outer)}}
	q := url.Values{"at": {h.snlm}}
	if uploaded != nil {
		q.Set("hl", h.language)
		q.Set("rt", "c")
		q.Set("_reqid", strconv.FormatUint(h.reqID.Add(1), 10))
		if h.build != "" {
			q.Set("bl", h.build)
		}
		if h.sid != "" {
			q.Set("f.sid", h.sid)
		}
	}
	// Source-aligned request metadata is injected by the request-scoped helper;
	// no account identity or credential is placed in the metadata header.
	generateRaw := consumerWebGenerateURL + "?" + q.Encode()
	d.RequestBuilt = true
	slog.Info("consumer_web_video_streamgenerate_attempt", "account_id", account.ID)
	ctx = context.WithValue(ctx, consumerWebVideoAttemptDiagnosticKey{}, &d)
	resp, err := t.videoGenerateRequest(ctx, account, h, generateRaw, form.Encode(), requestID, uploaded != nil)
	if err != nil {
		if d.Sent {
			d.Cause = ConsumerWebVideoFailureTransport
		} else {
			d.Cause = ConsumerWebVideoFailureRequestBuild
		}
		return nil, d, err
	}
	defer resp.Body.Close()
	d.ResponseReceived = true
	d.HTTPStatus = resp.StatusCode
	slog.Info("consumer_web_video_streamgenerate_response", "account_id", account.ID, "status", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		d.Cause = ConsumerWebVideoFailureHTTP
		return nil, d, mapConsumerWebStatus(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil || len(body) > 8<<20 {
		d.Cause = ConsumerWebVideoFailureBodyRead
		return nil, d, ErrConsumerWebProtocolDrift
	}
	parsed, parsedDiagnostic, err := parseConsumerWebGeneratedVideosDetailed(body)
	parsedDiagnostic.RequestBuilt, parsedDiagnostic.Sent = d.RequestBuilt, d.Sent
	parsedDiagnostic.ResponseReceived, parsedDiagnostic.HTTPStatus = d.ResponseReceived, d.HTTPStatus
	return parsed, parsedDiagnostic, err
}

func (t *ConsumerWebHTTPTransport) videoGenerateRequest(ctx context.Context, account *Account, h *consumerWebHTTPHandle, raw, form, requestID string, hasUpload bool) (*http.Response, error) {
	// The generic videoRequest owns proxy, cookies and redirect security. The
	// frozen source's supplemental header is carried via the explicit request
	// context only for this generation call.
	traceID := strings.ToLower(consumerWebVideoRequestID())[:16]
	extra := fmt.Sprintf(`[1,null,null,null,"%s",null,null,0,[4,5,6,8],null,null,2,null,null,6,1,"%s"]`, traceID, consumerWebVideoRequestID())
	ctx = context.WithValue(ctx, consumerWebVideoHeaderKey{}, consumerWebVideoHeaders{requestID: requestID, hasUpload: hasUpload, extra: extra})
	return t.videoRequest(ctx, account, h, http.MethodPost, raw, strings.NewReader(form), "application/x-www-form-urlencoded;charset=UTF-8")
}

type consumerWebVideoHeaderKey struct{}
type consumerWebVideoHeaders struct {
	requestID string
	hasUpload bool
	extra     string
}

func (t *ConsumerWebHTTPTransport) DownloadVideo(ctx context.Context, account *Account, session ConsumerWebSessionHandle, raw string) ([]byte, bool, error) {
	h, ok := session.(*consumerWebHTTPHandle)
	if !ok || h.closed.Load() || !consumerWebTrustedVideoURL(raw) {
		return nil, false, ErrConsumerWebProtocolDrift
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	resp, err := t.videoRequest(ctx, account, h, http.MethodGet, raw, nil, "")
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPartialContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, mapConsumerWebStatus(resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if !strings.HasPrefix(ct, "video/") && !strings.HasPrefix(ct, "application/octet-stream") {
		return nil, false, ErrConsumerWebProtocolDrift
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, consumerWebVideoLimit+1))
	if err != nil || len(body) > consumerWebVideoLimit {
		return nil, false, ErrConsumerWebProtocolDrift
	}
	if len(body) < 8 || string(body[4:8]) != "ftyp" {
		return nil, false, ErrConsumerWebProtocolDrift
	}
	return body, true, nil
}
