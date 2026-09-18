package connection_health

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

type mutableSchedulerPriorityReader struct {
	mu         sync.Mutex
	priority   int
	listCalls  int
	credential upstream.ProbeCredential
}

type failingInventoryMySitesReader struct {
	fakeMySitesReader
	mu    sync.Mutex
	calls int
	err   error
}

type failingInventoryGroupReader struct {
	fakePlatformGroupReader
	mu    sync.Mutex
	calls int
	err   error
}

type panickingInventoryMySitesReader struct {
	fakeMySitesReader
	mu    sync.Mutex
	calls int
}

type blockingInventoryMySitesReader struct {
	fakeMySitesReader
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type inventoryLoadResult struct {
	inventory *adminWorkspaceInventory
	err       error
	attempted bool
}

func (r *failingInventoryGroupReader) FetchAdminAllGroups(upstream.Session) ([]upstream.AdminGroupInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return nil, r.err
}

func (r *failingInventoryGroupReader) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *failingInventoryMySitesReader) RequireSession(context.Context, string, string) (upstream.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.session, r.err
}

func (r *failingInventoryMySitesReader) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *failingInventoryMySitesReader) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *panickingInventoryMySitesReader) RequireSession(context.Context, string, string) (upstream.Session, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	panic("inventory session panic")
}

func (r *panickingInventoryMySitesReader) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *blockingInventoryMySitesReader) RequireSession(context.Context, string, string) (upstream.Session, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.session, nil
}

func (r *blockingInventoryMySitesReader) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestLoadAdminInventory_UsesWorkspaceFailureBackoff(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	sessionErr := errors.New("session unavailable")
	mySites := &failingInventoryMySitesReader{
		fakeMySitesReader: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		err:               sessionErr,
	}
	service := &Service{
		mySites: mySites, platformGroups: fakePlatformGroupReader{}, inventoryNow: func() time.Time { return now },
	}

	load := func(wantAttempt bool, wantCalls int) {
		t.Helper()
		_, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
		if err == nil {
			t.Fatal("expected inventory load failure")
		}
		if attempted != wantAttempt || mySites.callCount() != wantCalls {
			t.Fatalf("attempted=%v calls=%d, want attempted=%v calls=%d", attempted, mySites.callCount(), wantAttempt, wantCalls)
		}
		if !wantAttempt && !errors.Is(err, errAdminInventoryRetryDeferred) {
			t.Fatalf("deferred load error=%v, want %v", err, errAdminInventoryRetryDeferred)
		}
	}

	load(true, 1)
	now = now.Add(2*time.Minute - time.Second)
	load(false, 1)
	now = now.Add(time.Second)
	load(true, 2)
	now = now.Add(5*time.Minute - time.Second)
	load(false, 2)
	now = now.Add(time.Second)
	load(true, 3)
	now = now.Add(10*time.Minute - time.Second)
	load(false, 3)
	now = now.Add(time.Second)
	load(true, 4)
	now = now.Add(10*time.Minute - time.Second)
	load(false, 4)
	now = now.Add(time.Second)
	load(true, 5)
}

func TestLoadAdminInventory_CacheFailureReportsOnlyActualRequest(t *testing.T) {
	mySites := &failingInventoryMySitesReader{err: errors.New("session unavailable")}
	service := &Service{mySites: mySites, platformGroups: fakePlatformGroupReader{}}
	cache := make(adminInventoryCache)

	_, _, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", cache)
	if !attempted {
		t.Fatal("the first load must report the actual upstream request")
	}
	_, _, attempted = service.loadAdminInventory(context.Background(), "user1", "ws1", cache)
	if attempted || mySites.callCount() != 1 {
		t.Fatalf("cached failure must stay quiet, attempted=%v calls=%d", attempted, mySites.callCount())
	}
}

