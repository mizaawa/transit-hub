package connection_health

import (
	"context"
	"log"
	"sort"

	"transithub/backend/internal/modules/upstream"
)

const (
	// Sub2API 数值越小越优先。该值只是健康阻断档的下限；实际写入值还必须
	// 不小于当前 workspace inventory 中的最大 priority，避免反向提升故障账号。
	sub2APIBlockedPriority = 10000
	// PrioritySyncState 原本只存储倍率值。负值用作健康降级接管的内部标记，
	// 不会作为倍率暴露给前端，也不参与排序。
	healthPriorityMultiplierSentinel = -1
)

// TargetPriorityActioner 是倍率排序策略对 upstream 模块的唯一写依赖。真实实现根据 session
// 平台更新 New API channel 或 Sub2API account 的 priority，并由 upstream 模块保证字段级写入安全。
type TargetPriorityActioner interface {
	UpdateAdminTargetPriority(session upstream.Session, targetID string, priority int) error
}

type priorityTargetInventory struct {
	target           AdminProbeTarget
	account          upstream.AdminGroupAccountInfo
	policies         []Policy
	multipliers      []float64
	manualMultiplier *float64
	currentPriority  int
}

// syncMultiplierPriorities 同步上游优先级。普通倍率策略仍然「健康优先、倍率次之」，
// 仅倍率策略完全忽略探活状态。Sub2API 的健康远端动作也在这里处理：
// 本来应关闭的账号只降到最低 priority，不修改 active/inactive。
func (s *Service) syncMultiplierPriorities(
	ctx context.Context,
	policies []Policy,
	targetAssignments []PolicyAssignment,
	groupAssignments []GroupPolicyAssignment,
	exclusions []GroupTargetExclusion,
	allSyncStates []PrioritySyncState,
) {
	s.syncMultiplierPrioritiesWithCache(ctx, policies, targetAssignments, groupAssignments, exclusions, allSyncStates, make(adminInventoryCache))
}

