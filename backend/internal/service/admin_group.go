package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// Group management implementations
func (s *adminServiceImpl) ListGroups(ctx context.Context, page, pageSize int, platform, status, search string, isExclusive *bool, sortBy, sortOrder string) ([]Group, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	groups, result, err := s.groupRepo.ListWithFilters(ctx, params, platform, status, search, isExclusive)
	if err != nil {
		return nil, 0, err
	}
	return groups, result.Total, nil
}

func (s *adminServiceImpl) GetAllGroups(ctx context.Context) ([]Group, error) {
	return s.groupRepo.ListActive(ctx)
}

func (s *adminServiceImpl) GetAllGroupsByPlatform(ctx context.Context, platform string) ([]Group, error) {
	return s.groupRepo.ListActiveByPlatform(ctx, platform)
}

func (s *adminServiceImpl) GetAllGroupsIncludingInactive(ctx context.Context) ([]Group, error) {
	// ListWithFilters with empty status = no status filter, so active + disabled groups are returned.
	// PageSize 10000 is intentionally large; group count is O(dozens) in practice.
	groups, _, err := s.groupRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10000}, "", "", "", nil)
	return groups, err
}

func (s *adminServiceImpl) GetGroup(ctx context.Context, id int64) (*Group, error) {
	return s.groupRepo.GetByID(ctx, id)
}

func (s *adminServiceImpl) GetGroupModelsListCandidates(ctx context.Context, id int64, platform string) ([]string, error) {
	platform = strings.TrimSpace(platform)
	if id > 0 {
		group, err := s.groupRepo.GetByIDLite(ctx, id)
		if err != nil {
			return nil, err
		}
		if platform == "" {
			platform = group.Platform
		}
	}
	if platform == "" {
		platform = PlatformAnthropic
	}

	candidates := defaultModelsListCandidateIDs(platform)
	if id <= 0 || s.accountRepo == nil {
		return candidates, nil
	}

	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, id)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, model := range candidates {
		seen[model] = struct{}{}
	}
	for _, acc := range accounts {
		if platform == PlatformComposite {
			if !isConcreteRequestPlatform(acc.Platform) {
				continue
			}
		} else if acc.Platform != platform {
			continue
		}
		for model := range acc.GetModelMapping() {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			candidates = append(candidates, model)
		}
	}
	return candidates, nil
}

func (s *adminServiceImpl) ListCompositeRoutes(ctx context.Context, groupID int64) ([]CompositeModelRoute, error) {
	if err := s.requireCompositeGroup(ctx, groupID); err != nil {
		return nil, err
	}
	if s.compositeRouteRepo == nil {
		return nil, fmt.Errorf("composite route repository is not configured")
	}
	return s.compositeRouteRepo.ListByGroup(ctx, groupID, true)
}

func (s *adminServiceImpl) CreateCompositeRoute(ctx context.Context, groupID int64, input CompositeRouteInput) (*CompositeModelRoute, error) {
	if err := s.requireCompositeGroup(ctx, groupID); err != nil {
		return nil, err
	}
	if s.compositeRouteRepo == nil {
		return nil, fmt.Errorf("composite route repository is not configured")
	}
	route, err := compositeRouteFromInput(groupID, input)
	if err != nil {
		return nil, err
	}
	if err := s.compositeRouteRepo.Create(ctx, route); err != nil {
		return nil, err
	}
	return route, nil
}

func (s *adminServiceImpl) UpdateCompositeRoute(ctx context.Context, groupID, routeID int64, input CompositeRouteInput) (*CompositeModelRoute, error) {
	if err := s.requireCompositeGroup(ctx, groupID); err != nil {
		return nil, err
	}
	if s.compositeRouteRepo == nil {
		return nil, fmt.Errorf("composite route repository is not configured")
	}
	if ok, err := s.compositeRouteBelongsToGroup(ctx, groupID, routeID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrCompositeRouteNotFound
	}
	route, err := compositeRouteFromInput(groupID, input)
	if err != nil {
		return nil, err
	}
	route.ID = routeID
	if err := s.compositeRouteRepo.Update(ctx, route); err != nil {
		return nil, err
	}
	return route, nil
}

