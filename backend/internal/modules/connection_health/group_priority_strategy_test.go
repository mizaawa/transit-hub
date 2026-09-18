package connection_health

import (
	"context"
	"errors"
	"testing"

	"transithub/backend/internal/modules/upstream"
)

func TestCollectAdminProbeJobs_GroupAssignmentAndExclusion(t *testing.T) {
	repo := newFakeRepository()
	service := &Service{
		repo:           repo,
		mySites:        fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: schedulerReader("100"),
	}
	policies := []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}}
	groupAssignments := []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "vip", PolicyID: "p1",
	}}

	jobs := service.collectAdminProbeJobsWithGroups(context.Background(), policies, nil, groupAssignments, nil)
	if len(jobs) != 1 || jobs[0].target.TargetID != "newapi:ws1:100" {
		t.Fatalf("group policy should auto-include target, got %+v", jobs)
	}

	exclusions := []GroupTargetExclusion{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", TargetID: "newapi:ws1:100",
	}}
	jobs = service.collectAdminProbeJobsWithGroups(context.Background(), policies, nil, groupAssignments, exclusions)
	if len(jobs) != 0 {
		t.Fatalf("excluded target must not inherit group policy, got %+v", jobs)
	}

	// 旧版显式 target 分配优先于分组排除，保证已有线上配置不被新功能一棍子打死。
	explicit := []PolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "p1",
	}}
	jobs = service.collectAdminProbeJobsWithGroups(context.Background(), policies, explicit, groupAssignments, exclusions)
	if len(jobs) != 1 {
		t.Fatalf("legacy explicit assignment must survive group exclusion, got %+v", jobs)
	}
}

func TestCollectAdminProbeJobs_PreservesPolicySourceGroupForSharedTarget(t *testing.T) {
	repo := newFakeRepository()
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "first"}, {ID: "g2", Name: "second"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", BaseURL: "https://up", Models: "model-a,model-b"}},
			"g2": {{ID: "100", BaseURL: "https://up", Models: "model-a,model-b"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader,
	}
	policies := []Policy{
		{ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ModelTargets: []ModelTarget{{ModelName: "model-a", Enabled: true}}},
		{ID: "p2", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ModelTargets: []ModelTarget{{ModelName: "model-b", Enabled: true}}},
	}
	assignments := []GroupPolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "first", PolicyID: "p1"},
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g2", AdminGroupName: "second", PolicyID: "p2"},
	}

	jobs := service.collectAdminProbeJobsWithGroups(context.Background(), policies, nil, assignments, nil)
	if len(jobs) != 1 || len(jobs[0].dueSpecs) != 2 {
		t.Fatalf("shared target should produce one two-model job, got %+v", jobs)
	}
	groupsByModel := make(map[string]string)
	for _, spec := range jobs[0].dueSpecs {
		groupsByModel[spec.modelName] = spec.eventAdminGroupID
	}
	if groupsByModel["model-a"] != "g1" || groupsByModel["model-b"] != "g2" {
		t.Fatalf("each policy event must retain its source group: %+v", groupsByModel)
	}
}

func TestAdminGroups_ReportsInheritedPolicyAndExclusion(t *testing.T) {
	repo := newFakeRepository()
	repo.policies = []Policy{probePolicy()}
	repo.groupAssignments = []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "vip", PolicyID: "policy-1",
	}}
	repo.groupExclusions = []GroupTargetExclusion{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", TargetID: "newapi:ws1:200",
	}}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.5)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {
				{ID: "100", Name: "included", BaseURL: "https://up", Models: "gpt-4o"},
				{ID: "200", Name: "excluded", BaseURL: "https://up", Models: "gpt-4o"},
			},
		},
	}
	service := newAdminGroupsService(reader, fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}, repo)

	groups, err := service.AdminGroups(context.Background(), "user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 || groups[0].MonitoredAccountCount != 1 || groups[0].ExcludedAccountCount != 1 {
		t.Fatalf("unexpected group assignment summary: %+v", groups)
	}
	for _, account := range groups[0].Accounts {
		if account.ID == "100" && (!account.HasAssignedPolicy || account.PolicyAssignmentSource != "group") {
			t.Fatalf("included account should inherit group policy: %+v", account)
		}
		if account.ID == "200" && (!account.ExcludedFromGroupPolicy || account.HasAssignedPolicy) {
			t.Fatalf("excluded account should not inherit group policy: %+v", account)
		}
	}
}

func TestSetAdminGroupPolicyConfiguration_ValidatesAndPersistsScope(t *testing.T) {
	repo := newFakeRepository()
	repo.policies = []Policy{probePolicy()}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", BaseURL: "https://up", Models: "gpt-4o"}},
		},
	}
	service := newAdminGroupsService(reader, fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}, repo)
	targetID := "newapi:ws1:100"

	configuration, err := service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{
		PolicyIDs: []string{"policy-1", "policy-1"}, ExcludedTargetIDs: []string{targetID, targetID},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configuration.PolicyIDs) != 1 || configuration.PolicyIDs[0] != "policy-1" ||
		len(configuration.ExcludedTargetIDs) != 1 || configuration.ExcludedTargetIDs[0] != targetID {
		t.Fatalf("configuration should be deduplicated and persisted: %+v", configuration)
	}

	_, err = service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{
		PolicyIDs: []string{"policy-1"}, ExcludedTargetIDs: []string{"newapi:ws1:not-in-group"},
	})
	if err == nil || err.Error() != ErrorProbeTargetNotFound {
		t.Fatalf("cross-group exclusion must be rejected, got %v", err)
	}
}

