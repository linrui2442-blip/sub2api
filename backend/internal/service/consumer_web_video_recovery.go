package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// A conversation read never submits a prompt. It may only recover the turn
// identified by the accepted StreamGenerate response on the same account.
type consumerWebVideoRecoveryState string

const (
	consumerWebVideoRecoveryPending consumerWebVideoRecoveryState = "PENDING"
	consumerWebVideoRecoveryVideo   consumerWebVideoRecoveryState = "VIDEO"
	consumerWebVideoRecoveryNoVideo consumerWebVideoRecoveryState = "TERMINAL_NO_VIDEO"
	consumerWebVideoRecoveryRefusal consumerWebVideoRecoveryState = "TERMINAL_REFUSAL"
)

type consumerWebVideoRecoveryResult struct {
	State          consumerWebVideoRecoveryState
	TargetFound    bool
	CandidateCount int
	Videos         []consumerWebGeneratedVideo
}

// hNvQHb is the source-backed LIST_CONVERSATION_TURNS RPC. The response is a
// batchexecute frame whose [0] contains newest-first conversation turns.
func (t *ConsumerWebHTTPTransport) ReadVideoConversation(ctx context.Context, account *Account, session ConsumerWebSessionHandle, identity consumerWebVideoIdentity) (consumerWebVideoRecoveryResult, error) {
	pending := consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryPending}
	h, ok := session.(*consumerWebHTTPHandle)
	if !ok || h.closed.Load() || account == nil || identity.CID == "" || (identity.RID == "" && identity.RCID == "") {
		return pending, ErrConsumerWebProtocolDrift
	}
	payload, err := json.Marshal([]any{identity.CID, 10, nil, 1, []int{1}, []int{4}, nil, 1})
	if err != nil {
		return pending, ErrConsumerWebProtocolDrift
	}
	freq, err := json.Marshal([]any{[]any{[]any{"hNvQHb", string(payload), nil, "generic"}}})
	if err != nil {
		return pending, ErrConsumerWebProtocolDrift
	}
	form := url.Values{
		"rpcids": {"hNvQHb"}, "source-path": {"/app"}, "hl": {h.language},
		"_reqid": {strconv.FormatUint(h.reqID.Add(1), 10)}, "rt": {"c"},
		"f.req": {string(freq)}, "at": {h.snlm},
	}
	if h.build != "" {
		form.Set("bl", h.build)
	}
	if h.sid != "" {
		form.Set("f.sid", h.sid)
	}
	resp, err := t.do(ctx, account, h, http.MethodPost, h.endpoints.otaq, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded;charset=UTF-8")
	if err != nil {
		return pending, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pending, mapConsumerWebStatus(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil || len(body) > 8<<20 {
		return pending, ErrConsumerWebProtocolDrift
	}
	return parseConsumerWebVideoConversation(body, identity)
}

func parseConsumerWebVideoConversation(body []byte, identity consumerWebVideoIdentity) (consumerWebVideoRecoveryResult, error) {
	result := consumerWebVideoRecoveryResult{State: consumerWebVideoRecoveryPending}
	if identity.CID == "" || (identity.RID == "" && identity.RCID == "") {
		return result, ErrConsumerWebProtocolDrift
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ")]}'"))
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var root []any
		if json.Unmarshal([]byte(line), &root) != nil {
			continue
		}
		frames := make([][]any, 0)
		if len(root) > 0 {
			if name, ok := root[0].(string); ok && name == "wrb.fr" {
				frames = append(frames, root)
			} else {
				for _, raw := range root {
					if frame, ok := raw.([]any); ok {
						frames = append(frames, frame)
					}
				}
			}
		}
		for _, frame := range frames {
			if len(frame) < 3 || frame[0] != "wrb.fr" || frame[1] != "hNvQHb" {
				continue
			}
			payloadText, ok := frame[2].(string)
			if !ok || payloadText == "" {
				return result, nil
			}
			var payload []any
			if json.Unmarshal([]byte(payloadText), &payload) != nil {
				return result, ErrConsumerWebProtocolDrift
			}
			turns, ok := consumerWebListAt(payload, 0).([]any)
			if !ok {
				return result, nil
			}
			for _, rawTurn := range turns {
				rid, _ := consumerWebListAt(rawTurn, 0, 1).(string)
				if identity.RID != "" && rid != identity.RID {
					continue
				}
				candidates, ok := consumerWebListAt(rawTurn, 3, 0).([]any)
				if !ok || len(candidates) == 0 {
					if identity.RID != "" && rid == identity.RID {
						result.TargetFound = true
					}
					continue
				}
				for _, candidate := range candidates {
					rcid, _ := consumerWebListAt(candidate, 0).(string)
					if identity.RCID != "" && rcid != identity.RCID {
						continue
					}
					if identity.RID == "" && rcid == "" {
						continue
					}
					result.TargetFound = true
					result.CandidateCount++
					diagnostic := ConsumerWebVideoAttemptDiagnostic{}
					videos := consumerWebCandidateVideos(candidate, &diagnostic, make(map[string]struct{}))
					if len(videos) > 0 {
						result.Videos = videos
						result.State = consumerWebVideoRecoveryVideo
						return result, nil
					}
					if status, ok := consumerWebListAt(candidate, 8, 0).(float64); ok && status == 2 {
						result.State = consumerWebVideoRecoveryNoVideo
						return result, nil
					}
				}
			}
			return result, nil
		}
	}
	return result, ErrConsumerWebProtocolDrift
}