func TestLoadAdminInventory_FetchGroupsFailureBacksOffAcrossTicks(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	groups := &failingInventoryGroupReader{err: errors.New("admin groups unavailable")}
	service := &Service{
		mySites:        fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: groups,
		inventoryNow:   func() time.Time { return now },
	}

	_, _, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if !attempted || groups.callCount() != 1 {
		t.Fatalf("initial group fetch must be attempted once, attempted=%v calls=%d", attempted, groups.callCount())
	}
	now = now.Add(schedulerTickInterval)
	_, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if attempted || !errors.Is(err, errAdminInventoryRetryDeferred) || groups.callCount() != 1 {
		t.Fatalf("next scheduler tick must be deferred, attempted=%v err=%v calls=%d", attempted, err, groups.callCount())
	}
}

func TestLoadAdminInventory_GroupAccountFailureReturnsPartialSnapshotAndBacksOff(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "healthy"}, {ID: "broken-a"}, {ID: "broken-b"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"healthy": {{ID: "account-1"}},
		},
		errByGrp: map[string]error{
			"broken-a": errors.New("accounts unavailable a"),
			"broken-b": errors.New("accounts unavailable b"),
		},
	}
	service := &Service{
		mySites:        fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		platformGroups: reader,
		inventoryNow:   func() time.Time { return now },
	}
	var logs bytes.Buffer
	previousWriter, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	cache := make(adminInventoryCache)
	inventory, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", cache)
	if err != nil || !attempted {
		t.Fatalf("partial inventory must remain usable in the current tick, attempted=%v err=%v", attempted, err)
	}
	if len(inventory.groups) != 3 || len(inventory.groups[0].accounts) != 1 || inventory.groups[0].err != nil {
		t.Fatalf("successful group missing from partial inventory: %+v", inventory.groups)
	}
	if inventory.groups[1].err == nil || inventory.groups[2].err == nil {
		t.Fatalf("failed groups must remain marked in the partial inventory: %+v", inventory.groups)
	}
	if count := strings.Count(logs.String(), "admin inventory group accounts failed"); count != 1 {
		t.Fatalf("account-list failures from one request must produce one aggregate log, count=%d logs=%q", count, logs.String())
	}
	if strings.Contains(strings.TrimSuffix(logs.String(), "\n"), "\n") {
		t.Fatalf("aggregate account-list failure must stay on one physical log line: %q", logs.String())
	}

	cached, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", cache)
	if err != nil || attempted || cached != inventory {
		t.Fatalf("the current tick must reuse its partial snapshot, attempted=%v err=%v inventory=%p want=%p", attempted, err, cached, inventory)
	}
	if count := strings.Count(logs.String(), "admin inventory group accounts failed"); count != 1 {
		t.Fatalf("cached partial inventory must not be logged again, count=%d logs=%q", count, logs.String())
	}

	now = now.Add(schedulerTickInterval)
	_, err, attempted = service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if attempted || !errors.Is(err, errAdminInventoryRetryDeferred) {
		t.Fatalf("the next scheduler tick must defer a partial inventory retry, attempted=%v err=%v", attempted, err)
	}
}

func TestLoadAdminInventory_PanicRecordsBackoffAndRepanics(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	mySites := &panickingInventoryMySitesReader{
		fakeMySitesReader: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
	}
	service := &Service{
		mySites: mySites, platformGroups: fakePlatformGroupReader{}, inventoryNow: func() time.Time { return now },
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	}()
	if recovered != "inventory session panic" {
		t.Fatalf("inventory panic must propagate unchanged, recovered=%v", recovered)
	}

	now = now.Add(schedulerTickInterval)
	_, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if attempted || !errors.Is(err, errAdminInventoryRetryDeferred) || mySites.callCount() != 1 {
		t.Fatalf("panic must release in-flight state into backoff, attempted=%v err=%v calls=%d", attempted, err, mySites.callCount())
	}
}