func TestSetAdminGroupPolicyConfiguration_QuickPolicyCreatesAndBindsTogether(t *testing.T) {
	repo := newFakeRepository()
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", BaseURL: "https://up", Models: "gpt-4o"}},
		},
	}
	service := newAdminGroupsService(reader, fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}, repo)
	configuration, err := service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{
		QuickPolicy: &PolicyInput{
			Name: "quick", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
			ModelTargets: []ModelTargetInput{{ModelName: "gpt-4o", ProviderFamily: ProviderOpenAI, Enabled: true}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configuration.PolicyIDs) != 1 || len(repo.policies) != 1 || repo.policies[0].ID != configuration.PolicyIDs[0] {
		t.Fatalf("quick policy must be created and bound in one operation: config=%+v policies=%+v", configuration, repo.policies)
	}
}

func TestSetAdminGroupPolicyConfiguration_MultiplierPolicyRequiresGroupMultiplier(t *testing.T) {
	repo := newFakeRepository()
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: nil}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel"}},
		},
	}
	service := newAdminGroupsService(reader, fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}, repo)
	_, err := service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{
		QuickPolicy: &PolicyInput{
			Name: "price only", Enabled: true, StrategyMode: StrategyModeMultiplierOnly,
		},
	})
	if err == nil || err.Error() != ErrorMultiplierRequired {
		t.Fatalf("missing group multiplier must reject multiplier policy, got %v", err)
	}
	if len(repo.policies) != 0 {
		t.Fatalf("rejected multiplier policy must not be persisted: %+v", repo.policies)
	}
}

func TestSetAdminGroupPolicyConfiguration_ReclaimsOnlyConflictedPriorities(t *testing.T) {
	repo := newFakeRepository()
	repo.policies = []Policy{probePolicy()}
	conflictedTarget := "newapi:ws1:100"
	healthyTarget := "newapi:ws1:200"
	repo.priorityStates["user1|ws1|"+conflictedTarget] = PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: conflictedTarget, Conflict: true,
	}
	repo.priorityStates["user1|ws1|"+healthyTarget] = PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: healthyTarget, OriginalPriority: 7, LastAppliedPriority: 40000,
	}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100"}, {ID: "200"}},
		},
	}
	service := newAdminGroupsService(reader, fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}, repo)

	if _, err := service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{PolicyIDs: []string{"policy-1"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := repo.priorityStates["user1|ws1|"+conflictedTarget]; exists {
		t.Fatal("saving group configuration should clear conflicted state so the scheduler can reclaim it")
	}
	if _, exists := repo.priorityStates["user1|ws1|"+healthyTarget]; !exists {
		t.Fatal("non-conflicted state must retain its original priority baseline")
	}
}

type priorityUpdateCall struct {
	targetID string
	priority int
}

type fakeTargetPriorityActioner struct {
	calls []priorityUpdateCall
}

func (f *fakeTargetPriorityActioner) UpdateAdminTargetPriority(session upstream.Session, targetID string, priority int) error {
	f.calls = append(f.calls, priorityUpdateCall{targetID: targetID, priority: priority})
	return nil
}

func TestMultiplierPrioritySyncAndManualConflict(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	accountPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", Priority: &accountPriority, BaseURL: "https://up", Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo:            repo,
		mySites:         fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups:  reader,
		priorityActions: priorityActions,
	}
	policies := []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}}
	groupAssignments := []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "vip", PolicyID: "p1",
	}}

	service.syncMultiplierPriorities(context.Background(), policies, nil, groupAssignments, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority <= accountPriority {
		t.Fatalf("expected lower multiplier to receive managed high priority, calls=%+v", priorityActions.calls)
	}
	stored := repo.priorityStates["user1|ws1|newapi:ws1:100"]
	if stored.OriginalPriority != 7 || stored.LastAppliedPriority != priorityActions.calls[0].priority || stored.Conflict {
		t.Fatalf("unexpected stored priority state: %+v", stored)
	}

	// 模拟管理员在上游把系统写入值手动改为 23；下一轮只能标记冲突，不能再次覆盖。
	manualPriority := 23
	reader.accountsByGrp["g1"] = []upstream.AdminGroupAccountInfo{{
		ID: "100", Name: "channel", Priority: &manualPriority, BaseURL: "https://up", Models: "gpt-4o",
	}}
	service.platformGroups = reader
	service.syncMultiplierPriorities(context.Background(), policies, nil, groupAssignments, nil, []PrioritySyncState{stored})
	if len(priorityActions.calls) != 1 {
		t.Fatalf("manual priority change must not be overwritten, calls=%+v", priorityActions.calls)
	}
	stored = repo.priorityStates["user1|ws1|newapi:ws1:100"]
	if !stored.Conflict || stored.LastConflictPriority == nil || *stored.LastConflictPriority != manualPriority {
		t.Fatalf("manual change should be recorded as conflict: %+v", stored)
	}
}