func (s *adminServiceImpl) DeleteCompositeRoute(ctx context.Context, groupID, routeID int64) error {
	if err := s.requireCompositeGroup(ctx, groupID); err != nil {
		return err
	}
	if s.compositeRouteRepo == nil {
		return fmt.Errorf("composite route repository is not configured")
	}
	if ok, err := s.compositeRouteBelongsToGroup(ctx, groupID, routeID); err != nil {
		return err
	} else if !ok {
		return ErrCompositeRouteNotFound
	}
	return s.compositeRouteRepo.Delete(ctx, routeID)
}

func (s *adminServiceImpl) PreviewCompositeRoute(ctx context.Context, groupID int64, input CompositeRoutePreviewRequest) (*CompositeRouteDecision, error) {
	if err := s.requireCompositeGroup(ctx, groupID); err != nil {
		return nil, err
	}
	resolver := s.compositeResolver
	if resolver == nil {
		resolver = NewCompositeRouteResolver(s.compositeRouteRepo)
	}
	decision, err := resolver.Resolve(ctx, groupID, input.Model, input.Endpoint)
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

func (s *adminServiceImpl) requireCompositeGroup(ctx context.Context, groupID int64) error {
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		return err
	}
	if group.Platform != PlatformComposite {
		return fmt.Errorf("group %d is not a composite group", groupID)
	}
	return nil
}

func (s *adminServiceImpl) compositeRouteBelongsToGroup(ctx context.Context, groupID, routeID int64) (bool, error) {
	routes, err := s.compositeRouteRepo.ListByGroup(ctx, groupID, true)
	if err != nil {
		return false, err
	}
	for i := range routes {
		if routes[i].ID == routeID {
			return true, nil
		}
	}
	return false, nil
}

func compositeRouteFromInput(groupID int64, input CompositeRouteInput) (*CompositeModelRoute, error) {
	input = normalizeCompositeRouteInput(input)
	if input.PublicModel == "" {
		return nil, fmt.Errorf("public_model is required")
	}
	if !isConcreteRequestPlatform(input.TargetPlatform) {
		return nil, fmt.Errorf("target_platform must be a concrete provider")
	}
	if input.Priority == 0 {
		input.Priority = 100
	}
	return &CompositeModelRoute{
		GroupID:        groupID,
		PublicModel:    input.PublicModel,
		MatchType:      input.MatchType,
		TargetPlatform: input.TargetPlatform,
		UpstreamModel:  input.UpstreamModel,
		Endpoint:       input.Endpoint,
		Priority:       input.Priority,
		Enabled:        input.Enabled,
		Notes:          input.Notes,
	}, nil
}