func TestLoadAdminInventory_SuccessResetsFailureBackoff(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	sessionErr := errors.New("session unavailable")
	mySites := &failingInventoryMySitesReader{
		fakeMySitesReader: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		err:               sessionErr,
	}
	service := &Service{
		mySites: mySites, platformGroups: fakePlatformGroupReader{}, inventoryNow: func() time.Time { return now },
	}

	_, _, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if !attempted {
		t.Fatal("initial failing inventory request was not attempted")
	}
	now = now.Add(adminInventoryRetryDelays[0])
	mySites.setError(nil)
	if _, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache)); err != nil || !attempted {
		t.Fatalf("inventory retry should recover, attempted=%v err=%v", attempted, err)
	}

	mySites.setError(sessionErr)
	_, _, attempted = service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if !attempted {
		t.Fatal("first failure after a success must be attempted immediately")
	}
	now = now.Add(adminInventoryRetryDelays[0])
	_, _, attempted = service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
	if !attempted || mySites.callCount() != 4 {
		t.Fatalf("success must reset the next failure to the two-minute delay, attempted=%v calls=%d", attempted, mySites.callCount())
	}
}

func TestLoadAdminInventory_ConcurrentRequestsUseSingleUpstreamAttempt(t *testing.T) {
	mySites := &blockingInventoryMySitesReader{
		fakeMySitesReader: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		started:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	service := &Service{mySites: mySites, platformGroups: fakePlatformGroupReader{}}
	released := false
	defer func() {
		if !released {
			close(mySites.release)
		}
	}()

	load := func(results chan<- inventoryLoadResult) {
		inventory, err, attempted := service.loadAdminInventory(context.Background(), "user1", "ws1", make(adminInventoryCache))
		results <- inventoryLoadResult{inventory: inventory, err: err, attempted: attempted}
	}
	firstResult := make(chan inventoryLoadResult, 1)
	go load(firstResult)
	select {
	case <-mySites.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first inventory request did not reach the upstream reader")
	}

	secondResult := make(chan inventoryLoadResult, 1)
	go load(secondResult)
	var second inventoryLoadResult
	select {
	case second = <-secondResult:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent inventory request was not deferred")
	}
	if second.attempted || !errors.Is(second.err, errAdminInventoryRetryDeferred) || second.inventory != nil {
		t.Fatalf("concurrent request must be deferred, result=%+v", second)
	}

	close(mySites.release)
	released = true
	var first inventoryLoadResult
	select {
	case first = <-firstResult:
	case <-time.After(2 * time.Second):
		t.Fatal("first inventory request did not finish after release")
	}
	if first.err != nil || !first.attempted || first.inventory == nil || mySites.callCount() != 1 {
		t.Fatalf("exactly one upstream request must complete successfully, result=%+v calls=%d", first, mySites.callCount())
	}
}

func (r *mutableSchedulerPriorityReader) FetchAdminAllGroups(session upstream.Session) ([]upstream.AdminGroupInfo, error) {
	multiplier := 0.5
	return []upstream.AdminGroupInfo{{ID: "g1", Name: "vip", Multiplier: &multiplier}}, nil
}

func (r *mutableSchedulerPriorityReader) ListAdminGroupAccounts(session upstream.Session, group upstream.AdminGroupInfo) ([]upstream.AdminGroupAccountInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	priority := r.priority
	return []upstream.AdminGroupAccountInfo{{
		ID: "acc-1", Name: "account", Status: "active", Priority: &priority, Models: "gpt-4o",
	}}, nil
}

func (r *mutableSchedulerPriorityReader) ResolveProbeCredential(session upstream.Session, account upstream.AdminGroupAccountInfo) (upstream.ProbeCredential, error) {
	return r.credential, nil
}

func (r *mutableSchedulerPriorityReader) setPriority(priority int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.priority = priority
}

func (r *mutableSchedulerPriorityReader) accountListCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls
}