func TestMultiplierPrioritySync_IgnoresFrozenHealthWhenAutoDegradeDisabled(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	targetID := "newapi:ws1:100"
	currentPriority := 1
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: false, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID,
	}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {ConnectionID: targetID, ModelName: "gpt-4o", State: StateSuspended, CurrentWeight: 0, UserID: "user1", AdminAccountID: "ws1"},
	}
	stored := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalPriority: 7, LastAppliedPriority: currentPriority,
	}
	repo.priorityStates["user1|ws1|"+targetID] = stored

	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	want := desiredManagedPriorityForPlatformWithExpected(upstream.PlatformNewAPI, nil, 0, 0)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != want {
		t.Fatalf("frozen suspended state must not pin multiplier priority, want=%d calls=%+v", want, priorityActions.calls)
	}
}

func TestMultiplierPrioritySync_MultiplierOnlyOverridesOverlappingProbeHealth(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	targetID := "newapi:ws1:100"
	currentPriority := 1
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	healthPolicy := Policy{
		ID: "health", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeHealthProbe, AutoDegradeEnabled: true, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	pricePolicy := Policy{
		ID: "price", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeMultiplierOnly, PriorityMode: PriorityModeMultiplier,
	}
	assignments := []GroupPolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: healthPolicy.ID},
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: pricePolicy.ID},
	}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {ConnectionID: targetID, ModelName: "gpt-4o", State: StateSuspended, CurrentWeight: 0, UserID: "user1", AdminAccountID: "ws1"},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{healthPolicy, pricePolicy}, nil, assignments, nil, nil)
	want := desiredManagedPriorityForPlatformWithExpected(upstream.PlatformNewAPI, nil, 0, 0)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != want {
		t.Fatalf("explicit multiplier-only policy must ignore overlapping health state, want=%d calls=%+v", want, priorityActions.calls)
	}
}

func TestMultiplierPrioritySync_ConfirmsPendingSystemWrite(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	desired := desiredManagedPriorityForPlatform(upstream.PlatformNewAPI, nil, 0)
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Priority: &desired, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, PriorityMode: PriorityModeMultiplier, ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	pending := desired
	stored := PrioritySyncState{UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", OriginalPriority: 7, LastAppliedPriority: 7, PendingPriority: &pending}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, []PrioritySyncState{stored})
	updated := repo.priorityStates["user1|ws1|newapi:ws1:100"]
	if updated.Conflict || updated.PendingPriority != nil || updated.LastAppliedPriority != desired {
		t.Fatalf("pending priority write should be confirmed without conflict: %+v", updated)
	}
	if len(priorityActions.calls) != 0 {
		t.Fatalf("already-applied priority must not be written twice: %+v", priorityActions.calls)
	}
}

func TestMultiplierPrioritySync_DoesNotRestoreWhenInventoryIsIncomplete(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	reader := fakePlatformGroupReader{
		groups:   []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		errByGrp: map[string]error{"g1": errors.New("temporary upstream failure")},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	stored := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100",
		OriginalPriority: 7, LastAppliedPriority: 40999,
	}
	repo.priorityStates["user1|ws1|"+stored.TargetID] = stored

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, []PrioritySyncState{stored})
	if len(priorityActions.calls) != 0 {
		t.Fatalf("incomplete inventory must not restore or rewrite priority: %+v", priorityActions.calls)
	}
	if _, exists := repo.priorityStates["user1|ws1|"+stored.TargetID]; !exists {
		t.Fatal("incomplete inventory must retain the priority checkpoint for the next scan")
	}
}

func TestMultiplierPrioritySync_MissingMultiplierDoesNotWriteOrRestore(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 40999
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: nil}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", Priority: &currentPriority}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		PriorityMode: PriorityModeMultiplier, StrategyMode: StrategyModeMultiplierOnly,
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	stored := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100",
		OriginalPriority: 7, LastAppliedPriority: currentPriority, EffectiveMultiplier: 0.4,
	}
	repo.priorityStates["user1|ws1|"+stored.TargetID] = stored

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, []PrioritySyncState{stored})
	if len(priorityActions.calls) != 0 {
		t.Fatalf("missing multiplier must not write or restore priority: %+v", priorityActions.calls)
	}
	if got, exists := repo.priorityStates["user1|ws1|"+stored.TargetID]; !exists || got.LastAppliedPriority != currentPriority {
		t.Fatalf("missing multiplier must retain the existing sync checkpoint: %+v", got)
	}
}

func TestMultiplierPrioritySync_MissingConflictedTargetIsNotOverwritten(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: fakePlatformGroupReader{}, priorityActions: priorityActions,
	}
	stored := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100",
		OriginalPriority: 7, LastAppliedPriority: 40999, Conflict: true,
	}
	repo.priorityStates["user1|ws1|"+stored.TargetID] = stored

	service.syncMultiplierPriorities(context.Background(), nil, nil, nil, nil, []PrioritySyncState{stored})
	if len(priorityActions.calls) != 0 {
		t.Fatalf("missing target with a manual conflict must not be overwritten: %+v", priorityActions.calls)
	}
	if _, exists := repo.priorityStates["user1|ws1|"+stored.TargetID]; exists {
		t.Fatal("unmanaged conflicted target should release its stale checkpoint without a remote write")
	}
}