func (s *Service) syncMultiplierPrioritiesWithCache(
	ctx context.Context,
	policies []Policy,
	targetAssignments []PolicyAssignment,
	groupAssignments []GroupPolicyAssignment,
	exclusions []GroupTargetExclusion,
	allSyncStates []PrioritySyncState,
	inventoryCache adminInventoryCache,
) {
	if s.priorityActions == nil || s.platformGroups == nil {
		return
	}

	assignedTargets := assignedEnabledPoliciesByTarget(policies, targetAssignments)
	assignedGroups := assignedEnabledPoliciesByGroup(policies, groupAssignments)
	excluded := groupTargetExclusionIndex(exclusions)
	statesByWorkspace := make(map[string][]PrioritySyncState)
	workspaceIdentity := make(map[string][2]string)
	for _, state := range allSyncStates {
		key := state.UserID + "|" + state.AdminAccountID
		statesByWorkspace[key] = append(statesByWorkspace[key], state)
		workspaceIdentity[key] = [2]string{state.UserID, state.AdminAccountID}
	}
	for _, policy := range policies {
		key := policy.UserID + "|" + policy.AdminAccountID
		workspaceIdentity[key] = [2]string{policy.UserID, policy.AdminAccountID}
	}
	for _, assignment := range targetAssignments {
		key := assignment.UserID + "|" + assignment.AdminAccountID
		workspaceIdentity[key] = [2]string{assignment.UserID, assignment.AdminAccountID}
	}
	for _, assignment := range groupAssignments {
		key := assignment.UserID + "|" + assignment.AdminAccountID
		workspaceIdentity[key] = [2]string{assignment.UserID, assignment.AdminAccountID}
	}

	for workspaceKey, identity := range workspaceIdentity {
		userID, adminAccountID := identity[0], identity[1]
		inventorySnapshot, err, attempted := s.loadAdminInventory(ctx, userID, adminAccountID, inventoryCache)
		if err != nil {
			if attempted {
				log.Printf("[connection-health] priority sync load admin inventory failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
			}
			continue
		}
		session := inventorySnapshot.session
		manualMultipliers, multiplierErr := s.loadAccountMultiplierOverrides(ctx, userID, adminAccountID)
		if multiplierErr != nil {
			// An unreadable override set is not equivalent to an empty set: falling
			// back to group values here could overwrite a manually managed priority.
			log.Printf("[connection-health] priority sync account multiplier read failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, multiplierErr)
			continue
		}
		// The upstream API-key group is an automatic fallback for targets whose
		// admin group has no multiplier. Keep this lookup best-effort, matching the
		// read-only health aggregation; manual values still work when the optional
		// upstream metadata cannot be resolved.
		upstreamKeyGroups := s.upstreamKeyGroupsByAdminAccount(ctx, userID, adminAccountID, string(session.Platform))
		inventory, inventoryComplete, err := s.priorityInventoryForSnapshotWithSources(
			inventorySnapshot,
			adminAccountID,
			assignedTargets[workspaceKey],
			assignedGroups[workspaceKey],
			excluded[workspaceKey],
			manualMultipliers,
			upstreamKeyGroups,
		)
		if err != nil {
			log.Printf("[connection-health] priority sync inventory failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
			continue
		}
		states, err := s.repo.ListStatesByWorkspace(ctx, userID, adminAccountID)
		if err != nil {
			log.Printf("[connection-health] priority sync list health states failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
			continue
		}
		s.syncWorkspacePriorities(ctx, session, userID, adminAccountID, inventory, inventoryComplete, states, statesByWorkspace[workspaceKey])
	}
}

func (s *Service) priorityInventoryForSnapshot(
	snapshot *adminWorkspaceInventory,
	adminAccountID string,
	targetPolicies map[string][]Policy,
	groupPolicies map[string][]Policy,
	excludedByGroup map[string]map[string]bool,
) (map[string]*priorityTargetInventory, bool, error) {
	return s.priorityInventoryForSnapshotWithOverrides(snapshot, adminAccountID, targetPolicies, groupPolicies, excludedByGroup, nil)
}

func (s *Service) priorityInventoryForSnapshotWithOverrides(
	snapshot *adminWorkspaceInventory,
	adminAccountID string,
	targetPolicies map[string][]Policy,
	groupPolicies map[string][]Policy,
	excludedByGroup map[string]map[string]bool,
	manualMultipliers map[string]float64,
) (map[string]*priorityTargetInventory, bool, error) {
	return s.priorityInventoryForSnapshotWithSources(
		snapshot,
		adminAccountID,
		targetPolicies,
		groupPolicies,
		excludedByGroup,
		manualMultipliers,
		nil,
	)
}

func (s *Service) priorityInventoryForSnapshotWithSources(
	snapshot *adminWorkspaceInventory,
	adminAccountID string,
	targetPolicies map[string][]Policy,
	groupPolicies map[string][]Policy,
	excludedByGroup map[string]map[string]bool,
	manualMultipliers map[string]float64,
	upstreamKeyGroups map[string]upstreamKeyGroupInfo,
) (map[string]*priorityTargetInventory, bool, error) {
	session := snapshot.session
	platform := string(session.Platform)
	inventory := make(map[string]*priorityTargetInventory)
	inventoryComplete := true
	for _, groupInventory := range snapshot.groups {
		group := groupInventory.group
		if groupInventory.err != nil {
			// 单个分组失败不阻断其它分组排序；目标如果只存在于失败分组，本轮保持原值。
			inventoryComplete = false
			continue
		}
		for _, account := range groupInventory.accounts {
			targetID := buildTargetID(platform, adminAccountID, account.ID)
			item := inventory[targetID]
			if item == nil {
				item = &priorityTargetInventory{
					target: AdminProbeTarget{
						TargetID: targetID, Platform: platform, AdminGroupID: group.ID, AdminGroupName: group.Name,
						AccountID: account.ID, AccountName: account.Name, AccountStatus: account.Status, AccountWeight: cloneIntPointer(account.Weight),
						ProviderFamily: account.Platform, Models: splitModelList(account.Models),
					},
					account: account,
				}
				if account.Priority != nil {
					item.currentPriority = *account.Priority
				}
				inventory[targetID] = item
			} else if item.account.Priority == nil && account.Priority != nil {
				// The same account can be returned by several groups. A partial row must
				// not hide a usable current priority from another occurrence.
				item.account.Priority = account.Priority
				item.currentPriority = *account.Priority
			}
			if manualMultiplier := accountMultiplierForTarget(manualMultipliers, targetID); manualMultiplier != nil {
				// The same target can appear in several groups. Keep one validated manual
				// value and ignore group-derived values for every occurrence.
				item.manualMultiplier = manualMultiplier
			}
			inherited := groupPolicies[group.ID]
			excluded := excludedByGroup[group.ID][targetID]
			if excluded {
				inherited = nil
			}
			// 倍率只来自目标实际参与策略继承的分组。先前在排除判断前收集倍率，会让已排除
			// 或无倍率策略的其它成员分组错误地压低当前目标优先级。
			explicitMultiplier := hasMultiplierPriorityPolicy(targetPolicies[targetID])
			inheritedMultiplier := !excluded && hasMultiplierPriorityPolicy(inherited)
			if item.manualMultiplier == nil && group.Multiplier != nil && (explicitMultiplier || inheritedMultiplier) {
				item.multipliers = append(item.multipliers, *group.Multiplier)
			}
			item.policies = mergePoliciesByID(item.policies, targetPolicies[targetID], inherited)
		}
	}
	// Use the upstream API-key group only when no applicable admin-group value
	// was collected. This preserves the existing admin-group precedence while
	// making upstream-only targets sortable and consistent with the merged UI
	// value.
	for _, item := range inventory {
		if item.manualMultiplier != nil || len(item.multipliers) > 0 {
			continue
		}
		upstreamGroup, ok := upstreamKeyGroups[item.account.ID]
		if !ok || upstreamGroup.multiplier == nil || !validAccountMultiplier(*upstreamGroup.multiplier) {
			continue
		}
		item.multipliers = append(item.multipliers, *upstreamGroup.multiplier)
	}
	return inventory, inventoryComplete, nil
}

func (s *Service) syncWorkspacePriorities(
	ctx context.Context,
	session upstream.Session,
	userID string,
	adminAccountID string,
	inventory map[string]*priorityTargetInventory,
	inventoryComplete bool,
	healthStates []ConnectionHealthState,
	syncStates []PrioritySyncState,
) {
	statesByTarget := make(map[string][]ConnectionHealthState)
	for _, state := range healthStates {
		if _, isTarget := parseTargetID(state.ConnectionID); isTarget {
			statesByTarget[state.ConnectionID] = append(statesByTarget[state.ConnectionID], state)
		}
	}

	managed := make(map[string]*priorityTargetInventory)
	healthBlocked := make(map[string]struct{})
	missingMultiplier := make(map[string]struct{})
	distinctMultipliers := make([]float64, 0)
	seenMultipliers := make(map[float64]struct{})
	for targetID, item := range inventory {
		if session.Platform == upstream.PlatformSub2API && item.account.Priority == nil {
			// A target we cannot safely snapshot must not affect the rank assigned
			// to other targets in the same workspace either.
			missingMultiplier[targetID] = struct{}{}
			continue
		}
		if !hasMultiplierPriorityPolicy(item.policies) {
			continue
		}
		if item.manualMultiplier != nil {
			item.multipliers = []float64{*item.manualMultiplier}
		} else if len(item.multipliers) == 0 {
			// 分组没有返回倍率时进入等待态：既不猜测 1x，也不把已接管目标恢复成旧优先级。
			// 保留同步快照后，倍率恢复可见时下一轮会从原状态继续安全同步。
			missingMultiplier[targetID] = struct{}{}
			continue
		}
		multiplier := minFloat(item.multipliers)
		item.multipliers = []float64{multiplier}
		managed[targetID] = item
		if _, exists := seenMultipliers[multiplier]; !exists {
			seenMultipliers[multiplier] = struct{}{}
			distinctMultipliers = append(distinctMultipliers, multiplier)
		}
	}
	sort.Float64s(distinctMultipliers)
	multiplierRank := make(map[float64]int, len(distinctMultipliers))
	for rank, multiplier := range distinctMultipliers {
		multiplierRank[multiplier] = rank
	}

	storedByTarget := make(map[string]PrioritySyncState, len(syncStates))
	for _, state := range syncStates {
		storedByTarget[state.TargetID] = state
	}
	blockedPriority := sub2APIBlockedPriority
	if session.Platform == upstream.PlatformSub2API {
		for _, item := range inventory {
			if item.account.Priority != nil && *item.account.Priority > blockedPriority {
				blockedPriority = *item.account.Priority
			}
		}
		for _, stored := range syncStates {
			if stored.EffectiveMultiplier == healthPriorityMultiplierSentinel && stored.LastAppliedPriority > blockedPriority {
				// Never move an already blocked account back toward the front merely
				// because another account's high priority disappeared or decreased.
				blockedPriority = stored.LastAppliedPriority
			}
		}
	}

	// Sub2API 分组探活不再关闭账号。健康阻断独立于倍率排序并拥有更高优先级：
	// 一旦进入观察、恢复或阻断状态，保持最低调度档直到全部受控模型恢复健康。
	if session.Platform == upstream.PlatformSub2API {
		for targetID, item := range inventory {
			eligible, allHealthy, shouldBlock := sub2APIRemotePriorityStatus(item, statesByTarget[targetID])
			stored, exists := storedByTarget[targetID]
			wasHealthBlocked := exists && stored.EffectiveMultiplier == healthPriorityMultiplierSentinel
			if item.account.Priority == nil {
				// Without the upstream value there is no safe original-value snapshot or
				// manual-conflict check. Hold both multiplier and health management.
				delete(managed, targetID)
				missingMultiplier[targetID] = struct{}{}
				continue
			}
			if !inventoryComplete && (wasHealthBlocked || (eligible && shouldBlock)) {
				// A partial workspace cannot establish the true lowest scheduling tier.
				// Preserve any existing checkpoint and wait for a complete inventory.
				delete(managed, targetID)
				missingMultiplier[targetID] = struct{}{}
				continue
			}
			if !eligible {
				if wasHealthBlocked {
					// 远端动作被关闭或解绑后，不能让倍率缺失等待态把健康阻断永久保留。
					delete(missingMultiplier, targetID)
				}
				continue
			}
			if shouldBlock || (wasHealthBlocked && !allHealthy) {
				managed[targetID] = item
				healthBlocked[targetID] = struct{}{}
				delete(missingMultiplier, targetID)
				continue
			}
			if allHealthy && wasHealthBlocked {
				// 倍率来源恰好在恢复期间消失时，也不能让故障档 10000 永久残留。
				delete(missingMultiplier, targetID)
			}
		}
	}

	for targetID, item := range managed {
		multiplier := float64(healthPriorityMultiplierSentinel)
		desired := blockedPriority
		if _, blockedByHealth := healthBlocked[targetID]; !blockedByHealth {
			multiplier = item.multipliers[0]
			activeModels := make(map[string]struct{})
			if !hasMultiplierOnlyPolicy(item.policies) {
				for _, spec := range candidateModelSpecs(item.target.Models, item.policies) {
					// 关闭自动降级后模型状态不会继续推进，因此不能让历史 suspended/degraded
					// 状态永久影响倍率排序。倍率本身继续生效，但健康层级回到未配置档。
					if spec.policy.AutoDegradeEnabled {
						activeModels[spec.modelName] = struct{}{}
					}
				}
			}
			activeStates := make([]ConnectionHealthState, 0, len(activeModels))
			for _, state := range statesByTarget[targetID] {
				if _, active := activeModels[state.ModelName]; active {
					activeStates = append(activeStates, state)
				}
			}
			desired = desiredManagedPriorityForPlatformWithExpected(
				session.Platform, activeStates, multiplierRank[multiplier], len(activeModels),
			)
			if session.Platform == upstream.PlatformSub2API && desired == sub2APIBlockedPriority {
				desired = blockedPriority
			}
		}
		stored, exists := storedByTarget[targetID]
		if !exists {
			stored = PrioritySyncState{
				UserID: userID, AdminAccountID: adminAccountID, TargetID: targetID,
				OriginalPriority: item.currentPriority, LastAppliedPriority: item.currentPriority,
			}
		}
		if stored.Conflict {
			continue
		}
		if stored.PendingPriority != nil && item.currentPriority == *stored.PendingPriority {
			stored.LastAppliedPriority = *stored.PendingPriority
			stored.PendingPriority = nil
		}
		if exists && item.currentPriority != stored.LastAppliedPriority && stored.PendingPriority == nil {
			current := item.currentPriority
			stored.Conflict = true
			stored.LastConflictPriority = &current
			stored.EffectiveMultiplier = multiplier
			if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
				log.Printf("[connection-health] priority conflict state save failed target_id=%s err=%v", targetID, err)
			}
			continue
		}
		if exists && stored.PendingPriority != nil && item.currentPriority != stored.LastAppliedPriority {
			current := item.currentPriority
			stored.Conflict = true
			stored.PendingPriority = nil
			stored.LastConflictPriority = &current
			stored.EffectiveMultiplier = multiplier
			if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
				log.Printf("[connection-health] priority pending conflict state save failed target_id=%s err=%v", targetID, err)
			}
			continue
		}
		if item.currentPriority != desired {
			pending := desired
			stored.PendingPriority = &pending
			stored.EffectiveMultiplier = multiplier
			if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
				log.Printf("[connection-health] priority sync intent save failed target_id=%s err=%v", targetID, err)
				continue
			}
			if err := s.priorityActions.UpdateAdminTargetPriority(session, item.target.AccountID, desired); err != nil {
				log.Printf("[connection-health] priority sync update failed target_id=%s err=%v", targetID, err)
				continue
			}
		}
		stored.LastAppliedPriority = desired
		stored.PendingPriority = nil
		stored.EffectiveMultiplier = multiplier
		stored.Conflict = false
		stored.LastConflictPriority = nil
		if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
			log.Printf("[connection-health] priority sync state save failed target_id=%s err=%v", targetID, err)
		}
	}

	// 不再被任何倍率策略覆盖的目标恢复接管前优先级。若管理员已经人工改过，则保留人工值。
	for targetID, stored := range storedByTarget {
		if _, stillManaged := managed[targetID]; stillManaged {
			continue
		}
		if _, waitingForMultiplier := missingMultiplier[targetID]; waitingForMultiplier {
			continue
		}
		item := inventory[targetID]
		if item == nil {
			if !inventoryComplete {
				// 分组读取失败时无法证明目标已经消失，保留当前优先级和同步快照，
				// 等下一次完整扫描再决定是否恢复。
				continue
			}
			if stored.Conflict {
				// 已确认目标不再受策略管理，但人工修改过的值不能被原始快照覆盖。
				if err := s.repo.DeletePrioritySyncState(ctx, userID, adminAccountID, targetID); err != nil {
					log.Printf("[connection-health] missing conflicted target priority state delete failed target_id=%s err=%v", targetID, err)
				}
				continue
			}
			parsed, ok := parseTargetID(targetID)
			if !ok || parsed.adminAccountID != adminAccountID || parsed.platform != string(session.Platform) {
				continue
			}
			pending := stored.OriginalPriority
			stored.PendingPriority = &pending
			if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
				log.Printf("[connection-health] missing target priority restore intent save failed target_id=%s err=%v", targetID, err)
				continue
			}
			if err := s.priorityActions.UpdateAdminTargetPriority(session, parsed.accountID, stored.OriginalPriority); err != nil {
				log.Printf("[connection-health] missing target priority restore failed target_id=%s err=%v", targetID, err)
				continue
			}
			if err := s.repo.DeletePrioritySyncState(ctx, userID, adminAccountID, targetID); err != nil {
				log.Printf("[connection-health] missing target priority state delete failed target_id=%s err=%v", targetID, err)
			}
			continue
		}
		if stored.PendingPriority != nil && item.currentPriority == *stored.PendingPriority {
			stored.LastAppliedPriority = *stored.PendingPriority
			stored.PendingPriority = nil
		}
		if !stored.Conflict && item.currentPriority == stored.LastAppliedPriority && item.currentPriority != stored.OriginalPriority {
			pending := stored.OriginalPriority
			stored.PendingPriority = &pending
			if err := s.repo.UpsertPrioritySyncState(ctx, stored); err != nil {
				log.Printf("[connection-health] priority restore intent save failed target_id=%s err=%v", targetID, err)
				continue
			}
			if err := s.priorityActions.UpdateAdminTargetPriority(session, item.target.AccountID, stored.OriginalPriority); err != nil {
				log.Printf("[connection-health] priority restore failed target_id=%s err=%v", targetID, err)
				continue
			}
		}
		if err := s.repo.DeletePrioritySyncState(ctx, userID, adminAccountID, targetID); err != nil {
			log.Printf("[connection-health] priority sync state delete failed target_id=%s err=%v", targetID, err)
		}
	}
}