func TestRunSchedulerTick_ManualPriorityChangeDuringProbeBecomesConflict(t *testing.T) {
	reader := &mutableSchedulerPriorityReader{priority: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader.setPriority(23)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	reader.credential = upstream.ProbeCredential{BaseURL: server.URL, Key: "probe-key"}

	repo := newFakeRepository()
	repo.policies = []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		PriorityMode: PriorityModeMultiplier, AutoDegradeEnabled: true,
		FailureThreshold: 3, SuccessThreshold: 2, DailyProbeBudget: 1000,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", ProviderFamily: ProviderOpenAI, Enabled: true, MaxProbeTokens: 1}},
	}}
	repo.groupAssignments = []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "vip", PolicyID: "p1",
	}}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateHealthy, CurrentWeight: 100,
		},
	}
	priorityActions := &fakeTargetPriorityActioner{}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		dispatcher: noopRemoteActionRunner{}, probeRunner: NewRealProbeRunner(),
		platformGroups: reader, priorityActions: priorityActions,
	}

	service.runSchedulerTick(context.Background())

	if len(priorityActions.calls) != 0 {
		t.Fatalf("manual priority change during probe must not be overwritten, got %+v", priorityActions.calls)
	}
	stored, ok := repo.priorityStates["user1|ws1|"+targetID]
	if !ok || !stored.Conflict || stored.LastConflictPriority == nil || *stored.LastConflictPriority != 23 {
		t.Fatalf("manual priority change must be recorded as a conflict, got %+v", stored)
	}
	if calls := reader.accountListCalls(); calls != 2 {
		t.Fatalf("pre-sync and probe collection must share inventory while post-probe sync reloads it, account list calls=%d", calls)
	}
}

func TestRunSchedulerTick_Sub2APIHardFailureLowersPriorityInSameTick(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: true,
		FailureThreshold: 3, SuccessThreshold: 2, DailyProbeBudget: 1000,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", ProviderFamily: ProviderOpenAI, Enabled: true, MaxProbeTokens: 1}},
	}}
	repo.groupAssignments = []GroupPolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", AdminGroupID: "g1", AdminGroupName: "vip", PolicyID: "p1",
	}}
	priority := 7
	reader := fakePlatformGroupReader{
		groups: []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{
			"g1": {{ID: "acc-1", Name: "account", Status: "active", Priority: &priority, Models: "gpt-4o"}},
		},
		credByAccount: map[string]upstream.ProbeCredential{
			"acc-1": {BaseURL: server.URL, Key: "probe-key"},
		},
	}
	priorityActions := &fakeTargetPriorityActioner{}
	statusActions := &fakePlatformActioner{}
	service := &Service{
		repo: repo, mySites: fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}},
		dispatcher: newRemoteActionDispatcher(nil, nil, statusActions), probeRunner: NewRealProbeRunner(),
		platformGroups: reader, priorityActions: priorityActions,
	}

	service.runSchedulerTick(context.Background())

	targetID := "sub2api:ws1:acc-1"
	if state := repo.states[targetID]["gpt-4o"]; state.State != StateSuspended {
		t.Fatalf("hard failure must still suspend local health state, got %+v", state)
	}
	if len(statusActions.sub2APICalls) != 0 {
		t.Fatalf("scheduler must never disable a Sub2API account, got %+v", statusActions.sub2APICalls)
	}
	if len(priorityActions.calls) != 1 || priorityActions.calls[0].targetID != "acc-1" || priorityActions.calls[0].priority != sub2APIBlockedPriority {
		t.Fatalf("hard failure must lower priority in the same tick, got %+v", priorityActions.calls)
	}
	stored, ok := repo.priorityStates["user1|ws1|"+targetID]
	if !ok || stored.OriginalPriority != priority || stored.LastAppliedPriority != sub2APIBlockedPriority {
		t.Fatalf("priority ownership snapshot must preserve the original value, got %+v", stored)
	}
}