func TestSub2APIHealthPrioritySync_BlocksUntilFullyHealthyThenRestores(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Priority: &currentPriority, Models: "model-a,model-b"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true, PriorityMode: PriorityModeNone,
		ModelTargets: []ModelTarget{
			{ModelName: "model-a", Enabled: true},
			{ModelName: "model-b", Enabled: true},
		},
	}
	assignment := GroupPolicyAssignment{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID,
	}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"model-a": {ConnectionID: targetID, ModelName: "model-a", UserID: "user1", AdminAccountID: "ws1", State: StateSuspended, CurrentWeight: 0},
		"model-b": {ConnectionID: targetID, ModelName: "model-b", UserID: "user1", AdminAccountID: "ws1", State: StateHealthy, CurrentWeight: 100},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != sub2APIBlockedPriority {
		t.Fatalf("blocking health must lower Sub2API priority, calls=%+v states=%+v", priorityActions.calls, repo.priorityStates)
	}
	stateKey := "user1|ws1|" + targetID
	stored, ok := repo.priorityStates[stateKey]
	if !ok || stored.OriginalPriority != 7 || stored.LastAppliedPriority != sub2APIBlockedPriority || stored.EffectiveMultiplier != healthPriorityMultiplierSentinel {
		t.Fatalf("unexpected health priority checkpoint: %+v", stored)
	}

	// Recovering is still not safe for full traffic. Keep the account in the
	// blocking priority until every controlled model is healthy.
	currentPriority = sub2APIBlockedPriority
	repo.states[targetID]["model-a"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "model-a", UserID: "user1", AdminAccountID: "ws1", State: StateRecovering, CurrentWeight: 25,
	}
	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 1 {
		t.Fatalf("partially recovered target must remain blocked without another write: %+v", priorityActions.calls)
	}
	stored = repo.priorityStates[stateKey]
	if stored.LastAppliedPriority != sub2APIBlockedPriority || stored.Conflict {
		t.Fatalf("partially recovered checkpoint must remain owned: %+v", stored)
	}

	// A missing controlled-model state is unknown, not healthy. Do not restore
	// traffic merely because the one visible model has recovered.
	repo.states[targetID]["model-a"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "model-a", UserID: "user1", AdminAccountID: "ws1", State: StateHealthy, CurrentWeight: 100,
	}
	delete(repo.states[targetID], "model-b")
	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 1 {
		t.Fatalf("missing controlled model must keep the target blocked: %+v", priorityActions.calls)
	}
	stored = repo.priorityStates[stateKey]

	repo.states[targetID]["model-a"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "model-a", UserID: "user1", AdminAccountID: "ws1", State: StateHealthy, CurrentWeight: 100,
	}
	repo.states[targetID]["model-b"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "model-b", UserID: "user1", AdminAccountID: "ws1", State: StateHealthy, CurrentWeight: 100,
	}
	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 2 || priorityActions.calls[1].priority != 7 {
		t.Fatalf("fully healthy target must restore its original priority, got %+v", priorityActions.calls)
	}
	if _, exists := repo.priorityStates[stateKey]; exists {
		t.Fatal("restored health priority checkpoint must be deleted")
	}
}

func TestSub2APIHealthPrioritySync_PreservesManualPriorityChange(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1", State: StateSuspended, CurrentWeight: 0},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)
	stateKey := "user1|ws1|" + targetID
	stored := repo.priorityStates[stateKey]
	currentPriority = 23
	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 1 {
		t.Fatalf("manual priority change must not be overwritten: %+v", priorityActions.calls)
	}
	stored = repo.priorityStates[stateKey]
	if !stored.Conflict || stored.LastConflictPriority == nil || *stored.LastConflictPriority != currentPriority {
		t.Fatalf("manual priority change must be recorded as a conflict: %+v", stored)
	}

	repo.states[targetID]["gpt-4o"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1", State: StateHealthy, CurrentWeight: 100,
	}
	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 1 {
		t.Fatalf("healthy cleanup must preserve the manual value: %+v", priorityActions.calls)
	}
	if _, exists := repo.priorityStates[stateKey]; exists {
		t.Fatal("resolved conflicted target must release its health priority checkpoint")
	}
}

func TestSub2APIHealthPrioritySync_RemoteActionDisabledDoesNotManagePriority(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: false,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateSuspended, CurrentWeight: 0,
		},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)
	if len(priorityActions.calls) != 0 || len(repo.priorityStates) != 0 {
		t.Fatalf("disabled remote actions must not manage health priority, calls=%+v states=%+v", priorityActions.calls, repo.priorityStates)
	}
}

