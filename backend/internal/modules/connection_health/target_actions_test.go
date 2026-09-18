package connection_health

import (
	"context"
	"testing"

	"transithub/backend/internal/modules/upstream"
)

func TestReconcileTargetRemoteAction_Sub2APINeverWritesStatusOrCreatesSnapshot(t *testing.T) {
	tests := []struct {
		name          string
		state         State
		weight        int
		accountStatus string
	}{
		{name: "healthy", state: StateHealthy, weight: 100, accountStatus: "active"},
		{name: "degraded", state: StateDegraded, weight: 75, accountStatus: "active"},
		{name: "recovering", state: StateRecovering, weight: 25, accountStatus: "active"},
		{name: "observing", state: StateObserving, weight: 0, accountStatus: "active"},
		{name: "suspended", state: StateSuspended, weight: 0, accountStatus: "active"},
		{name: "disabled", state: StateDisabled, weight: 0, accountStatus: "active"},
		{name: "already inactive", state: StateSuspended, weight: 0, accountStatus: "inactive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			platform := &fakePlatformActioner{}
			service := &Service{repo: repo, dispatcher: newRemoteActionDispatcher(nil, nil, platform)}
			targetID := "sub2api:ws1:acc-1"
			repo.states[targetID] = map[string]ConnectionHealthState{
				"model-a": {ConnectionID: targetID, ModelName: "model-a", State: tt.state, CurrentWeight: tt.weight},
			}
			policy := Policy{ID: "p1", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true}
			target := AdminProbeTarget{
				TargetID: targetID, Platform: string(upstream.PlatformSub2API),
				AccountID: "acc-1", AccountStatus: tt.accountStatus,
			}

			action, err := service.reconcileTargetRemoteAction(
				context.Background(), "user1", "ws1", upstream.Session{Platform: upstream.PlatformSub2API},
				target, []probeModelSpec{{modelName: "model-a", policy: policy}},
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action != "" || len(platform.sub2APICalls) != 0 {
				t.Fatalf("Sub2API reconcile must not write account status, action=%q calls=%+v", action, platform.sub2APICalls)
			}
			if len(repo.targetActionStates) != 0 {
				t.Fatalf("Sub2API reconcile must not create a status snapshot: %+v", repo.targetActionStates)
			}
		})
	}
}