func TestIsDue_NeverProbedIsDue(t *testing.T) {
	repo := newFakeRepository()
	svc := &Service{repo: repo}
	if !svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, time.Now()) {
		t.Fatalf("expected never-probed target to be due")
	}
}

func TestIsDue_DisabledNeverDue(t *testing.T) {
	repo := newFakeRepository()
	repo.states["conn-1"] = map[string]ConnectionHealthState{
		"m1": {ConnectionID: "conn-1", ModelName: "m1", State: StateDisabled},
	}
	svc := &Service{repo: repo}
	if svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, time.Now()) {
		t.Fatalf("disabled state must never be due for automatic probing")
	}
}

func TestRecordTargetCredentialUnavailable_PreservesLegacyRemoteAction(t *testing.T) {
	repo := newFakeRepository()
	svc := &Service{repo: repo}
	targetID := "sub2api:ws1:acc-1"
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateSuspended, LastRemoteAction: RemoteActionSub2APIStatusInactive,
		},
	}
	target := AdminProbeTarget{TargetID: targetID, Platform: string(upstream.PlatformSub2API), AccountID: "acc-1"}
	spec := probeModelSpec{modelName: "gpt-4o", policy: Policy{ID: "p1"}}

	svc.recordTargetCredentialUnavailable(context.Background(), "user1", "ws1", target, []probeModelSpec{spec}, upstream.ReasonCredentialUnavailable)
	stored := repo.states[targetID]["gpt-4o"]
	if stored.LastRemoteAction != RemoteActionSub2APIStatusInactive {
		t.Fatalf("credential failure must preserve legacy ownership evidence, got %+v", stored)
	}
}

func TestIsDue_WithinCooldownIsNotDue(t *testing.T) {
	repo := newFakeRepository()
	future := time.Now().Add(1 * time.Minute)
	repo.states["conn-1"] = map[string]ConnectionHealthState{
		"m1": {ConnectionID: "conn-1", ModelName: "m1", State: StateSuspended, CooldownUntil: &future},
	}
	svc := &Service{repo: repo}
	if svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, time.Now()) {
		t.Fatalf("expected target within cooldown to not be due")
	}
}

func TestIsDue_RespectsIntervalAndBackoff(t *testing.T) {
	repo := newFakeRepository()
	now := time.Now()
	recentProbe := now.Add(-10 * time.Second)
	repo.states["conn-1"] = map[string]ConnectionHealthState{
		"m1": {ConnectionID: "conn-1", ModelName: "m1", State: StateHealthy, LastProbeAt: &recentProbe},
	}
	svc := &Service{repo: repo}

	if svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, now) {
		t.Fatalf("expected not due within interval")
	}

	repo.states["conn-1"] = map[string]ConnectionHealthState{
		"m1": {ConnectionID: "conn-1", ModelName: "m1", State: StateDegraded, LastProbeAt: &recentProbe, ConsecutiveFailures: 2},
	}
	if svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, now) {
		t.Fatalf("expected backoff window to still be active 10s after failure")
	}

	longAgo := now.Add(-6 * time.Minute)
	repo.states["conn-1"] = map[string]ConnectionHealthState{
		"m1": {ConnectionID: "conn-1", ModelName: "m1", State: StateDegraded, LastProbeAt: &longAgo, ConsecutiveFailures: 2},
	}
	if !svc.isDue(context.Background(), "conn-1", "m1", Policy{ProbeIntervalSeconds: 60}, now) {
		t.Fatalf("expected due after backoff window elapses")
	}
}

// schedulerReader 构造一个平台读取器：单分组，若干可探活 channel（带 base_url + models）。
func schedulerReader(accountIDs ...string) fakePlatformGroupReader {
	accounts := make([]upstream.AdminGroupAccountInfo, 0, len(accountIDs))
	for _, id := range accountIDs {
		accounts = append(accounts, upstream.AdminGroupAccountInfo{ID: id, Name: "ch-" + id, BaseURL: "https://up", Models: "gpt-4o"})
	}
	return fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": accounts},
	}
}