// desiredManagedPriorityForPlatform 按平台真实语义计算优先级：NewAPI 沿用「分数越高越优先」；
// Sub2API 使用紧凑的小数值状态分段，数值越小越优先。
func desiredManagedPriorityForPlatform(platform upstream.Platform, states []ConnectionHealthState, multiplierRank int) int {
	if platform == upstream.PlatformSub2API {
		return desiredSub2APIManagedPriority(states, multiplierRank, len(states))
	}
	return desiredManagedPriority(states, multiplierRank)
}

func desiredManagedPriorityForPlatformWithExpected(platform upstream.Platform, states []ConnectionHealthState, multiplierRank int, expectedModels int) int {
	if platform == upstream.PlatformSub2API {
		return desiredSub2APIManagedPriority(states, multiplierRank, expectedModels)
	}
	score := desiredManagedPriority(states, multiplierRank)
	if len(states) < expectedModels && score != 1 {
		// Missing model states are unconfigured, not healthy. A known suspended/disabled
		// state remains the lowest tier even when another model has not been probed yet.
		priceScore := maxInt(0, 999-multiplierRank)
		score = 10000 + priceScore
	}
	return score
}

// desiredSub2APIManagedPriority 使用 Sub2API「数值越小越优先」的原生语义，并为不同健康
// 状态预留互不重叠的区间：健康 1-9、恢复中 10-99、降级 100-999、待配置
// 1000-9999、暂停/禁用 10000。同一状态内 multiplierRank 越小，priority 越小。
// rank 超出区间容量时在区间末尾并列，避免价格排序跨越健康状态边界。
func desiredSub2APIManagedPriority(states []ConnectionHealthState, multiplierRank int, expectedModels int) int {
	for _, state := range states {
		if state.State == StateDisabled || state.State == StateSuspended || state.State == StateObserving {
			return sub2APIBlockedPriority
		}
	}
	if len(states) < expectedModels {
		return sub2APIPriorityWithinBand(1000, 10000, multiplierRank)
	}

	base, nextBase := 1, 10
	for _, state := range states {
		switch state.State {
		case StateDegraded, StateObserving:
			base, nextBase = 100, 1000
		case StateRecovering:
			if base < 10 {
				base, nextBase = 10, 100
			}
		}
	}
	return sub2APIPriorityWithinBand(base, nextBase, multiplierRank)
}