func TestSub2APIHealthPrioritySync_OverridesMultiplierOnlyUntilFullyHealthy(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	healthPolicy := Policy{
		ID: "health", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeHealthProbe, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	pricePolicy := Policy{
		ID: "price", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeMultiplierOnly, PriorityMode: PriorityModeMultiplier,
	}
	assignments := []GroupPolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: healthPolicy.ID},
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: pricePolicy.ID},
	}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateObserving, CurrentWeight: 0,
		},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{healthPolicy, pricePolicy}, nil, assignments, nil, nil)
	stateKey := "user1|ws1|" + targetID
	stored, ok := repo.priorityStates[stateKey]
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != sub2APIBlockedPriority {
		t.Fatalf("health blocking must override multiplier-only sorting: %+v", priorityActions.calls)
	}
	if !ok || stored.EffectiveMultiplier != healthPriorityMultiplierSentinel || stored.OriginalPriority != 7 {
		t.Fatalf("health override must retain its ownership latch and original priority: %+v", stored)
	}

	currentPriority = sub2APIBlockedPriority
	repo.states[targetID]["gpt-4o"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
		State: StateRecovering, CurrentWeight: 25,
	}
	service.syncMultiplierPriorities(
		context.Background(), []Policy{healthPolicy, pricePolicy}, nil, assignments, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 1 {
		t.Fatalf("recovering target must remain health-blocked: %+v", priorityActions.calls)
	}
	stored = repo.priorityStates[stateKey]
	if stored.EffectiveMultiplier != healthPriorityMultiplierSentinel || stored.LastAppliedPriority != sub2APIBlockedPriority {
		t.Fatalf("recovering target lost the health ownership latch: %+v", stored)
	}

	repo.states[targetID]["gpt-4o"] = ConnectionHealthState{
		ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
		State: StateHealthy, CurrentWeight: 100,
	}
	service.syncMultiplierPriorities(
		context.Background(), []Policy{healthPolicy, pricePolicy}, nil, assignments, nil,
		[]PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 2 || priorityActions.calls[1].priority != 1 {
		t.Fatalf("fully healthy target must release to multiplier sorting: %+v", priorityActions.calls)
	}
	stored = repo.priorityStates[stateKey]
	if stored.EffectiveMultiplier != 0.4 || stored.LastAppliedPriority != 1 || stored.Conflict {
		t.Fatalf("healthy target must return to multiplier ownership: %+v", stored)
	}
}

func TestSub2APIHealthPrioritySync_BlockedPriorityUsesInventoryMaximum(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	targetPriority := 20000
	peerPriority := 35000
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {
				{ID: "acc-1", Priority: &targetPriority, Models: "gpt-4o"},
				{ID: "acc-2", Priority: &peerPriority, Models: "other-model"},
			},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	targetID := "sub2api:ws1:acc-1"
	assignment := PolicyAssignment{UserID: "user1", AdminAccountID: "ws1", TargetID: targetID, PolicyID: policy.ID}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateSuspended, CurrentWeight: 0,
		},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, []PolicyAssignment{assignment}, nil, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].targetID != "acc-1" || priorityActions.calls[0].priority != peerPriority {
		t.Fatalf("blocked priority must not be below the target or inventory maximum: %+v", priorityActions.calls)
	}
	stored := repo.priorityStates["user1|ws1|"+targetID]
	if stored.OriginalPriority != targetPriority || stored.LastAppliedPriority != peerPriority || stored.EffectiveMultiplier != healthPriorityMultiplierSentinel {
		t.Fatalf("unexpected high-priority health checkpoint: %+v", stored)
	}
}

func TestSub2APIHealthPrioritySync_MissingCurrentPriorityHoldsManagement(t *testing.T) {
	tests := []struct {
		name   string
		state  State
		stored *PrioritySyncState
	}{
		{name: "fresh suspended target", state: StateSuspended},
		{
			name:  "existing recovering target",
			state: StateRecovering,
			stored: &PrioritySyncState{
				UserID: "user1", AdminAccountID: "ws1", TargetID: "sub2api:ws1:acc-1",
				OriginalPriority: 7, LastAppliedPriority: sub2APIBlockedPriority,
				EffectiveMultiplier: healthPriorityMultiplierSentinel,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			priorityActions := &fakeTargetPriorityActioner{}
			reader := fakePlatformGroupReader{
				groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: float64Ptr(0.4)}},
				accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
					"g1": {{ID: "acc-1", Priority: nil, Models: "gpt-4o"}},
				},
			}
			service := &Service{
				repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
				platformGroups: reader, priorityActions: priorityActions,
			}
			healthPolicy := Policy{
				ID: "health", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
				StrategyMode: StrategyModeHealthProbe, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
				ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
			}
			pricePolicy := Policy{
				ID: "price", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
				StrategyMode: StrategyModeMultiplierOnly, PriorityMode: PriorityModeMultiplier,
			}
			assignments := []GroupPolicyAssignment{
				{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: healthPolicy.ID},
				{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: pricePolicy.ID},
			}
			targetID := "sub2api:ws1:acc-1"
			repo.states[targetID] = map[string]ConnectionHealthState{
				"gpt-4o": {
					ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
					State: tt.state, CurrentWeight: 0,
				},
			}
			var syncStates []PrioritySyncState
			if tt.stored != nil {
				repo.priorityStates["user1|ws1|"+targetID] = *tt.stored
				syncStates = []PrioritySyncState{*tt.stored}
			}

			service.syncMultiplierPriorities(
				context.Background(), []Policy{healthPolicy, pricePolicy}, nil, assignments, nil, syncStates,
			)
			if len(priorityActions.calls) != 0 {
				t.Fatalf("missing current priority must prevent every write: %+v", priorityActions.calls)
			}
			stored, exists := repo.priorityStates["user1|ws1|"+targetID]
			if tt.stored == nil {
				if exists {
					t.Fatalf("missing current priority must not create a zero-value baseline: %+v", stored)
				}
			} else if !exists || stored.OriginalPriority != tt.stored.OriginalPriority ||
				stored.LastAppliedPriority != tt.stored.LastAppliedPriority ||
				stored.EffectiveMultiplier != tt.stored.EffectiveMultiplier {
				t.Fatalf("existing checkpoint must be held unchanged: exists=%v state=%+v", exists, stored)
			}
		})
	}
}

