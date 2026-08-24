package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const fingerprintSalt = "59cf53e54c78"

func computeClaudeCodeFingerprint(body []byte, version string) string {
	firstText := extractFirstUserText(body)
	chars := make([]byte, 0, 3)
	for _, index := range []int{4, 7, 20} {
		if index < len(firstText) {
			chars = append(chars, firstText[index])
		} else {
			chars = append(chars, '0')
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + string(chars) + version))
	return hex.EncodeToString(sum[:])[:3]
}

func extractFirstUserText(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return ""
	}
	first := ""
	messages.ForEach(func(_, message gjson.Result) bool {
		if message.Get("role").String() != "user" {
			return true
		}
		content := message.Get("content")
		if content.Type == gjson.String {
			first = content.String()
			return false
		}
		content.ForEach(func(_, block gjson.Result) bool {
			if block.Get("type").String() == "text" {
				first = block.Get("text").String()
				return false
			}
			return true
		})
		return false
	})
	return first
}

// buildBillingAttributionText emits Anthropic's protocol-defined Claude Code
// identity marker. "billing-header" is an upstream wire name, not local billing.
func buildBillingAttributionText(body []byte, cliVersion string) (string, error) {
	if cliVersion == "" {
		return "", fmt.Errorf("cliVersion required")
	}
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli;", cliVersion, computeClaudeCodeFingerprint(body, cliVersion)), nil
}

var ccVersionInBillingRe = regexp.MustCompile(`cc_version=\d+\.\d+\.\d+`)

func syncBillingHeaderVersion(body []byte, userAgent string) []byte {
	version := ExtractCLIVersion(userAgent)
	if version == "" {
		return body
	}
	system := gjson.GetBytes(body, "system")
	if !system.IsArray() {
		return body
	}
	index := 0
	system.ForEach(func(_, item gjson.Result) bool {
		text := item.Get("text")
		if text.Type == gjson.String && strings.HasPrefix(text.String(), "x-anthropic-billing-header") {
			updated := ccVersionInBillingRe.ReplaceAllString(text.String(), "cc_version="+version)
			if updated != text.String() {
				if next, err := sjson.SetBytes(body, fmt.Sprintf("system.%d.text", index), updated); err == nil {
					body = next
				}
			}
		}
		index++
		return true
	})
	return body
}