func TestReconcileTargetRemoteAction_DoesNotTrustLegacyDisableWithoutSnapshot(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	service := &Service{repo: repo, dispatcher: newRemoteActionDispatcher(nil, nil, platform)}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", State: StateSuspended, CurrentWeight: 0,
			LastRemoteAction: RemoteActionSub2APIStatusInactive,
		},
	}
	target := AdminProbeTarget{
		TargetID: targetID, Platform: string(upstream.PlatformSub2API), AccountID: "acc-1", AccountStatus: "inactive",
	}
	policy := Policy{ID: "p1", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true}

	action, err := service.reconcileTargetRemoteAction(
		context.Background(), "user1", "ws1", upstream.Session{Platform: upstream.PlatformSub2API},
		target, []probeModelSpec{{modelName: "gpt-4o", policy: policy}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "" || len(platform.sub2APICalls) != 0 {
		t.Fatalf("stale legacy evidence must not override a potentially manual inactive status, action=%q calls=%+v", action, platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("legacy repair must not create a new status ownership snapshot: %+v", repo.targetActionStates)
	}
}

func TestReconcileTargetRemoteAction_RestoresOriginalNewAPIWeight(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	service := &Service{repo: repo, dispatcher: newRemoteActionDispatcher(nil, nil, platform)}
	targetID := "newapi:ws1:100"
	originalWeight, appliedWeight, currentWeight := 37, 25, 25
	repo.states[targetID] = map[string]ConnectionHealthState{
		"model-a": {ConnectionID: targetID, ModelName: "model-a", State: StateHealthy, CurrentWeight: 100},
	}
	repo.targetActionStates["user1|ws1|"+targetID] = TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "1", OriginalWeight: &originalWeight, LastAppliedStatus: "1", LastAppliedWeight: &appliedWeight,
	}
	policy := Policy{ID: "p1", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true}
	target := AdminProbeTarget{
		TargetID: targetID, Platform: string(upstream.PlatformNewAPI), AccountID: "100",
		AccountStatus: "1", AccountWeight: &currentWeight,
	}

	action, err := service.reconcileTargetRemoteAction(context.Background(), "user1", "ws1", upstream.Session{Platform: upstream.PlatformNewAPI}, target, []probeModelSpec{{modelName: "model-a", policy: policy}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "newapi_channel_weight_37" || len(platform.calls) != 1 || platform.calls[0].weight != 37 || platform.calls[0].status != 1 {
		t.Fatalf("expected exact original weight restore, action=%q calls=%+v", action, platform.calls)
	}
	if _, exists := repo.targetActionStates["user1|ws1|"+targetID]; exists {
		t.Fatal("action snapshot should be removed after the original state is restored")
	}
}

func TestReconcileTargetRemoteAction_ScalesNewAPIWeightFromOriginal(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	service := &Service{repo: repo, dispatcher: newRemoteActionDispatcher(nil, nil, platform)}
	targetID := "newapi:ws1:100"
	originalWeight, currentWeight := 37, 37
	repo.states[targetID] = map[string]ConnectionHealthState{
		"model-a": {ConnectionID: targetID, ModelName: "model-a", State: StateDegraded, CurrentWeight: 75},
	}
	policy := Policy{ID: "p1", Enabled: true, AutoDegradeEnabled: true, AutoRemoteActionEnabled: true}
	target := AdminProbeTarget{
		TargetID: targetID, Platform: string(upstream.PlatformNewAPI), AccountID: "100",
		AccountStatus: "1", AccountWeight: &currentWeight,
	}

	action, err := service.reconcileTargetRemoteAction(context.Background(), "user1", "ws1", upstream.Session{Platform: upstream.PlatformNewAPI}, target, []probeModelSpec{{modelName: "model-a", policy: policy}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "" || len(platform.calls) != 0 {
		t.Fatalf("ordinary degraded state should not take over an unmanaged target: action=%q calls=%+v", action, platform.calls)
	}

	appliedWeight := 0
	repo.targetActionStates["user1|ws1|"+targetID] = TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "1", OriginalWeight: &originalWeight, LastAppliedStatus: "2", LastAppliedWeight: &appliedWeight,
	}
	target.AccountStatus = "2"
	target.AccountWeight = &appliedWeight
	action, err = service.reconcileTargetRemoteAction(context.Background(), "user1", "ws1", upstream.Session{Platform: upstream.PlatformNewAPI}, target, []probeModelSpec{{modelName: "model-a", policy: policy}})
	if err != nil {
		t.Fatalf("unexpected managed recovery error: %v", err)
	}
	if action != "newapi_channel_weight_28" || len(platform.calls) != 1 || platform.calls[0].weight != 28 {
		t.Fatalf("75%% recovery of original weight 37 must write 28, action=%q calls=%+v", action, platform.calls)
	}
}

func TestRestoreUnmanagedTargetActions_RestoresLegacySub2APIStatusExactlyOnce(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Status: "inactive", Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, dispatcher: newRemoteActionDispatcher(nil, nil, platform),
	}
	targetID := "sub2api:ws1:acc-1"
	stored := TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "active", LastAppliedStatus: "inactive",
	}
	repo.targetActionStates["user1|ws1|"+targetID] = stored

	states, err := repo.ListAllTargetActionStates(context.Background())
	if err != nil {
		t.Fatalf("list legacy snapshots: %v", err)
	}
	service.restoreUnmanagedTargetActions(context.Background(), nil, nil, nil, nil, states, nil, make(adminInventoryCache))
	if len(platform.sub2APICalls) != 1 || platform.sub2APICalls[0].accountID != "acc-1" || platform.sub2APICalls[0].status != stored.OriginalStatus {
		t.Fatalf("legacy snapshot must restore its exact original status once: %+v", platform.sub2APICalls)
	}
	if _, exists := repo.targetActionStates["user1|ws1|"+targetID]; exists {
		t.Fatal("restored legacy snapshot must be deleted")
	}
	if len(repo.events) != 1 || repo.events[0].Result != "policy_unmanaged_restore" || repo.events[0].RemoteAction != RemoteActionSub2APIStatusActive {
		t.Fatalf("restore should be traceable in events: %+v", repo.events)
	}

	states, err = repo.ListAllTargetActionStates(context.Background())
	if err != nil {
		t.Fatalf("list snapshots after restore: %v", err)
	}
	service.restoreUnmanagedTargetActions(context.Background(), nil, nil, nil, nil, states, nil, make(adminInventoryCache))
	if len(platform.sub2APICalls) != 1 || len(repo.events) != 1 {
		t.Fatalf("deleted snapshot must not restore again, calls=%+v events=%+v", platform.sub2APICalls, repo.events)
	}
}

func TestRestoreUnmanagedTargetActions_DoesNotOverwriteManualSub2APIStatusConflict(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Status: "active", Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, dispatcher: newRemoteActionDispatcher(nil, nil, platform),
	}
	targetID := "sub2api:ws1:acc-1"
	stored := TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "active", LastAppliedStatus: "inactive",
	}
	repo.targetActionStates["user1|ws1|"+targetID] = stored

	service.restoreUnmanagedTargetActions(context.Background(), nil, nil, nil, nil, []TargetActionState{stored}, nil, make(adminInventoryCache))
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("manual status change must not be overwritten: %+v", platform.sub2APICalls)
	}
	got, exists := repo.targetActionStates["user1|ws1|"+targetID]
	if !exists || !got.Conflict || got.PendingStatus != "" {
		t.Fatalf("manual status change must retain a conflict snapshot: exists=%v state=%+v", exists, got)
	}
	if len(repo.events) != 0 {
		t.Fatalf("manual conflict must not emit a restore event: %+v", repo.events)
	}
}

