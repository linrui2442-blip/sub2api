package service

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// consumerWebGeneratedVideo is internal metadata from a StreamGenerate result.
// The URL is credential-bearing in some responses and must never be logged.
type consumerWebGeneratedVideo struct {
	URL   string
	Title string
	MIME  string
}

// Provider identity may be persisted only inside encrypted internal task traces.
// It is never serialized into public task status or logs.
type consumerWebVideoIdentity struct{ CID, RID, RCID string }

// ConsumerWebVideoFailureCause contains only a bounded, secret-free stage.
type ConsumerWebVideoFailureCause string

const (
	ConsumerWebVideoFailureNone            ConsumerWebVideoFailureCause = "NONE"
	ConsumerWebVideoFailurePrepare         ConsumerWebVideoFailureCause = "PREPARE_FAILED"
	ConsumerWebVideoFailureModelResolution ConsumerWebVideoFailureCause = "MODEL_RESOLUTION_FAILED"
	ConsumerWebVideoFailureRequestBuild    ConsumerWebVideoFailureCause = "STREAMGENERATE_REQUEST_BUILD_FAILED"
	ConsumerWebVideoFailureTransport       ConsumerWebVideoFailureCause = "STREAMGENERATE_TRANSPORT_FAILED"
	ConsumerWebVideoFailureHTTP            ConsumerWebVideoFailureCause = "STREAMGENERATE_HTTP_FAILED"
	ConsumerWebVideoFailureBodyRead        ConsumerWebVideoFailureCause = "STREAMGENERATE_BODY_READ_FAILED"
	ConsumerWebVideoFailureParse           ConsumerWebVideoFailureCause = "STREAMGENERATE_PARSE_FAILED"
	ConsumerWebVideoFailureProviderBlock   ConsumerWebVideoFailureCause = "STREAMGENERATE_PROVIDER_ERROR_BLOCK"
	ConsumerWebVideoFailureNoVideo         ConsumerWebVideoFailureCause = "STREAMGENERATE_NO_VIDEO"
	ConsumerWebVideoAcceptedPending        ConsumerWebVideoFailureCause = "STREAMGENERATE_ACCEPTED_PENDING"
	ConsumerWebVideoRecoveryTimeout        ConsumerWebVideoFailureCause = "CONVERSATION_RECOVERY_TIMEOUT"
	ConsumerWebVideoRecoveryUnavailable    ConsumerWebVideoFailureCause = "CONVERSATION_RECOVERY_UNAVAILABLE"
	ConsumerWebVideoFailureSlotParse       ConsumerWebVideoFailureCause = "STREAMGENERATE_VIDEO_SLOT_PARSE_FAILED"
	ConsumerWebVideoFailureURLMissing      ConsumerWebVideoFailureCause = "STREAMGENERATE_VIDEO_URL_MISSING"
	ConsumerWebVideoFailureMediaNotReady   ConsumerWebVideoFailureCause = "MEDIA_NOT_READY"
	ConsumerWebVideoFailureMediaDownload   ConsumerWebVideoFailureCause = "MEDIA_DOWNLOAD_FAILED"
	ConsumerWebVideoFailureUnknown         ConsumerWebVideoFailureCause = "UNKNOWN"
)

// ConsumerWebVideoAttemptDiagnostic is safe to log. Provider identifiers and
// response text are represented only by presence/shape, never by their value.
type ConsumerWebVideoAttemptDiagnostic struct {
	VideoKey65Present                                             bool
	Cause                                                         ConsumerWebVideoFailureCause
	RequestBuilt, Sent, ResponseReceived                          bool
	HTTPStatus                                                    int
	BodyNonempty, ParsePass, ProviderErrorBlock                   bool
	ConversationIDPresent, ResponseIDPresent, ChoiceIDPresent     bool
	TextPresent                                                   bool
	TextLengthBucket                                              string
	CandidateCount                                                int
	MediaContainerPresent, VideoKey60Present, SlotRootShapeValid  bool
	VideoMetadataCount, VideoURLVariantCount, VideoExtractedCount int
}

func consumerWebTextLengthBucket(n int) string {
	switch {
	case n == 0:
		return "NONE"
	case n < 100:
		return "LT_100"
	case n < 1000:
		return "100_TO_999"
	default:
		return "GE_1000"
	}
}

var errConsumerWebVideoResponse = errors.New("consumer web generated video response is invalid")

func consumerWebTrustedVideoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "contribution.usercontent.google.com" || host == "lh3.googleusercontent.com" || strings.HasSuffix(host, ".googleusercontent.com")
}

func consumerWebVideoURL(raw any) string {
	urls, ok := raw.([]any)
	if !ok {
		return ""
	}
	first := ""
	for _, item := range urls {
		s, ok := item.(string)
		if !ok || !consumerWebTrustedVideoURL(s) {
			continue
		}
		if first == "" {
			first = s
		}
		if strings.EqualFold(mustParseHost(s), "contribution.usercontent.google.com") {
			return s
		}
	}
	return first
}

func mustParseHost(raw string) string {
	u, _ := url.Parse(raw)
	return u.Hostname()
}

func consumerWebListAt(value any, indexes ...int) any {
	for _, index := range indexes {
		list, ok := value.([]any)
		if !ok || index < 0 || index >= len(list) {
			return nil
		}
		value = list[index]
	}
	return value
}

// parseConsumerWebGeneratedVideos scans all StreamGenerate frames and media
// entries. The source-backed video slot is a map keyed "60" somewhere within
// candidate[12]; its position is not stable.
func parseConsumerWebGeneratedVideos(body []byte) ([]consumerWebGeneratedVideo, error) {
	videos, _, err := parseConsumerWebGeneratedVideosDetailed(body)
	return videos, err
}