func defaultModelsListCandidateIDs(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformComposite:
		return compositeDefaultModelsListCandidateIDs()
	default:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

func compositeDefaultModelsListCandidateIDs() []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek} {
		for _, id := range defaultModelsListCandidateIDs(platform) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func canCopyAccountsFromGroupPlatform(targetPlatform, sourcePlatform string) bool {
	if targetPlatform == PlatformComposite {
		return sourcePlatform == PlatformComposite || isConcreteRequestPlatform(sourcePlatform)
	}
	return sourcePlatform == targetPlatform
}

func groupSupportsOAuthOnlyFilter(platform string) bool {
	return platform == PlatformOpenAI ||
		platform == PlatformAntigravity ||
		platform == PlatformAnthropic ||
		platform == PlatformGemini ||
		platform == PlatformGrok ||
		platform == PlatformComposite
}

func (s *adminServiceImpl) CreateGroup(ctx context.Context, input *CreateGroupInput) (*Group, error) {
	if input == nil {
		return nil, errors.New("group input is required")
	}
	platform := NormalizeGroupPlatform(input.Platform)
	maxReasoningEffort, err := normalizeMaxReasoningEffortForPlatform(platform, input.MaxReasoningEffort)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_MAX_REASONING_EFFORT", "%v", err)
	}
	reasoningMappings, err := NormalizeReasoningEffortMappings(platform, input.ReasoningEffortMappings)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_REASONING_EFFORT_MAPPING", "%v", err)
	}
	if input.FallbackGroupID != nil {
		if err := s.validateFallbackGroup(ctx, 0, *input.FallbackGroupID); err != nil {
			return nil, err
		}
	}
	fallbackInvalid := input.FallbackGroupIDOnInvalidRequest
	if fallbackInvalid != nil && *fallbackInvalid <= 0 {
		fallbackInvalid = nil
	}
	if fallbackInvalid != nil {
		if err := s.validateFallbackGroupOnInvalidRequest(ctx, 0, platform, *fallbackInvalid); err != nil {
			return nil, err
		}
	}
	mcpXMLInject := true
	if input.MCPXMLInject != nil {
		mcpXMLInject = *input.MCPXMLInject
	}
	group := &Group{
		Name: input.Name, Description: input.Description, Platform: platform,
		IsExclusive: input.IsExclusive, Status: StatusActive,
		ClaudeCodeOnly: input.ClaudeCodeOnly, FallbackGroupID: input.FallbackGroupID,
		FallbackGroupIDOnInvalidRequest: fallbackInvalid,
		ModelRouting:                    input.ModelRouting, ModelRoutingEnabled: input.ModelRoutingEnabled,
		MCPXMLInject: mcpXMLInject, SupportedModelScopes: input.SupportedModelScopes,
		AllowMessagesDispatch: input.AllowMessagesDispatch, AllowLive: input.AllowLive,
		RequireOAuthOnly: input.RequireOAuthOnly, RequirePrivacySet: input.RequirePrivacySet,
		DefaultMappedModel:          input.DefaultMappedModel,
		MessagesDispatchModelConfig: normalizeOpenAIMessagesDispatchModelConfig(input.MessagesDispatchModelConfig),
		ModelsListConfig:            normalizeGroupModelsListConfig(input.ModelsListConfig),
		RPMLimit:                    input.RPMLimit, MaxReasoningEffort: maxReasoningEffort,
		ReasoningEffortMappings: reasoningMappings,
	}
	sanitizeGroupMessagesDispatchFields(group)
	if group.Platform != PlatformOpenAI && group.Platform != PlatformComposite {
		group.AllowLive = false
	}
	sanitizeGroupReasoningEffortPolicy(group)
	if err := s.groupRepo.Create(ctx, group); err != nil {
		return nil, err
	}
	accountIDs, err := s.accountIDsFromSourceGroups(ctx, 0, group.Platform, input.CopyAccountsFromGroupIDs)
	if err != nil {
		return nil, err
	}
	accountIDs, err = s.filterOAuthOnlyAccounts(ctx, group, accountIDs)
	if err != nil {
		return nil, err
	}
	if len(accountIDs) > 0 {
		if err := s.groupRepo.BindAccountsToGroup(ctx, group.ID, accountIDs); err != nil {
			return nil, fmt.Errorf("bind copied accounts: %w", err)
		}
		group.AccountCount = int64(len(accountIDs))
	}
	return group, nil
}