func TestSub2APIPrioritySync_MissingPriorityDoesNotAffectOtherRanks(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	validPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{
			{ID: "unknown", Name: "unknown", Multiplier: float64Ptr(0.1)},
			{ID: "valid", Name: "valid", Multiplier: float64Ptr(0.4)},
		},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"unknown": {{ID: "acc-1", Priority: nil}},
			"valid":   {{ID: "acc-2", Priority: &validPriority}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "price", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeMultiplierOnly, PriorityMode: PriorityModeMultiplier,
	}
	assignments := []GroupPolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "unknown", PolicyID: policy.ID},
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "valid", PolicyID: policy.ID},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, assignments, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].targetID != "acc-2" || priorityActions.calls[0].priority != 1 {
		t.Fatalf("unreadable target must be excluded from multiplier ranks: %+v", priorityActions.calls)
	}
	if _, exists := repo.priorityStates["user1|ws1|sub2api:ws1:acc-1"]; exists {
		t.Fatal("unreadable target must not create a priority checkpoint")
	}
}

func TestSub2APIHealthPrioritySync_DuplicateGroupUsesVisiblePriority(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "partial", Name: "partial"}, {ID: "managed", Name: "managed"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"partial": {{ID: "acc-1", Priority: nil, Models: "gpt-4o"}},
			"managed": {{ID: "acc-1", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "managed", PolicyID: policy.ID,
	}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateSuspended, CurrentWeight: 0,
		},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != sub2APIBlockedPriority {
		t.Fatalf("a later duplicate row with priority must make health blocking safe: %+v", priorityActions.calls)
	}
	stored := repo.priorityStates["user1|ws1|"+targetID]
	if stored.OriginalPriority != currentPriority {
		t.Fatalf("priority baseline must come from the visible duplicate row: %+v", stored)
	}
}

func TestSub2APIHealthPrioritySync_DoesNotReduceExistingBlockedPriority(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 35000
	peerPriority := 20000
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {
				{ID: "acc-1", Priority: &currentPriority, Models: "gpt-4o"},
				{ID: "acc-2", Priority: &peerPriority, Models: "other-model"},
			},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	targetID := "sub2api:ws1:acc-1"
	assignment := PolicyAssignment{UserID: "user1", AdminAccountID: "ws1", TargetID: targetID, PolicyID: policy.ID}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateRecovering, CurrentWeight: 25,
		},
	}
	stored := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalPriority: 7, LastAppliedPriority: currentPriority,
		EffectiveMultiplier: healthPriorityMultiplierSentinel,
	}
	repo.priorityStates["user1|ws1|"+targetID] = stored

	service.syncMultiplierPriorities(
		context.Background(), []Policy{policy}, []PolicyAssignment{assignment}, nil, nil, []PrioritySyncState{stored},
	)
	if len(priorityActions.calls) != 0 {
		t.Fatalf("a blocked account must not move toward the front when the workspace maximum falls: %+v", priorityActions.calls)
	}
	got := repo.priorityStates["user1|ws1|"+targetID]
	if got.LastAppliedPriority != currentPriority || got.EffectiveMultiplier != healthPriorityMultiplierSentinel {
		t.Fatalf("health ownership must remain at the existing blocked priority: %+v", got)
	}
}

func TestSub2APIHealthPrioritySync_WaitsForCompleteInventory(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	targetPriority := 7
	peerPriority := 35000
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "visible", Name: "visible"}, {ID: "unavailable", Name: "unavailable"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"visible":     {{ID: "acc-1", Priority: &targetPriority, Models: "gpt-4o"}},
			"unavailable": {{ID: "acc-2", Priority: &peerPriority, Models: "other-model"}},
		},
		errByGrp: map[string]error{"unavailable": errors.New("temporary upstream failure")},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	targetID := "sub2api:ws1:acc-1"
	assignment := PolicyAssignment{UserID: "user1", AdminAccountID: "ws1", TargetID: targetID, PolicyID: policy.ID}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateSuspended, CurrentWeight: 0,
		},
	}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, []PolicyAssignment{assignment}, nil, nil, nil)
	if len(priorityActions.calls) != 0 || len(repo.priorityStates) != 0 {
		t.Fatalf("incomplete inventory must defer a new health takeover: calls=%+v states=%+v", priorityActions.calls, repo.priorityStates)
	}

	delete(reader.errByGrp, "unavailable")
	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, []PolicyAssignment{assignment}, nil, nil, nil)
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].priority != peerPriority {
		t.Fatalf("complete inventory must apply the real workspace blocking tier: %+v", priorityActions.calls)
	}
	stored := repo.priorityStates["user1|ws1|"+targetID]
	if stored.OriginalPriority != targetPriority || stored.LastAppliedPriority != peerPriority ||
		stored.EffectiveMultiplier != healthPriorityMultiplierSentinel {
		t.Fatalf("unexpected checkpoint after inventory recovery: %+v", stored)
	}
}