func parseConsumerWebGeneratedVideosDetailed(body []byte) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, error) {
	videos, diagnostic, _, err := parseConsumerWebGeneratedVideoAttempt(body)
	return videos, diagnostic, err
}

func consumerWebCandidateVideos(rawCandidate any, diagnostic *ConsumerWebVideoAttemptDiagnostic, seen map[string]struct{}) []consumerWebGeneratedVideo {
	result := make([]consumerWebGeneratedVideo, 0)
	media, ok := consumerWebListAt(rawCandidate, 12).([]any)
	if !ok {
		return result
	}
	diagnostic.MediaContainerPresent = true
	for _, rawEntry := range media {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := entry["65"]; exists {
			diagnostic.VideoKey65Present = true
		}
		if _, exists := entry["60"]; !exists {
			continue
		}
		diagnostic.VideoKey60Present = true
		group, ok := consumerWebListAt(entry["60"], 0, 0, 0).([]any)
		if !ok {
			continue
		}
		diagnostic.SlotRootShapeValid = true
		for _, rawMeta := range group {
			diagnostic.VideoMetadataCount++
			meta, ok := rawMeta.([]any)
			if !ok || len(meta) <= 7 {
				continue
			}
			if urls, ok := meta[7].([]any); ok {
				diagnostic.VideoURLVariantCount += len(urls)
			}
			videoURL := consumerWebVideoURL(meta[7])
			if videoURL == "" {
				continue
			}
			if _, exists := seen[videoURL]; exists {
				continue
			}
			seen[videoURL] = struct{}{}
			video := consumerWebGeneratedVideo{URL: videoURL, MIME: "video/mp4"}
			if title, ok := meta[2].(string); ok {
				video.Title = title
			}
			if len(meta) > 11 {
				if mime, ok := meta[11].(string); ok && strings.HasPrefix(mime, "video/") {
					video.MIME = mime
				}
			}
			result = append(result, video)
		}
	}
	return result
}

func parseConsumerWebGeneratedVideoAttempt(body []byte) ([]consumerWebGeneratedVideo, ConsumerWebVideoAttemptDiagnostic, consumerWebVideoIdentity, error) {
	d := ConsumerWebVideoAttemptDiagnostic{Cause: ConsumerWebVideoFailureParse, BodyNonempty: len(body) > 0, TextLengthBucket: "NONE"}
	identity := consumerWebVideoIdentity{}
	seen := make(map[string]struct{})
	result := make([]consumerWebGeneratedVideo, 0)
	validFrame := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ")]}'"))
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var envelope []any
		if json.Unmarshal([]byte(line), &envelope) != nil {
			continue
		}
		for _, rawFrame := range envelope {
			frame, ok := rawFrame.([]any)
			if !ok || len(frame) < 3 {
				continue
			}
			payloadText, ok := frame[2].(string)
			if !ok {
				continue
			}
			var payload []any
			if json.Unmarshal([]byte(payloadText), &payload) != nil {
				continue
			}
			validFrame = true
			if ids, ok := consumerWebListAt(payload, 1).([]any); ok {
				if len(ids) > 0 {
					if id, ok := ids[0].(string); ok && id != "" {
						identity.CID = id
						d.ConversationIDPresent = true
					}
				}
				if len(ids) > 1 {
					if id, ok := ids[1].(string); ok && id != "" {
						identity.RID = id
						d.ResponseIDPresent = true
					}
				}
			}
			candidates, ok := consumerWebListAt(payload, 4).([]any)
			if !ok {
				continue
			}
			for _, rawCandidate := range candidates {
				d.CandidateCount++
				if choice, ok := consumerWebListAt(rawCandidate, 0).(string); ok && choice != "" {
					identity.RCID = choice
					d.ChoiceIDPresent = true
				}
				if textParts, ok := consumerWebListAt(rawCandidate, 1).([]any); ok {
					for _, rawText := range textParts {
						if text, ok := rawText.(string); ok && text != "" {
							d.TextPresent = true
							d.TextLengthBucket = consumerWebTextLengthBucket(len(text))
							break
						}
					}
				}
				result = append(result, consumerWebCandidateVideos(rawCandidate, &d, seen)...)
			}
		}
	}
	d.ParsePass = validFrame
	d.VideoExtractedCount = len(result)
	if len(result) == 0 {
		switch {
		case !validFrame:
			d.Cause = ConsumerWebVideoFailureParse
		case d.VideoKey60Present && !d.SlotRootShapeValid:
			d.Cause = ConsumerWebVideoFailureSlotParse
		case d.VideoKey60Present && d.VideoMetadataCount > 0:
			d.Cause = ConsumerWebVideoFailureURLMissing
		case d.VideoKey60Present:
			d.Cause = ConsumerWebVideoFailureSlotParse
		case validFrame && identity.CID != "" && identity.RID != "":
			d.Cause = ConsumerWebVideoAcceptedPending
		default:
			d.Cause = ConsumerWebVideoFailureNoVideo
		}
		// A provider error marker is meaningful only when no normal candidate
		// was parsed; successful terminal frames may also contain error metadata.
		if d.CandidateCount == 0 && strings.Contains(string(body), "BardErrorInfo") {
			d.ProviderErrorBlock = true
			d.Cause = ConsumerWebVideoFailureProviderBlock
		}
		if d.Cause == ConsumerWebVideoAcceptedPending {
			return nil, d, identity, nil
		}
		return nil, d, identity, errConsumerWebVideoResponse
	}
	d.Cause = ConsumerWebVideoFailureNone
	return result, d, identity, nil
}