func (s *adminServiceImpl) accountIDsFromSourceGroups(ctx context.Context, currentID int64, platform string, sourceIDs []int64) ([]int64, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	seen := make(map[int64]struct{}, len(sourceIDs))
	unique := make([]int64, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if sourceID <= 0 || sourceID == currentID {
			return nil, errors.New("invalid source group")
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		unique = append(unique, sourceID)
	}
	for _, sourceID := range unique {
		source, err := s.groupRepo.GetByIDLite(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("source group %d not found: %w", sourceID, err)
		}
		if !canCopyAccountsFromGroupPlatform(platform, source.Platform) {
			return nil, fmt.Errorf("source group %d platform mismatch", sourceID)
		}
	}
	return s.groupRepo.GetAccountIDsByGroupIDs(ctx, unique)
}

func (s *adminServiceImpl) filterOAuthOnlyAccounts(ctx context.Context, group *Group, ids []int64) ([]int64, error) {
	if group == nil || !group.RequireOAuthOnly || !groupSupportsOAuthOnlyFilter(group.Platform) || len(ids) == 0 {
		return ids, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		if account.Type != AccountTypeAPIKey {
			allowed[account.ID] = struct{}{}
		}
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *adminServiceImpl) validateFallbackGroup(ctx context.Context, currentGroupID, fallbackGroupID int64) error {
	// 不能将自己设置为降级分组
	if currentGroupID > 0 && currentGroupID == fallbackGroupID {
		return fmt.Errorf("cannot set self as fallback group")
	}

	visited := map[int64]struct{}{}
	nextID := fallbackGroupID
	for {
		if _, seen := visited[nextID]; seen {
			return fmt.Errorf("fallback group cycle detected")
		}
		visited[nextID] = struct{}{}
		if currentGroupID > 0 && nextID == currentGroupID {
			return fmt.Errorf("fallback group cycle detected")
		}

		// 检查降级分组是否存在
		fallbackGroup, err := s.groupRepo.GetByIDLite(ctx, nextID)
		if err != nil {
			return fmt.Errorf("fallback group not found: %w", err)
		}

		// 降级分组不能启用 claude_code_only，否则会造成死循环
		if nextID == fallbackGroupID && fallbackGroup.ClaudeCodeOnly {
			return fmt.Errorf("fallback group cannot have claude_code_only enabled")
		}

		if fallbackGroup.FallbackGroupID == nil {
			return nil
		}
		nextID = *fallbackGroup.FallbackGroupID
	}
}

// validateFallbackGroupOnInvalidRequest 校验无效请求兜底分组的有效性
// currentGroupID: 当前分组 ID（新建时为 0）
// platform: 当前分组的有效平台
// fallbackGroupID: 兜底分组 ID
func (s *adminServiceImpl) validateFallbackGroupOnInvalidRequest(ctx context.Context, currentGroupID int64, platform string, fallbackGroupID int64) error {
	if platform != PlatformAnthropic && platform != PlatformAntigravity {
		return fmt.Errorf("invalid request fallback only supported for anthropic or antigravity groups")
	}
	if currentGroupID > 0 && currentGroupID == fallbackGroupID {
		return fmt.Errorf("cannot set self as invalid request fallback group")
	}

	fallbackGroup, err := s.groupRepo.GetByIDLite(ctx, fallbackGroupID)
	if err != nil {
		return fmt.Errorf("fallback group not found: %w", err)
	}
	if fallbackGroup.Platform != PlatformAnthropic {
		return fmt.Errorf("fallback group must be anthropic platform")
	}
	if fallbackGroup.FallbackGroupIDOnInvalidRequest != nil {
		return fmt.Errorf("fallback group cannot have invalid request fallback configured")
	}
	return nil
}

func (s *adminServiceImpl) UpdateGroup(ctx context.Context, id int64, input *UpdateGroupInput) (*Group, error) {
	if input == nil {
		return nil, errors.New("group input is required")
	}
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	previousPlatform := group.Platform
	if input.Name != "" {
		group.Name = input.Name
	}
	if input.Description != nil {
		group.Description = *input.Description
	}
	if input.Platform != "" {
		group.Platform = NormalizeGroupPlatform(input.Platform)
	}
	if group.FallbackGroupIDOnInvalidRequest != nil && group.Platform != PlatformAnthropic && group.Platform != PlatformAntigravity {
		return nil, fmt.Errorf("invalid request fallback only supported for anthropic or antigravity groups")
	}
	if input.IsExclusive != nil {
		group.IsExclusive = *input.IsExclusive
	}
	if input.Status != "" {
		group.Status = input.Status
	}
	if input.ClaudeCodeOnly != nil {
		group.ClaudeCodeOnly = *input.ClaudeCodeOnly
	}
	if input.FallbackGroupID != nil {
		if *input.FallbackGroupID > 0 {
			if err := s.validateFallbackGroup(ctx, id, *input.FallbackGroupID); err != nil {
				return nil, err
			}
			group.FallbackGroupID = input.FallbackGroupID
		} else {
			group.FallbackGroupID = nil
		}
	}
	if input.FallbackGroupIDOnInvalidRequest != nil {
		if *input.FallbackGroupIDOnInvalidRequest > 0 {
			if err := s.validateFallbackGroupOnInvalidRequest(ctx, id, group.Platform, *input.FallbackGroupIDOnInvalidRequest); err != nil {
				return nil, err
			}
			group.FallbackGroupIDOnInvalidRequest = input.FallbackGroupIDOnInvalidRequest
		} else {
			group.FallbackGroupIDOnInvalidRequest = nil
		}
	}
	if input.ModelRouting != nil {
		group.ModelRouting = input.ModelRouting
	}
	if input.ModelRoutingEnabled != nil {
		group.ModelRoutingEnabled = *input.ModelRoutingEnabled
	}
	if input.MCPXMLInject != nil {
		group.MCPXMLInject = *input.MCPXMLInject
	}
	if input.SupportedModelScopes != nil {
		group.SupportedModelScopes = *input.SupportedModelScopes
	}
	if input.AllowMessagesDispatch != nil {
		group.AllowMessagesDispatch = *input.AllowMessagesDispatch
	}
	if input.AllowLive != nil {
		group.AllowLive = *input.AllowLive
	}
	if input.RequireOAuthOnly != nil {
		group.RequireOAuthOnly = *input.RequireOAuthOnly
	}
	if input.RequirePrivacySet != nil {
		group.RequirePrivacySet = *input.RequirePrivacySet
	}
	if input.DefaultMappedModel != nil {
		group.DefaultMappedModel = *input.DefaultMappedModel
	}
	if input.MessagesDispatchModelConfig != nil {
		group.MessagesDispatchModelConfig = normalizeOpenAIMessagesDispatchModelConfig(*input.MessagesDispatchModelConfig)
	}
	if input.ModelsListConfig != nil {
		group.ModelsListConfig = normalizeGroupModelsListConfig(*input.ModelsListConfig)
	}
	if input.RPMLimit != nil {
		group.RPMLimit = *input.RPMLimit
	}
	if input.MaxReasoningEffort != nil {
		value, err := normalizeMaxReasoningEffortForPlatform(group.Platform, *input.MaxReasoningEffort)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_MAX_REASONING_EFFORT", "%v", err)
		}
		group.MaxReasoningEffort = value
	}
	if input.ReasoningEffortMappings != nil {
		value, err := NormalizeReasoningEffortMappings(group.Platform, *input.ReasoningEffortMappings)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_REASONING_EFFORT_MAPPING", "%v", err)
		}
		group.ReasoningEffortMappings = value
	}
	sanitizeGroupMessagesDispatchFields(group)
	if group.Platform != PlatformOpenAI && group.Platform != PlatformComposite {
		group.AllowLive = false
	}
	sanitizeGroupReasoningEffortPolicy(group)
	if err := s.groupRepo.Update(ctx, group); err != nil {
		return nil, err
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}
	if group.Platform != previousPlatform && s.channelCacheInvalidator != nil {
		s.channelCacheInvalidator.InvalidateCache()
	}
	if len(input.CopyAccountsFromGroupIDs) > 0 {
		ids, err := s.accountIDsFromSourceGroups(ctx, id, group.Platform, input.CopyAccountsFromGroupIDs)
		if err != nil {
			return nil, err
		}
		ids, err = s.filterOAuthOnlyAccounts(ctx, group, ids)
		if err != nil {
			return nil, err
		}
		if _, err := s.groupRepo.DeleteAccountGroupsByGroupID(ctx, id); err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			if err := s.groupRepo.BindAccountsToGroup(ctx, id, ids); err != nil {
				return nil, err
			}
		}
	}
	return group, nil
}