// TestCollectAdminProbeJobs_GeneratesDueTargets 验证独立探活调度：为可探活、到期（从未探活）的
// 目标模型生成任务，禁用的模型目标不生成任务。
func TestCollectAdminProbeJobs_GeneratesDueTargets(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}

	policies := []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60,
		ModelTargets: []ModelTarget{
			{ModelName: "gpt-4o", Enabled: true},
			{ModelName: "disabled-model", Enabled: false},
		},
	}}
	assignments := []PolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "p1"},
	}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 target job, got %d", len(jobs))
	}
	if jobs[0].target.TargetID != "newapi:ws1:100" {
		t.Fatalf("unexpected targetId: %q", jobs[0].target.TargetID)
	}
	if len(jobs[0].dueSpecs) != 1 || jobs[0].dueSpecs[0].modelName != "gpt-4o" {
		t.Fatalf("expected only enabled gpt-4o due, got %+v", jobs[0].dueSpecs)
	}
}

// TestCollectAdminProbeJobs_UnassignedTargetNeverScheduled 验证核心新语义：即使 workspace 有启用
// 策略且模型能匹配，没有显式分配关系的 target 也绝不会自动探活。
func TestCollectAdminProbeJobs_UnassignedTargetNeverScheduled(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}

	policies := []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60,
		ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}},
	}}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, nil)
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs without any policy assignment, got %d", len(jobs))
	}
}

// TestCollectAdminProbeJobs_MultiplierOnlyNeverScheduled guards the central safety contract:
// even legacy or malformed rows that still contain model targets cannot enter probe scheduling.
func TestCollectAdminProbeJobs_MultiplierOnlyNeverScheduled(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}
	policies := []Policy{{
		ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true,
		StrategyMode: StrategyModeMultiplierOnly, PriorityMode: PriorityModeMultiplier,
		ModelTargets: []ModelTarget{{ModelName: "legacy-model", Enabled: true}},
	}}
	assignments := []PolicyAssignment{{
		UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "p1",
	}}

	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 0 {
		t.Fatalf("multiplier-only policy must never generate probe jobs: %+v", jobs)
	}
}

// TestCollectAdminProbeJobs_AssignmentToDisabledPolicyIgnored 验证分配指向的策略如果已被禁用，
// 该分配不生效（因为 policies 只包含 ListEnabledPolicies 的结果，policyByID 查不到）。
func TestCollectAdminProbeJobs_AssignmentToDisabledPolicyIgnored(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}

	// 模拟调度器视角：runSchedulerTick 只会把 ListEnabledPolicies 的结果传进来，
	// 一条被禁用的策略永远不会出现在 policies 参数里，即使它有分配记录。
	policies := []Policy{}
	assignments := []PolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "disabled-policy"},
	}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 0 {
		t.Fatalf("expected assignment to a disabled/nonexistent policy to be ignored, got %d jobs", len(jobs))
	}
}

// TestCollectAdminProbeJobs_OnlyUsesAssignedPolicies 验证 workspace 下其它启用策略，如果没有
// 分配给某个 target，就不会影响该 target 的候选模型计算——即使那条策略的模型池能匹配上。
func TestCollectAdminProbeJobs_OnlyUsesAssignedPolicies(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}

	policies := []Policy{
		{ID: "assigned", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60,
			ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}},
		{ID: "not-assigned", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60,
			ModelTargets: []ModelTarget{{ModelName: "gpt-4o-mini", Enabled: true}}},
	}
	assignments := []PolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "assigned"},
	}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 target job, got %d", len(jobs))
	}
	for _, spec := range jobs[0].dueSpecs {
		if spec.modelName == "gpt-4o-mini" {
			t.Fatalf("model from unassigned policy must not be scheduled: %+v", jobs[0].dueSpecs)
		}
	}
	if len(jobs[0].dueSpecs) != 1 || jobs[0].dueSpecs[0].modelName != "gpt-4o" {
		t.Fatalf("expected only gpt-4o (from assigned policy) due, got %+v", jobs[0].dueSpecs)
	}
}

