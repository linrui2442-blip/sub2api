package service

import "strings"

func NormalizeImageSizeTier(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "1k", "1024x1024":
		return "1K"
	case "4k", "4096x4096":
		return "4K"
	default:
		return "2K"
	}
}

func NormalizeVideoResolution(resolution string) string {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "720", "720p", "hd":
		return "720p"
	case "1080", "1080p", "full_hd", "full-hd", "fhd":
		return "1080p"
	default:
		return "480p"
	}
}

func NormalizeVideoDuration(seconds int) int {
	if seconds <= 0 {
		return 5
	}
	if seconds > 15 {
		return 15
	}
	return seconds
}
