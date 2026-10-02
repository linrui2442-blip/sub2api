package service

import "testing"

func TestConsumerWebRuntimeTokenShapeMatrix(t *testing.T) {
	tests := []struct {
		name, body string
		wantToken  bool
	}{
		{"compact object", `{"SNlM0e":"TOKEN"}`, true},
		{"spaced object", `{"SNlM0e": "TOKEN"}`, true},
		{"multiple spaces", `{"SNlM0e":    "TOKEN"}`, true},
		{"array", `["SNlM0e","TOKEN"]`, true},
		{"other init markers only", `{"cfb2h":"build","FdrFJe":"session","TuX5cc":"en","qKIAYe":"push"}`, false},
		{"unrelated key mention", `SNlM0e`, false},
		{"malformed value", `{"SNlM0e": TOKEN}`, false},
		{"escaped embedded text", `\"SNlM0e\":\"TOKEN\"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseConsumerWebRuntime(tc.body).AuthToken
			if (got != "") != tc.wantToken {
				t.Fatalf("token presence = %t, want %t", got != "", tc.wantToken)
			}
		})
	}
}