// TestCollectAdminProbeJobs_SkipsUnavailableTargets 验证不可探活目标（new-api 缺 base_url）不排期。
func TestCollectAdminProbeJobs_SkipsUnavailableTargets(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "100", Name: "ch", Models: "gpt-4o"}}}, // 无 base_url
	}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: reader}

	policies := []Policy{{ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60, ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}}}
	assignments := []PolicyAssignment{{UserID: "user1", AdminAccountID: "ws1", TargetID: "newapi:ws1:100", PolicyID: "p1"}}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 0 {
		t.Fatalf("expected unavailable target to be skipped, got %d jobs", len(jobs))
	}
}

// TestCollectAdminProbeJobs_CapsAtMaxJobsPerTick 验证单轮到期模型任务总数受 maxJobsPerTick 限制。
func TestCollectAdminProbeJobs_CapsAtMaxJobsPerTick(t *testing.T) {
	repo := newFakeRepository()
	ids := make([]string, 0, maxJobsPerTick+50)
	for i := range maxJobsPerTick + 50 {
		ids = append(ids, fmt.Sprintf("%d", i))
	}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader(ids...)}

	policies := []Policy{{ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60, ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}}}
	assignments := make([]PolicyAssignment, 0, len(ids))
	for _, id := range ids {
		assignments = append(assignments, PolicyAssignment{UserID: "user1", AdminAccountID: "ws1", TargetID: buildTargetID("newapi", "ws1", id), PolicyID: "p1"})
	}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	total := 0
	for _, j := range jobs {
		total += len(j.dueSpecs)
	}
	if total != maxJobsPerTick {
		t.Fatalf("expected due model tasks capped at %d, got %d", maxJobsPerTick, total)
	}
}

// TestCollectAdminProbeJobs_MultiWorkspaceIsolation 验证多 workspace 隔离：每个 workspace 的策略
// 只为自己 workspace 生成目标（targetId 内嵌各自 adminAccountID）。
func TestCollectAdminProbeJobs_MultiWorkspaceIsolation(t *testing.T) {
	repo := newFakeRepository()
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	svc := &Service{repo: repo, mySites: mySites, platformGroups: schedulerReader("100")}

	policies := []Policy{
		{ID: "p1", UserID: "user1", AdminAccountID: "ws1", Enabled: true, ProbeIntervalSeconds: 60, ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}},
		{ID: "p2", UserID: "user1", AdminAccountID: "ws2", Enabled: true, ProbeIntervalSeconds: 60, ModelTargets: []ModelTarget{{ModelName: "gpt-4o", Enabled: true}}},
	}
	assignments := []PolicyAssignment{
		{UserID: "user1", AdminAccountID: "ws1", TargetID: buildTargetID("newapi", "ws1", "100"), PolicyID: "p1"},
		{UserID: "user1", AdminAccountID: "ws2", TargetID: buildTargetID("newapi", "ws2", "100"), PolicyID: "p2"},
	}
	jobs := svc.collectAdminProbeJobs(context.Background(), policies, assignments)
	if len(jobs) != 2 {
		t.Fatalf("expected one job per workspace (2 total), got %d", len(jobs))
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.adminAccountID] = true
		if j.target.TargetID != buildTargetID("newapi", j.adminAccountID, "100") {
			t.Fatalf("target %q does not embed its workspace %q", j.target.TargetID, j.adminAccountID)
		}
	}
	if !seen["ws1"] || !seen["ws2"] {
		t.Fatalf("expected both workspaces scheduled, got %+v", seen)
	}
}
