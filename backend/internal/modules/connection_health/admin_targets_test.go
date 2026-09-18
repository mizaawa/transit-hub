package connection_health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

// newAdminTargetsRemoteActionService 构造一个用真实 remoteActionDispatcher（而不是
// noopRemoteActionRunner）驱动的 Service，供本文件测试断言 probeTargetOnce 触发的真实远端动作调用。
func newAdminTargetsRemoteActionService(reader PlatformGroupReader, mySites MySitesReader, repo *fakeRepository, platform *fakePlatformActioner) *Service {
	return &Service{
		repo:           repo,
		mySites:        mySites,
		accounts:       fakeAdminAccountResolver{id: "ws1"},
		dispatcher:     newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform),
		probeRunner:    NewRealProbeRunner(),
		platformGroups: reader,
	}
}

// sub2APIProbePolicy 返回一条启用策略：自动降级开启，自动远端动作按参数控制。
func sub2APIProbePolicy(autoRemoteAction bool) Policy {
	return Policy{
		ID: "policy-1", UserID: "user1", AdminAccountID: "ws1", Name: "p", Enabled: true, DailyProbeBudget: 1000,
		AutoDegradeEnabled: true, AutoRemoteActionEnabled: autoRemoteAction,
		FailureThreshold: 3, SuccessThreshold: 2, CooldownSeconds: 300, ObservationSeconds: 300, RecoveryStepPercent: 25,
		ModelTargets: []ModelTarget{{ID: "t1", PolicyID: "policy-1", ModelName: "gpt-4o", ProviderFamily: ProviderOpenAI, Enabled: true, MaxProbeTokens: 1}},
	}
}