func TestSub2APIRemotePriorityStatus_FreshRecoveringStateBlocks(t *testing.T) {
	policy := Policy{
		ID: "p1", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	item := &priorityTargetInventory{
		target:   AdminProbeTarget{Models: []string{"gpt-4o"}},
		policies: []Policy{policy},
	}
	eligible, allHealthy, shouldBlock := sub2APIRemotePriorityStatus(item, []ConnectionHealthState{{
		ModelName: "gpt-4o", State: StateRecovering, CurrentWeight: 25,
	}})
	if !eligible || allHealthy || !shouldBlock {
		t.Fatalf("a recovering target must acquire health priority ownership immediately: eligible=%v allHealthy=%v shouldBlock=%v", eligible, allHealthy, shouldBlock)
	}
}

func TestDesiredManagedPriority_HealthAlwaysBeatsMultiplier(t *testing.T) {
	healthyExpensive := desiredManagedPriority([]ConnectionHealthState{{State: StateHealthy, CurrentWeight: 100}}, 20)
	degradedCheap := desiredManagedPriority([]ConnectionHealthState{{State: StateDegraded, CurrentWeight: 75}}, 0)
	if healthyExpensive <= degradedCheap {
		t.Fatalf("health tier must outrank price: healthy=%d degraded=%d", healthyExpensive, degradedCheap)
	}
	cheap := desiredManagedPriority([]ConnectionHealthState{{State: StateHealthy, CurrentWeight: 100}}, 0)
	expensive := desiredManagedPriority([]ConnectionHealthState{{State: StateHealthy, CurrentWeight: 100}}, 1)
	if cheap <= expensive {
		t.Fatalf("within same health tier lower multiplier must rank higher: cheap=%d expensive=%d", cheap, expensive)
	}
}

func TestDesiredManagedPriority_MissingModelIsUnconfigured(t *testing.T) {
	healthy := []ConnectionHealthState{{State: StateHealthy, CurrentWeight: 100}}
	score := desiredManagedPriorityForPlatformWithExpected(upstream.PlatformNewAPI, healthy, 0, 2)
	expected := 10000 + 999
	if score != expected {
		t.Fatalf("one healthy and one unprobed model must use the unconfigured tier: got %d want %d", score, expected)
	}
	suspended := []ConnectionHealthState{{State: StateSuspended, CurrentWeight: 0}}
	if got := desiredManagedPriorityForPlatformWithExpected(upstream.PlatformNewAPI, suspended, 0, 2); got != 1 {
		t.Fatalf("known suspended model must remain the lowest tier even with missing siblings: %d", got)
	}
}

func TestMultiplierPrioritySync_IgnoresExcludedGroupMultiplier(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	priority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{
			{ID: "managed", Name: "managed", Multiplier: float64Ptr(2)},
			{ID: "excluded", Name: "excluded", Multiplier: float64Ptr(0.1)},
		},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"managed":  {{ID: "100", Priority: &priority, Models: "gpt-4o"}},
			"excluded": {{ID: "100", Priority: &priority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignments := []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "managed", AdminGroupName: "managed", PolicyID: "p1",
	}}
	exclusions := []GroupTargetExclusion{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "excluded", TargetID: "newapi:ws1:100",
	}}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, assignments, exclusions, nil)
	stored := repo.priorityStates["user1|ws1|newapi:ws1:100"]
	if stored.EffectiveMultiplier != 2 {
		t.Fatalf("excluded group multiplier must not leak into priority, got %+v", stored)
	}
}

func TestMultiplierPrioritySync_ExplicitTargetSurvivesGroupExclusion(t *testing.T) {
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	priority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "excluded", Name: "excluded", Multiplier: float64Ptr(0.1)}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"excluded": {{ID: "100", Priority: &priority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups: reader, priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	targetID := "newapi:ws1:100"
	exclusions := []GroupTargetExclusion{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "excluded", TargetID: targetID,
	}}
	targetAssignments := []PolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID, PolicyID: policy.ID,
	}}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, targetAssignments, nil, exclusions, nil)
	stored := repo.priorityStates["user1|ws1|"+targetID]
	if stored.EffectiveMultiplier != 0.1 || len(priorityActions.calls) != 1 {
		t.Fatalf("explicit target multiplier should survive group exclusion, stored=%+v calls=%+v", stored, priorityActions.calls)
	}
}

