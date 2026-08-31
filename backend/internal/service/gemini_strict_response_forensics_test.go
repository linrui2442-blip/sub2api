package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func strictForensicsChatResponse(blocks []apicompat.AnthropicContentBlock, stopReason string) *apicompat.ChatCompletionsResponse {
	anthropicResponse := &apicompat.AnthropicResponse{
		Content:    blocks,
		Model:      "gemini-test-model",
		StopReason: apicompat.AnthropicStopReasonPtr(stopReason),
	}
	responsesResponse := apicompat.AnthropicToResponsesResponse(anthropicResponse)
	return apicompat.ResponsesToChatCompletions(responsesResponse, anthropicResponse.Model)
}

func strictForensicsContent(t *testing.T, response *apicompat.ChatCompletionsResponse) string {
	t.Helper()
	require.Len(t, response.Choices, 1)
	var content string
	require.NoError(t, json.Unmarshal(response.Choices[0].Message.Content, &content))
	return content
}

func TestStrictStructuredResponseConversionBlockMatrix(t *testing.T) {
	validDocument, err := json.Marshal(validGenericComplexDocument())
	require.NoError(t, err)
	validText := string(validDocument)

	tests := []struct {
		name           string
		blocks         []apicompat.AnthropicContentBlock
		wantContent    string
		wantReasoning  string
		wantValidation bool
	}{
		{
			name:           "one complete JSON text block",
			blocks:         []apicompat.AnthropicContentBlock{{Type: "text", Text: validText}},
			wantContent:    validText,
			wantValidation: true,
		},
		{
			name: "JSON split across contiguous text blocks",
			blocks: []apicompat.AnthropicContentBlock{
				{Type: "text", Text: validText[:len(validText)/2]},
				{Type: "text", Text: validText[len(validText)/2:]},
			},
			wantContent:    validText,
			wantValidation: true,
		},
		{
			name: "independent JSON documents concatenate without delimiter",
			blocks: []apicompat.AnthropicContentBlock{
				{Type: "text", Text: validText},
				{Type: "text", Text: `{}`},
			},
			wantContent: validText + `{}`,
		},
		{
			name: "reasoning remains separate from JSON content",
			blocks: []apicompat.AnthropicContentBlock{
				{Type: "thinking", Thinking: "private synthetic reasoning"},
				{Type: "text", Text: validText},
			},
			wantContent:    validText,
			wantReasoning:  "private synthetic reasoning",
			wantValidation: true,
		},
		{
			name:           "surrounding JSON whitespace is retained and accepted",
			blocks:         []apicompat.AnthropicContentBlock{{Type: "text", Text: " \n" + validText + "\t "}},
			wantContent:    " \n" + validText + "\t ",
			wantValidation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := strictForensicsChatResponse(tt.blocks, "end_turn")
			require.Equal(t, "stop", response.Choices[0].FinishReason)
			require.Equal(t, tt.wantContent, strictForensicsContent(t, response))
			require.Equal(t, tt.wantReasoning, response.Choices[0].Message.ReasoningContent)
			err := validateStrictStructuredChatResponse(response, genericComplexOutput())
			if tt.wantValidation {
				require.NoError(t, err)
			} else {
				requireGenericComplexDiagnostic(t, err, "$", "parse", "valid JSON", "invalid_json", "parsing")
			}
		})
	}
}

func TestStrictStructuredResponseTruncationPreservesLengthBeforeParseFailure(t *testing.T) {
	response := strictForensicsChatResponse([]apicompat.AnthropicContentBlock{
		{Type: "text", Text: `{"header":`},
	}, "max_tokens")

	require.Equal(t, "length", response.Choices[0].FinishReason)
	requireGenericComplexDiagnostic(
		t,
		validateStrictStructuredChatResponse(response, genericComplexOutput()),
		"$", "parse", "valid JSON", "invalid_json", "parsing",
	)
}

func TestMergeCollectedGeminiPartsKeepsThinkingSeparateAndConcatenatesText(t *testing.T) {
	merged := mergeCollectedPartsToResponse(map[string]any{}, []map[string]any{
		{"text": "first"},
		{"text": "second"},
		{"text": "reasoning", "thought": true},
		{"text": "third"},
	})

	candidates := merged["candidates"].([]any)
	content := candidates[0].(map[string]any)["content"].(map[string]any)
	parts := content["parts"].([]any)
	require.Equal(t, []any{
		map[string]any{"text": "firstsecond"},
		map[string]any{"text": "reasoning", "thought": true},
		map[string]any{"text": "third"},
	}, parts)
}