// AutoRemoteActionEnabled=true 时，Sub2API target 遇到硬失败仍推进本地状态机，
// 但分组监控不得修改账号 status；priority 降级由独立同步器负责。
func TestProbeTargetOnce_Sub2APIHardFailureNeverDisablesAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(true)}
	platform := &fakePlatformActioner{}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "acc-1", Name: "acc", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"acc-1": {BaseURL: server.URL, Key: "k"}},
	}
	svc := newAdminTargetsRemoteActionService(reader, mySites, repo, platform)

	targetID := "sub2api:ws1:acc-1"
	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateSuspended {
		t.Fatalf("expected hard failure to suspend immediately, got %+v", results)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API hard failure must not change account status, got %+v", platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("Sub2API priority degradation must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != "" {
		t.Fatalf("state must not record a Sub2API status action, got %q", st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != "" {
		t.Fatalf("event must not record a Sub2API status action, got %+v", repo.events)
	}
}

// 从 observing 达到成功阈值时仍进入 recovering，但恢复流程同样不得改写账号 status。
func TestProbeTargetOnce_Sub2APIRestoreNeverChangesAccountStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(true)}
	targetID := "sub2api:ws1:acc-1"
	observingUntil := time.Now().Add(-1 * time.Minute)
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateObserving, ConsecutiveSuccesses: 1, ObservingUntil: &observingUntil, CurrentWeight: 0,
		},
	}
	platform := &fakePlatformActioner{}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "acc-1", Name: "acc", Status: "active", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"acc-1": {BaseURL: server.URL, Key: "k"}},
	}
	svc := newAdminTargetsRemoteActionService(reader, mySites, repo, platform)

	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateRecovering {
		t.Fatalf("expected transition to recovering after success threshold, got %+v", results)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API recovery must not change account status, got %+v", platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("Sub2API recovery must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != "" {
		t.Fatalf("state must not record a Sub2API status action, got %q", st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != "" {
		t.Fatalf("event must not record a Sub2API status action, got %+v", repo.events)
	}
}

// 即使注入的 status API 会失败，硬失败探活也不能触达该 API。
func TestProbeTargetOnce_Sub2APIHardFailureDoesNotReachStatusAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(true)}
	platform := &fakePlatformActioner{sub2APIErr: errors.New("upstream 500")}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "acc-1", Name: "acc", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"acc-1": {BaseURL: server.URL, Key: "k"}},
	}
	svc := newAdminTargetsRemoteActionService(reader, mySites, repo, platform)

	targetID := "sub2api:ws1:acc-1"
	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateSuspended {
		t.Fatalf("expected hard failure to suspend, got %+v", results)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API hard failure must not reach status API, got %+v", platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("Sub2API hard failure must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != "" {
		t.Fatalf("state must not record a failed Sub2API status action, got %q", st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != "" {
		t.Fatalf("event must not record a failed Sub2API status action, got %+v", repo.events)
	}
}

// 恢复状态转换也不能触达注入失败的 status API。
func TestProbeTargetOnce_Sub2APIRestoreDoesNotReachStatusAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(true)}
	targetID := "sub2api:ws1:acc-1"
	observingUntil := time.Now().Add(-1 * time.Minute)
	repo.states[targetID] = map[string]ConnectionHealthState{
		"gpt-4o": {
			ConnectionID: targetID, ModelName: "gpt-4o", UserID: "user1", AdminAccountID: "ws1",
			State: StateObserving, ConsecutiveSuccesses: 1, ObservingUntil: &observingUntil, CurrentWeight: 0,
		},
	}
	platform := &fakePlatformActioner{sub2APIErr: errors.New("upstream 500")}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "acc-1", Name: "acc", Status: "active", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"acc-1": {BaseURL: server.URL, Key: "k"}},
	}
	svc := newAdminTargetsRemoteActionService(reader, mySites, repo, platform)

	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateRecovering {
		t.Fatalf("expected transition to recovering, got %+v", results)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API recovery must not reach status API, got %+v", platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("Sub2API recovery must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != "" {
		t.Fatalf("state must not record a failed Sub2API status action, got %q", st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != "" {
		t.Fatalf("event must not record a failed Sub2API status action, got %+v", repo.events)
	}
}

// 真实 PlatformService 组合路径也必须保证：探活硬失败只推进本地状态，绝不请求
// Sub2API admin accounts 的 status 更新接口。
func TestProbeTargetOnce_Sub2APIRealPlatformServiceNeverUpdatesStatus(t *testing.T) {
	bulkUpdateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/accounts/bulk-update":
			bulkUpdateCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	realPlatform := upstream.NewPlatformService(upstream.NewHTTPClient(server.Client()))
	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(true)}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: server.URL, AccessToken: "token-1", TokenType: "Bearer"}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "1515", Name: "acc", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"1515": {BaseURL: server.URL, Key: "probe-key"}},
	}
	svc := &Service{
		repo: repo, mySites: mySites, accounts: fakeAdminAccountResolver{id: "ws1"},
		dispatcher:     newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, realPlatform),
		probeRunner:    NewRealProbeRunner(),
		platformGroups: reader,
	}

	targetID := "sub2api:ws1:1515"
	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateSuspended {
		t.Fatalf("expected hard failure to suspend, got %+v", results)
	}
	if bulkUpdateCalls != 0 {
		t.Fatalf("Sub2API hard failure must not call admin bulk update, got %d calls", bulkUpdateCalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("Sub2API hard failure must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != "" {
		t.Fatalf("state must not record a Sub2API status action, got %q", st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != "" {
		t.Fatalf("event must not record a Sub2API status action, got %+v", repo.events)
	}
}

// TestProbeTargetOnce_Sub2APIRemoteActionDisabledSkipsUpstream 验证 AutoRemoteActionEnabled=false
// 时，即使状态机触发远端动作，也绝不调用 Sub2API status 接口，只记录 skipped_independent_probe。
func TestProbeTargetOnce_Sub2APIRemoteActionDisabledSkipsUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	repo := newFakeRepository()
	repo.policies = []Policy{sub2APIProbePolicy(false)}
	platform := &fakePlatformActioner{}
	mySites := fakeMySitesReader{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	reader := fakePlatformGroupReader{
		groups:        []upstream.AdminGroupInfo{{ID: "g1", Name: "vip"}},
		accountsByGrp: map[string][]upstream.AdminGroupAccountInfo{"g1": {{ID: "acc-1", Name: "acc", Models: "gpt-4o"}}},
		credByAccount: map[string]upstream.ProbeCredential{"acc-1": {BaseURL: server.URL, Key: "k"}},
	}
	svc := newAdminTargetsRemoteActionService(reader, mySites, repo, platform)

	targetID := "sub2api:ws1:acc-1"
	results, err := svc.ProbeTarget(context.Background(), "user1", targetID, []string{"gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].State != StateSuspended {
		t.Fatalf("expected hard failure to suspend, got %+v", results)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("expected no upstream call when AutoRemoteActionEnabled=false, got %+v", platform.sub2APICalls)
	}
	if len(repo.targetActionStates) != 0 {
		t.Fatalf("disabled remote action must not create a status action snapshot, got %+v", repo.targetActionStates)
	}
	st := repo.states[targetID]["gpt-4o"]
	if st.LastRemoteAction != RemoteActionSkippedIndependentProbe {
		t.Fatalf("expected LastRemoteAction=%s, got %q", RemoteActionSkippedIndependentProbe, st.LastRemoteAction)
	}
	if len(repo.events) != 1 || repo.events[0].RemoteAction != RemoteActionSkippedIndependentProbe {
		t.Fatalf("expected skipped action audit, got %+v", repo.events)
	}
}
