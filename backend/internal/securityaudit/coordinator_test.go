package securityaudit

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePromptEngine struct {
	mode      Mode
	decision  *PromptDecision
	err       error
	enqueues  atomic.Int64
	evaluates atomic.Int64
}

func (f *fakePromptEngine) EffectiveMode() Mode                    { return f.mode }
func (f *fakePromptEngine) Enqueue(context.Context, Request) error { f.enqueues.Add(1); return f.err }
func (f *fakePromptEngine) Evaluate(context.Context, Request) (*PromptDecision, error) {
	f.evaluates.Add(1)
	return f.decision, f.err
}

func TestCoordinatorModes(t *testing.T) {
	tests := []struct {
		name           string
		mode           Mode
		prompt         *PromptDecision
		promptErr      error
		wantKind       DecisionKind
		wantCode       string
		wantEnqueue    int64
		wantEvaluation int64
	}{
		{name: "off", mode: ModeOff, wantKind: DecisionAllow},
		{name: "async only enqueues", mode: ModeAsync, wantKind: DecisionAllow, wantEnqueue: 1},
		{name: "blocking allow", mode: ModeBlocking, prompt: &PromptDecision{Kind: DecisionAllow, AllowNextStage: true}, wantKind: DecisionAllow, wantEvaluation: 1},
		{name: "blocking flag", mode: ModeBlocking, prompt: &PromptDecision{Kind: DecisionFlag, AllowNextStage: true}, wantKind: DecisionFlag, wantEvaluation: 1},
		{name: "blocking block", mode: ModeBlocking, prompt: &PromptDecision{Kind: DecisionBlock}, wantKind: DecisionBlock, wantCode: ErrorCodeBlocked, wantEvaluation: 1},
		{name: "blocking unavailable", mode: ModeBlocking, promptErr: errors.New("down"), wantKind: DecisionUnavailable, wantCode: ErrorCodeUnavailable, wantEvaluation: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := &fakePromptEngine{mode: tt.mode, decision: tt.prompt, err: tt.promptErr}
			decision := NewCoordinator(prompt).Check(context.Background(), Request{Body: []byte(`{}`)})
			require.Equal(t, tt.wantKind, decision.Kind)
			require.Equal(t, tt.wantCode, decision.ErrorCode)
			require.Equal(t, tt.wantEnqueue, prompt.enqueues.Load())
			require.Equal(t, tt.wantEvaluation, prompt.evaluates.Load())
		})
	}
}

func TestCoordinatorDoesNotMutateRequestBody(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	original := append([]byte(nil), body...)
	prompt := &fakePromptEngine{mode: ModeAsync}
	decision := NewCoordinator(prompt).Check(context.Background(), Request{Body: body})
	require.True(t, decision.AllowNextStage)
	require.Equal(t, original, body)
}

func TestCoordinatorPreservesPromptFactsAndMapsGatewayOutcome(t *testing.T) {
	result := &NormalizedResult{Decision: EventCritical, RiskLevel: RiskCritical, Action: ActionBlock, Categories: []string{"pii"}, ScannerScores: map[string]float64{"pii": 1}}
	promptDecision := &PromptDecision{Kind: DecisionBlock, Result: result}
	decision := NewCoordinator(&fakePromptEngine{mode: ModeBlocking, decision: promptDecision}).Check(context.Background(), Request{})
	require.Same(t, promptDecision, decision.Prompt)
	require.Same(t, result, decision.Prompt.Result)
	require.Equal(t, []string{"pii"}, decision.Prompt.Result.Categories)
	require.Equal(t, ErrorCodeBlocked, decision.ErrorCode)
}

func TestCoordinatorAsyncEnqueueFailuresNeverChangeResponse(t *testing.T) {
	for _, enqueueErr := range []error{ErrQueueFull, ErrQueueAdmissionBusy, errors.New("queue unavailable"), errors.New("publish failed")} {
		prompt := &fakePromptEngine{mode: ModeAsync, err: enqueueErr}
		decision := NewCoordinator(prompt).Check(context.Background(), Request{})
		require.True(t, decision.AllowNextStage)
		require.Equal(t, http.StatusOK, decision.HTTPStatus)
		require.Equal(t, int64(1), prompt.enqueues.Load())
		require.Zero(t, prompt.evaluates.Load())
	}
}