func TestRestoreUnmanagedTargetActions_RestoresWhenAutoDegradeDisabled(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Status: "inactive", Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, dispatcher: newRemoteActionDispatcher(nil, nil, platform),
	}
	targetID := "sub2api:ws1:acc-1"
	stored := TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "active", LastAppliedStatus: "inactive",
	}
	repo.targetActionStates["user1|ws1|"+targetID] = stored
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: false, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID,
	}

	service.restoreUnmanagedTargetActions(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]TargetActionState{stored}, nil, make(adminInventoryCache),
	)
	if len(platform.sub2APICalls) != 1 || platform.sub2APICalls[0].status != "active" {
		t.Fatalf("turning off auto degrade must release the captured upstream state: %+v", platform.sub2APICalls)
	}
	if _, exists := repo.targetActionStates["user1|ws1|"+targetID]; exists {
		t.Fatal("restored target must release its action snapshot")
	}
}

func TestRestoreUnmanagedTargetActions_DefersLegacySub2APIStatusUntilPriorityIsBlocked(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	blockedPriority := sub2APIBlockedPriority
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Status: "inactive", Priority: &blockedPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader, dispatcher: newRemoteActionDispatcher(nil, nil, platform),
	}
	targetID := "sub2api:ws1:acc-1"
	stored := TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "active", LastAppliedStatus: "inactive",
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {ConnectionID: targetID, ModelName: "gpt-4o", State: StateSuspended, CurrentWeight: 0},
	}

	service.restoreUnmanagedTargetActions(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]TargetActionState{stored}, nil, make(adminInventoryCache),
	)
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("legacy status must stay inactive until priority takeover succeeds: %+v", platform.sub2APICalls)
	}

	priorityState := PrioritySyncState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalPriority: 7, LastAppliedPriority: sub2APIBlockedPriority,
		EffectiveMultiplier: healthPriorityMultiplierSentinel,
	}
	blockedPriority = 23
	service.restoreUnmanagedTargetActions(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]TargetActionState{stored}, []PrioritySyncState{priorityState}, make(adminInventoryCache),
	)
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("a current priority that differs from the checkpoint must defer legacy restore: %+v", platform.sub2APICalls)
	}

	blockedPriority = sub2APIBlockedPriority
	service.restoreUnmanagedTargetActions(
		context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil,
		[]TargetActionState{stored}, []PrioritySyncState{priorityState}, make(adminInventoryCache),
	)
	if len(platform.sub2APICalls) != 1 || platform.sub2APICalls[0].status != "active" {
		t.Fatalf("confirmed priority takeover should permit one legacy active restore: %+v", platform.sub2APICalls)
	}
}

func TestRestoreUnmanagedTargetActions_DoesNotRestoreInvisibleSub2APITarget(t *testing.T) {
	repo := newFakeRepository()
	platform := &fakePlatformActioner{}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: fakePlatformGroupReader{}, dispatcher: newRemoteActionDispatcher(nil, nil, platform),
	}
	targetID := "sub2api:ws1:acc-1"
	stored := TargetActionState{
		UserID: "user1", AdminAccountID: "ws1", TargetID: targetID,
		OriginalStatus: "active", LastAppliedStatus: "inactive",
	}
	repo.targetActionStates["user1|ws1|"+targetID] = stored

	service.restoreUnmanagedTargetActions(context.Background(), nil, nil, nil, nil, []TargetActionState{stored}, nil, make(adminInventoryCache))
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("an invisible Sub2API target has no current status for conflict detection: %+v", platform.sub2APICalls)
	}
	if _, exists := repo.targetActionStates["user1|ws1|"+targetID]; !exists {
		t.Fatal("invisible target must retain its checkpoint until it can be checked safely")
	}
}
