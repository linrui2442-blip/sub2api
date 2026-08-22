package service

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type OpenAIMessagesDispatchModelConfig = domain.OpenAIMessagesDispatchModelConfig
type GroupModelsListConfig = domain.GroupModelsListConfig
type ReasoningEffortMapping = domain.ReasoningEffortMapping

// Group is a private routing and authorization boundary. It deliberately has
// no subscription, price, balance, profit, or commercial quota state.
type Group struct {
	ID                   int64
	Name                 string
	Description          string
	Platform             string
	IsExclusive          bool
	Status               string
	Hydrated             bool
	DuplicateOperationID string

	ClaudeCodeOnly                  bool
	FallbackGroupID                 *int64
	FallbackGroupIDOnInvalidRequest *int64
	ModelRouting                    map[string][]int64
	ModelRoutingEnabled             bool
	MCPXMLInject                    bool
	SupportedModelScopes            []string
	SortOrder                       int

	AllowMessagesDispatch       bool
	AllowLive                   bool
	RequireOAuthOnly            bool
	RequirePrivacySet           bool
	DefaultMappedModel          string
	MessagesDispatchModelConfig OpenAIMessagesDispatchModelConfig
	ModelsListConfig            GroupModelsListConfig
	RPMLimit                    int
	MaxReasoningEffort          string
	ReasoningEffortMappings     []ReasoningEffortMapping

	CreatedAt time.Time
	UpdatedAt time.Time

	AccountGroups           []AccountGroup
	AccountCount            int64
	ActiveAccountCount      int64
	RateLimitedAccountCount int64
}

func (g *Group) IsActive() bool { return g != nil && g.Status == StatusActive }

// IsGroupContextValid reports whether a group came from a trusted repository
// hydration and can safely drive routing decisions.
func IsGroupContextValid(group *Group) bool {
	return group != nil && group.ID > 0 && group.Hydrated
}

// GetRoutingAccountIDs resolves an optional model-specific preferred account
// order. Scheduler health, cooldown and failover checks still apply afterwards.
func (g *Group) GetRoutingAccountIDs(requestedModel string) []int64 {
	if g == nil || !g.ModelRoutingEnabled || len(g.ModelRouting) == 0 {
		return nil
	}
	requestedModel = strings.TrimSpace(strings.ToLower(requestedModel))
	if requestedModel == "" {
		return nil
	}
	for pattern, accountIDs := range g.ModelRouting {
		if matchModelPattern(strings.ToLower(strings.TrimSpace(pattern)), requestedModel) {
			return append([]int64(nil), accountIDs...)
		}
	}
	return nil
}

func matchModelPattern(pattern, model string) bool {
	if pattern == "" || model == "" {
		return false
	}
	if pattern == "*" || pattern == model {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

func NormalizeGroupPlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}