func (s *adminServiceImpl) DeleteGroup(ctx context.Context, id int64) error {
	var groupKeys []string
	if s.authCacheInvalidator != nil {
		keys, err := s.apiKeyRepo.ListKeysByGroupID(ctx, id)
		if err == nil {
			groupKeys = keys
		}
	}

	_, err := s.groupRepo.DeleteCascade(ctx, id)
	if err != nil {
		return err
	}
	// 注意：user_group_rate_multipliers 表通过外键 ON DELETE CASCADE 自动清理

	if s.authCacheInvalidator != nil {
		for _, key := range groupKeys {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key)
		}
	}

	return nil
}

func (s *adminServiceImpl) GetGroupAPIKeys(ctx context.Context, groupID int64, page, pageSize int) ([]APIKey, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	keys, result, err := s.apiKeyRepo.ListByGroupID(ctx, groupID, params)
	if err != nil {
		return nil, 0, err
	}
	return keys, result.Total, nil
}

func (s *adminServiceImpl) GetGroupRateMultipliers(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error) {
	if s.userGroupRateRepo == nil {
		return nil, nil
	}
	return s.userGroupRateRepo.GetByGroupID(ctx, groupID)
}

func (s *adminServiceImpl) ClearGroupRateMultipliers(ctx context.Context, groupID int64) error {
	if s.userGroupRateRepo == nil {
		return nil
	}
	return s.userGroupRateRepo.DeleteByGroupID(ctx, groupID)
}

