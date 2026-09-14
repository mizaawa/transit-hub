package connection_health

import (
	"context"
	"math"
	"testing"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

func float64PtrForMultiplierTest(value float64) *float64 {
	return &value
}

func TestResolveAccountMultiplierManualOverridePrecedence(t *testing.T) {
	manual := float64PtrForMultiplierTest(0.75)
	effective := float64PtrForMultiplierTest(1.2)
	group := float64PtrForMultiplierTest(0.9)
	upstream := float64PtrForMultiplierTest(0.42)

	got := resolveAccountMultiplier(manual, effective, group, upstream)
	if got == nil || *got != *manual {
		t.Fatalf("manual multiplier should take precedence: got %v, want %v", got, *manual)
	}
}

func TestResolveAccountMultiplierSkipsInvalidValues(t *testing.T) {
	negative := float64PtrForMultiplierTest(-1)
	nan := float64PtrForMultiplierTest(math.NaN())
	inf := float64PtrForMultiplierTest(math.Inf(1))
	fallback := float64PtrForMultiplierTest(0)

	got := resolveAccountMultiplier(negative, nan, inf, fallback)
	if got == nil || *got != 0 {
		t.Fatalf("zero is a valid fallback after invalid values: got %v", got)
	}

	if got = resolveAccountMultiplier(negative, nan, inf); got != nil {
		t.Fatalf("invalid-only multiplier sources should resolve to nil, got %v", *got)
	}
}

func TestResolveAccountMultiplierDoesNotInferLegacyAccountRate(t *testing.T) {
	// The legacy Sub2API account rate_multiplier is intentionally not passed to
	// this resolver: only the merged UI sources may determine accountMultiplier.
	if got := resolveAccountMultiplier(nil, nil, nil, nil); got != nil {
		t.Fatalf("missing group and upstream multipliers should remain unknown, got %v", *got)
	}
}

// multiplierTestRepository adds only the optional override methods to the shared
// health fake. This keeps the production repository contract exercised without
// changing the behavior of older tests that intentionally omit override support.
type multiplierTestRepository struct {
	*fakeRepository
	overrides map[string]float64
}

func (r *multiplierTestRepository) ListAccountMultiplierOverrides(ctx context.Context, userID string, adminAccountID string) ([]AccountMultiplierOverride, error) {
	rows := make([]AccountMultiplierOverride, 0, len(r.overrides))
	for targetID, multiplier := range r.overrides {
		rows = append(rows, AccountMultiplierOverride{UserID: userID, AdminAccountID: adminAccountID, TargetID: targetID, Multiplier: multiplier})
	}
	return rows, nil
}

func (r *multiplierTestRepository) GetAccountMultiplierOverride(ctx context.Context, userID string, adminAccountID string, targetID string) (*AccountMultiplierOverride, error) {
	value, ok := r.overrides[targetID]
	if !ok {
		return nil, nil
	}
	return &AccountMultiplierOverride{UserID: userID, AdminAccountID: adminAccountID, TargetID: targetID, Multiplier: value}, nil
}

func (r *multiplierTestRepository) UpsertAccountMultiplierOverride(ctx context.Context, override AccountMultiplierOverride) error {
	r.overrides[override.TargetID] = override.Multiplier
	return nil
}

func (r *multiplierTestRepository) DeleteAccountMultiplierOverride(ctx context.Context, userID string, adminAccountID string, targetID string) error {
	delete(r.overrides, targetID)
	return nil
}

func TestMultiplierPrioritySyncUsesManualAccountMultiplierWithoutGroupValue(t *testing.T) {
	const targetID = "newapi:ws1:100"
	repo := &multiplierTestRepository{
		fakeRepository: newFakeRepository(),
		overrides:      map[string]float64{targetID: 0.25},
	}
	priorityActions := &fakeTargetPriorityActioner{}
	currentPriority := 1
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "manual-only", Multiplier: nil}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo:            repo,
		mySites:         fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		platformGroups:  reader,
		priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)

	if len(priorityActions.calls) != 1 {
		t.Fatalf("manual multiplier should make a target sortable even without group multiplier, calls=%+v", priorityActions.calls)
	}
	state, ok := repo.priorityStates["user1|ws1|"+targetID]
	if !ok || state.EffectiveMultiplier != 0.25 {
		t.Fatalf("manual multiplier should be persisted as effective priority value, state=%+v", state)
	}
}