// sub2APIRemotePriorityStatus 把当前会真正启用远端动作的模型聚合为账号级决策。
// 候选策略的去重/安全优先级与 reconcileTargetRemoteAction 完全一致：同一模型
// 被一条关闭远端动作的策略覆盖时，不能被另一条策略绕过保护。
func sub2APIRemotePriorityStatus(item *priorityTargetInventory, states []ConnectionHealthState) (eligible bool, allHealthy bool, shouldBlock bool) {
	controlledModels := make(map[string]struct{})
	for _, spec := range candidateModelSpecs(item.target.Models, item.policies) {
		if policyRemoteActionEnabled(spec.policy) {
			controlledModels[spec.modelName] = struct{}{}
		}
	}
	if len(controlledModels) == 0 {
		return false, false, false
	}

	seen := make(map[string]struct{}, len(controlledModels))
	allHealthy = true
	for _, state := range states {
		if _, controlled := controlledModels[state.ModelName]; !controlled {
			continue
		}
		seen[state.ModelName] = struct{}{}
		if state.State != StateHealthy {
			allHealthy = false
		}
		if state.State == StateSuspended || state.State == StateObserving || state.State == StateRecovering || state.State == StateDisabled || state.CurrentWeight <= 0 {
			shouldBlock = true
		}
	}
	if len(seen) != len(controlledModels) {
		allHealthy = false
	}
	return true, allHealthy, shouldBlock
}