func (s *adminServiceImpl) BatchSetGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error {
	if s.userGroupRateRepo == nil {
		return nil
	}
	for _, e := range entries {
		if e.RateMultiplier <= 0 {
			return fmt.Errorf("rate_multiplier must be > 0 (user_id=%d)", e.UserID)
		}
	}
	return s.userGroupRateRepo.SyncGroupRateMultipliers(ctx, groupID, entries)
}

func (s *adminServiceImpl) ClearGroupRPMOverrides(ctx context.Context, groupID int64) error {
	if s.userGroupRateRepo == nil {
		return nil
	}
	if err := s.userGroupRateRepo.ClearGroupRPMOverrides(ctx, groupID); err != nil {
		return err
	}
	// RPM override 已嵌入 auth cache snapshot (v7)，变更后必须失效相关缓存。
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, groupID)
	}
	return nil
}

func (s *adminServiceImpl) BatchSetGroupRPMOverrides(ctx context.Context, groupID int64, entries []GroupRPMOverrideInput) error {
	if s.userGroupRateRepo == nil {
		return nil
	}
	for _, e := range entries {
		if e.RPMOverride != nil && *e.RPMOverride < 0 {
			return infraerrors.BadRequest("INVALID_RPM_OVERRIDE", fmt.Sprintf("rpm_override must be >= 0 (user_id=%d)", e.UserID))
		}
	}
	if err := s.userGroupRateRepo.SyncGroupRPMOverrides(ctx, groupID, entries); err != nil {
		return err
	}
	// RPM override 已嵌入 auth cache snapshot (v7)，变更后必须失效相关缓存。
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, groupID)
	}
	return nil
}

func (s *adminServiceImpl) UpdateGroupSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error {
	return s.groupRepo.UpdateSortOrders(ctx, updates)
}

// AdminUpdateAPIKeyGroupID 管理员修改 API Key 分组绑定
// groupID: nil=不修改, 指向0=解绑, 指向正整数=绑定到目标分组
func (s *adminServiceImpl) AdminUpdateAPIKeyGroupID(ctx context.Context, keyID int64, groupID *int64) (*AdminUpdateAPIKeyGroupIDResult, error) {
	apiKey, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}

	if groupID == nil {
		// nil 表示不修改，直接返回
		return &AdminUpdateAPIKeyGroupIDResult{APIKey: apiKey}, nil
	}

	if *groupID < 0 {
		return nil, infraerrors.BadRequest("INVALID_GROUP_ID", "group_id must be non-negative")
	}

	result := &AdminUpdateAPIKeyGroupIDResult{}

	if *groupID == 0 {
		// 0 表示解绑分组（不修改 user_allowed_groups，避免影响用户其他 Key）
		apiKey.GroupID = nil
		apiKey.Group = nil
	} else {
		// 验证目标分组存在且状态为 active
		group, err := s.groupRepo.GetByID(ctx, *groupID)
		if err != nil {
			return nil, err
		}
		if group.Status != StatusActive {
			return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
		}
		gid := *groupID
		apiKey.GroupID = &gid
		apiKey.Group = group

		// 专属标准分组：使用事务保证「添加分组权限」与「更新 API Key」的原子性
		if group.IsExclusive {
			opCtx := ctx
			var tx *dbent.Tx
			if s.entClient == nil {
				logger.LegacyPrintf("service.admin", "Warning: entClient is nil, skipping transaction protection for exclusive group binding")
			} else {
				var txErr error
				tx, txErr = s.entClient.Tx(ctx)
				if txErr != nil {
					return nil, fmt.Errorf("begin transaction: %w", txErr)
				}
				defer func() { _ = tx.Rollback() }()
				opCtx = dbent.NewTxContext(ctx, tx)
			}

			if addErr := s.userRepo.AddGroupToAllowedGroups(opCtx, apiKey.UserID, gid); addErr != nil {
				return nil, fmt.Errorf("add group to user allowed groups: %w", addErr)
			}
			if err := s.apiKeyRepo.Update(opCtx, apiKey, APIKeyUpdateFields{GroupID: true}); err != nil {
				return nil, fmt.Errorf("update api key: %w", err)
			}
			if tx != nil {
				if err := tx.Commit(); err != nil {
					return nil, fmt.Errorf("commit transaction: %w", err)
				}
			}

			result.AutoGrantedGroupAccess = true
			result.GrantedGroupID = &gid
			result.GrantedGroupName = group.Name

			// 失效认证缓存（在事务提交后执行）
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
			}

			result.APIKey = apiKey
			return result, nil
		}
	}

	// 非专属分组 / 解绑：无需事务，单步更新即可
	if err := s.apiKeyRepo.Update(ctx, apiKey, APIKeyUpdateFields{GroupID: true}); err != nil {
		return nil, fmt.Errorf("update api key: %w", err)
	}

	// 失效认证缓存
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}

	result.APIKey = apiKey
	return result, nil
}

