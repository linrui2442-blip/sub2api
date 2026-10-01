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
	seen := make(map[string]struct{})
	result := make([]consumerWebGeneratedVideo, 0)
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
			candidates, ok := consumerWebListAt(payload, 4).([]any)
			if !ok {
				continue
			}
			for _, rawCandidate := range candidates {
				media, ok := consumerWebListAt(rawCandidate, 12).([]any)
				if !ok {
					continue
				}
				for _, rawEntry := range media {
					entry, ok := rawEntry.(map[string]any)
					if !ok {
						continue
					}
					group, ok := consumerWebListAt(entry["60"], 0, 0, 0).([]any)
					if !ok {
						continue
					}
					for _, rawMeta := range group {
						meta, ok := rawMeta.([]any)
						if !ok || len(meta) <= 7 {
							continue
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
			}
		}
	}
	if len(result) == 0 {
		return nil, errConsumerWebVideoResponse
	}
	return result, nil
}