func TestDesiredManagedPriority_UsesPlatformPriorityDirection(t *testing.T) {
	healthy := []ConnectionHealthState{{State: StateHealthy, CurrentWeight: 100}}
	newAPICheap := desiredManagedPriorityForPlatform(upstream.PlatformNewAPI, healthy, 0)
	newAPIExpensive := desiredManagedPriorityForPlatform(upstream.PlatformNewAPI, healthy, 1)
	if newAPICheap <= newAPIExpensive {
		t.Fatalf("NewAPI must use a larger priority for the lower multiplier: cheap=%d expensive=%d", newAPICheap, newAPIExpensive)
	}
	sub2APICheap := desiredManagedPriorityForPlatform(upstream.PlatformSub2API, healthy, 0)
	sub2APIExpensive := desiredManagedPriorityForPlatform(upstream.PlatformSub2API, healthy, 1)
	if sub2APICheap >= sub2APIExpensive {
		t.Fatalf("Sub2API must use a smaller priority for the lower multiplier: cheap=%d expensive=%d", sub2APICheap, sub2APIExpensive)
	}
}

func TestDesiredManagedPriority_Sub2APIUsesCompactStateBands(t *testing.T) {
	tests := []struct {
		name           string
		states         []ConnectionHealthState
		rank           int
		expectedModels int
		want           int
	}{
		{name: "multiplier only best", rank: 0, expectedModels: 0, want: 1},
		{name: "multiplier only second", rank: 1, expectedModels: 0, want: 2},
		{name: "healthy third", states: []ConnectionHealthState{{State: StateHealthy}}, rank: 2, expectedModels: 1, want: 3},
		{name: "recovering", states: []ConnectionHealthState{{State: StateRecovering}}, rank: 0, expectedModels: 1, want: 10},
		{name: "degraded", states: []ConnectionHealthState{{State: StateDegraded}}, rank: 0, expectedModels: 1, want: 100},
		{name: "observing remains blocked", states: []ConnectionHealthState{{State: StateObserving}}, rank: 1, expectedModels: 1, want: 10000},
		{name: "missing model", states: []ConnectionHealthState{{State: StateHealthy}}, rank: 0, expectedModels: 2, want: 1000},
		{name: "suspended", states: []ConnectionHealthState{{State: StateSuspended}}, rank: 0, expectedModels: 1, want: 10000},
		{name: "disabled outranks missing", states: []ConnectionHealthState{{State: StateDisabled}}, rank: 0, expectedModels: 2, want: 10000},
		{name: "healthy rank stays in band", states: []ConnectionHealthState{{State: StateHealthy}}, rank: 99, expectedModels: 1, want: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := desiredManagedPriorityForPlatformWithExpected(upstream.PlatformSub2API, tt.states, tt.rank, tt.expectedModels)
			if got != tt.want {
				t.Fatalf("unexpected Sub2API priority: got %d want %d", got, tt.want)
			}
		})
	}
}

func TestFilterToAssignedTargetEvents_SameNameGroupExclusionDoesNotHideOtherAssignment(t *testing.T) {
	repo := newFakeRepository()
	repo.groupAssignments = []GroupPolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "same", PolicyID: "p1"},
		{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g2", AdminGroupName: "same", PolicyID: "p1"},
	}
	targetID := "newapi:ws1:100"
	repo.groupExclusions = []GroupTargetExclusion{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", TargetID: targetID,
	}}
	service := &Service{repo: repo}
	events, err := service.filterToAssignedTargetEvents(context.Background(), "user1", "ws1", []ConnectionHealthEvent{{
		ConnectionID: targetID, OwnGroupName: "same",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("assignment from the non-excluded same-name group should retain the event, got %+v", events)
	}
}

func TestFilterToAssignedTargetEvents_DropsUnassignedAdminGroupEvent(t *testing.T) {
	repo := newFakeRepository()
	service := &Service{repo: repo}
	targetID := "newapi:ws1:100"
	events, err := service.filterToAssignedTargetEvents(context.Background(), "user1", "ws1", []ConnectionHealthEvent{{
		ConnectionID: targetID, AdminGroupID: "removed", OwnGroupName: "removed",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("event from an unassigned admin group must be filtered, got %+v", events)
	}
}

func TestFilterToAssignedTargetEvents_KeepsUnmanagedRestoreAudit(t *testing.T) {
	repo := newFakeRepository()
	service := &Service{repo: repo}
	targetID := "newapi:ws1:100"
	events, err := service.filterToAssignedTargetEvents(context.Background(), "user1", "ws1", []ConnectionHealthEvent{{
		ConnectionID: targetID, Result: "policy_unmanaged_restore",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("automatic restore must remain visible after the final policy is unbound: %+v", events)
	}
}

func TestFilterToAssignedTargetEvents_UsesPolicyForLegacyWrongGroupMetadata(t *testing.T) {
	repo := newFakeRepository()
	repo.groupAssignments = []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g2", AdminGroupName: "second", PolicyID: "p2",
	}}
	service := &Service{repo: repo}
	targetID := "newapi:ws1:100"
	events, err := service.filterToAssignedTargetEvents(context.Background(), "user1", "ws1", []ConnectionHealthEvent{{
		ConnectionID: targetID, PolicyID: "p2", AdminGroupID: "removed-g1", OwnGroupName: "first",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("legacy event must follow its still-assigned policy when stored group metadata is stale: %+v", events)
	}
}

func float64Ptr(value float64) *float64 { return &value }