// AdminResetAPIKeyRateLimitUsage resets all API key rate-limit usage windows.
func (s *adminServiceImpl) AdminResetAPIKeyRateLimitUsage(ctx context.Context, keyID int64) (*APIKey, error) {
	apiKey, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	apiKey.Usage5h = 0
	apiKey.Usage1d = 0
	apiKey.Usage7d = 0
	apiKey.Window5hStart = nil
	apiKey.Window1dStart = nil
	apiKey.Window7dStart = nil
	if err := s.apiKeyRepo.Update(ctx, apiKey, APIKeyUpdateFields{RateLimitUsage: true}); err != nil {
		return nil, fmt.Errorf("reset api key rate limit usage: %w", err)
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	return apiKey, nil
}

// ReplaceUserGroup 替换用户的专属分组
func (s *adminServiceImpl) ReplaceUserGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (*ReplaceUserGroupResult, error) {
	if oldGroupID == newGroupID {
		return nil, infraerrors.BadRequest("SAME_GROUP", "old and new group must be different")
	}

	// 验证新分组存在且为活跃的专属标准分组
	newGroup, err := s.groupRepo.GetByID(ctx, newGroupID)
	if err != nil {
		return nil, err
	}
	if newGroup.Status != StatusActive {
		return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
	}
	if !newGroup.IsExclusive {
		return nil, infraerrors.BadRequest("GROUP_NOT_EXCLUSIVE", "target group is not exclusive")
	}

	// 事务保证原子性
	if s.entClient == nil {
		return nil, fmt.Errorf("entClient is nil, cannot perform group replacement")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	opCtx := dbent.NewTxContext(ctx, tx)

	// 1. 授予新分组权限
	if err := s.userRepo.AddGroupToAllowedGroups(opCtx, userID, newGroupID); err != nil {
		return nil, fmt.Errorf("add new group to allowed groups: %w", err)
	}

	// 2. 迁移绑定旧分组的 Key 到新分组
	migrated, err := s.apiKeyRepo.UpdateGroupIDByUserAndGroup(opCtx, userID, oldGroupID, newGroupID)
	if err != nil {
		return nil, fmt.Errorf("migrate api keys: %w", err)
	}

	// 3. 移除旧分组权限
	if err := s.userRepo.RemoveGroupFromUserAllowedGroups(opCtx, userID, oldGroupID); err != nil {
		return nil, fmt.Errorf("remove old group from allowed groups: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// 失效该用户所有 Key 的认证缓存
	if s.authCacheInvalidator != nil {
		keys, keyErr := s.apiKeyRepo.ListKeysByUserID(ctx, userID)
		if keyErr == nil {
			for _, k := range keys {
				s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, k)
			}
		}
	}

	return &ReplaceUserGroupResult{MigratedKeys: migrated}, nil
}