func TestMultiplierPrioritySyncUsesUpstreamKeyMultiplierWhenGroupValueMissing(t *testing.T) {
	const targetID = "newapi:ws1:100"
	currentPriority := 1
	repo := newFakeRepository()
	priorityActions := &fakeTargetPriorityActioner{}
	mySites := fakeAdminGroupKeyReader{
		fakeMySitesReader: fakeMySitesReader{
			session: upstream.Session{Platform: upstream.PlatformNewAPI},
			connections: []my_sites.RealConnection{{
				UserID:                  "user1",
				WorkspaceAdminAccountID: "ws1",
				UpstreamSiteID:          "site-1",
				UpstreamKeyID:           "key-1",
				AdminAccountID:          "100",
				AdminPlatform:           string(upstream.PlatformNewAPI),
			}},
		},
		keysBySite: map[string][]upstream.Sub2APIKeyItem{
			"site-1": {{ID: "key-1", GroupID: "upstream-1", GroupName: "vip"}},
		},
	}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "manual-fallback", Multiplier: nil}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel", Priority: &currentPriority, Models: "gpt-4o"}},
		},
	}
	service := &Service{
		repo:            repo,
		mySites:         mySites,
		sites:           fakeSiteLookup{site: &upstream.Site{ID: "site-1", Metrics: upstream.Metrics{Groups: []upstream.GroupInfo{{ID: "upstream-1", Name: "vip", Multiplier: float64Ptr(0.4)}}}}},
		platformGroups:  reader,
		priorityActions: priorityActions,
	}
	policy := Policy{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}
	assignment := GroupPolicyAssignment{UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", PolicyID: policy.ID}

	service.syncMultiplierPriorities(context.Background(), []Policy{policy}, nil, []GroupPolicyAssignment{assignment}, nil, nil)

	if len(priorityActions.calls) != 1 {
		t.Fatalf("upstream key multiplier should make a target sortable without group multiplier, calls=%+v", priorityActions.calls)
	}
	state, ok := repo.priorityStates["user1|ws1|"+targetID]
	if !ok || state.EffectiveMultiplier != 0.4 {
		t.Fatalf("upstream key multiplier should be persisted as effective priority value, state=%+v", state)
	}
}

func TestGroupMultiplierPolicyAllowsManualTargetMultiplierWithoutGroupValue(t *testing.T) {
	baseRepo := newFakeRepository()
	const targetID = "newapi:ws1:100"
	repo := &multiplierTestRepository{
		fakeRepository: baseRepo,
		overrides:      map[string]float64{targetID: 0.6},
	}
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "manual-only", Multiplier: nil}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "100", Name: "channel"}},
		},
	}
	service := &Service{
		repo:           repo,
		mySites:        fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}},
		accounts:       fakeAdminAccountResolver{id: "ws1"},
		dispatcher:     noopRemoteActionRunner{},
		probeRunner:    NewRealProbeRunner(),
		platformGroups: reader,
	}

	configuration, err := service.SetAdminGroupPolicyConfiguration(context.Background(), "user1", "g1", AdminGroupPolicyConfigurationInput{
		QuickPolicy: &PolicyInput{
			Name: "manual multiplier", Enabled: true, StrategyMode: StrategyModeMultiplierOnly,
		},
	})
	if err != nil {
		t.Fatalf("manual target multiplier should satisfy multiplier policy: %v", err)
	}
	if len(configuration.PolicyIDs) != 1 || len(baseRepo.policies) != 1 {
		t.Fatalf("expected quick policy to be persisted, configuration=%+v policies=%+v", configuration, baseRepo.policies)
	}
}