func sub2APIPriorityWithinBand(base int, nextBase int, multiplierRank int) int {
	offset := maxInt(0, multiplierRank)
	return base + minInt(offset, nextBase-base-1)
}

func hasMultiplierPriorityPolicy(policies []Policy) bool {
	for _, policy := range policies {
		if policy.Enabled && normalizePriorityMode(policy.PriorityMode) == PriorityModeMultiplier {
			return true
		}
	}
	return false
}

// hasMultiplierOnlyPolicy 让明确的仅倍率策略成为同一目标的优先级依据。即使目标还叠加了
// 一条负责记录健康状态的探活策略，健康状态也不会重新参与 priority 排名。
func hasMultiplierOnlyPolicy(policies []Policy) bool {
	for _, policy := range policies {
		if policy.Enabled && normalizeStrategyMode(policy.StrategyMode) == StrategyModeMultiplierOnly {
			return true
		}
	}
	return false
}

func minFloat(values []float64) float64 {
	minValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
	}
	return minValue
}

// desiredManagedPriority 计算平台无关的路由分数，并使用互不重叠的区间保证健康状态始终压过价格：
// healthy > recovering > degraded/observing > unconfigured > suspended/disabled。
// 同一健康层级内，倍率排名越靠前（倍率越低）分数越大；平台数值方向由上层映射。
func desiredManagedPriority(states []ConnectionHealthState, multiplierRank int) int {
	priceScore := 999 - multiplierRank
	if priceScore < 0 {
		priceScore = 0
	}
	if len(states) == 0 {
		return 10000 + priceScore
	}

	base := 40000
	weight := 100
	for _, state := range states {
		if state.CurrentWeight < weight {
			weight = state.CurrentWeight
		}
		switch state.State {
		case StateDisabled, StateSuspended:
			return 1
		case StateDegraded, StateObserving:
			if base > 20000 {
				base = 20000
			}
		case StateRecovering:
			if base > 30000 {
				base = 30000
			}
		}
	}
	if base == 30000 {
		base += maxInt(0, minInt(100, weight)) * 50
	} else if base == 20000 {
		base += maxInt(0, minInt(100, weight)) * 10
	}
	return base + priceScore
}
