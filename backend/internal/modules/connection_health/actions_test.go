package connection_health

import (
	"context"
	"errors"
	"testing"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

type fakeSiteLookup struct {
	site *upstream.Site
	err  error
}

func (f fakeSiteLookup) GetSite(ctx context.Context, siteID string) (*upstream.Site, error) {
	return f.site, f.err
}

type fakeSessionProvider struct {
	session upstream.Session
	err     error
}

func (f fakeSessionProvider) RequireSession(ctx context.Context, userID string, adminAccountID string) (upstream.Session, error) {
	return f.session, f.err
}

type fakePlatformActioner struct {
	err        error
	panicValue any
	calls      []struct {
		channelID string
		weight    int
		status    int
	}
	sub2APICalls []struct {
		accountID string
		status    string
	}
	sub2APIErr error
}

func (f *fakePlatformActioner) UpdateNewAPIChannelWeightStatus(session upstream.Session, channelID string, weight int, status int) error {
	if f.panicValue != nil {
		panic(f.panicValue)
	}
	f.calls = append(f.calls, struct {
		channelID string
		weight    int
		status    int
	}{channelID, weight, status})
	return f.err
}

func (f *fakePlatformActioner) UpdateSub2APIAdminAccountStatus(session upstream.Session, accountID string, status string) error {
	if f.panicValue != nil {
		panic(f.panicValue)
	}
	f.sub2APICalls = append(f.sub2APICalls, struct {
		accountID string
		status    string
	}{accountID, status})
	return f.sub2APIErr
}

func TestActions_NewAPIDegradeSuccess(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformNewAPI}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "channel-42", UserID: "u1", WorkspaceAdminAccountID: "ws1"}
	action, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "newapi_channel_disabled" {
		t.Fatalf("unexpected remote action: %s", action)
	}
	if len(platform.calls) != 1 || platform.calls[0].weight != 0 || platform.calls[0].status != 2 {
		t.Fatalf("expected one call with weight=0 status=2, got %+v", platform.calls)
	}
}

func TestActions_NewAPIDegradeFailurePropagatesAsUnsupported(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformNewAPI}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	platform := &fakePlatformActioner{err: errors.New("upstream 500")}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "channel-42"}
	action, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{})
	if err == nil {
		t.Fatalf("expected error to propagate")
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected unsupported on failure, got %s", action)
	}
}

func TestActions_NewAPIDegradePanicRecovered(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformNewAPI}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	platform := &fakePlatformActioner{panicValue: "boom"}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "channel-42"}

	action, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{})
	if err == nil {
		t.Fatalf("expected panic to surface as error, scheduler must not crash")
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected unsupported after panic recovery, got %s", action)
	}
}

func TestActions_Sub2APIDegradeNeverDisablesAccount(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformSub2API}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "sub-account-1", UpstreamKeyID: "key-1"}
	action, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected Sub2API status action to be unsupported, got %s", action)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API monitoring must never disable an account, got %+v", platform.sub2APICalls)
	}
}

func TestActions_Sub2APIRestoreNeverChangesAccountStatus(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformSub2API}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "sub-account-1"}
	action, err := dispatcher.Restore(context.Background(), conn, ConnectionHealthState{CurrentWeight: 25})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected Sub2API status action to be unsupported, got %s", action)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API monitoring must never change account status, got %+v", platform.sub2APICalls)
	}
}

// TestActions_Sub2APIDoesNotUseNewAPIWeightStatus 验证 sub2api 降级/恢复绝不调用
// new-api 专用的 UpdateNewAPIChannelWeightStatus（不把 priority 当 weight 处理）。
func TestActions_Sub2APIDoesNotUseNewAPIWeightStatus(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformSub2API}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "sub-account-1"}
	if _, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := dispatcher.Restore(context.Background(), conn, ConnectionHealthState{CurrentWeight: 50}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(platform.calls) != 0 {
		t.Fatalf("sub2api must never call the new-api channel weight/status update, got %d calls", len(platform.calls))
	}
}

// 即使注入的 status API 会报错，旧 real_connections 降级路径也不能触达它。
func TestActions_Sub2APIDoesNotReachStatusAPI(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformSub2API}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformSub2API}}
	platform := &fakePlatformActioner{sub2APIErr: errors.New("upstream 500")}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "sub-account-1"}
	action, err := dispatcher.Degrade(context.Background(), conn, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("status API must not be called: %v", err)
	}
	if action != RemoteActionUnsupported || len(platform.sub2APICalls) != 0 {
		t.Fatalf("expected no Sub2API status call, action=%q calls=%+v", action, platform.sub2APICalls)
	}
}

func TestActions_Sub2APIDegradeTargetNeverDisablesAccount(t *testing.T) {
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)

	session := upstream.Session{Platform: upstream.PlatformSub2API}
	target := AdminProbeTarget{TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1"}
	action, err := dispatcher.DegradeTarget(context.Background(), session, target, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected unsupported, got %s", action)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API target must never be disabled, got %+v", platform.sub2APICalls)
	}
}

func TestActions_Sub2APIRestoreTargetNeverChangesStatus(t *testing.T) {
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)

	session := upstream.Session{Platform: upstream.PlatformSub2API}
	target := AdminProbeTarget{TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1"}
	action, err := dispatcher.RestoreTarget(context.Background(), session, target, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != RemoteActionUnsupported {
		t.Fatalf("expected unsupported, got %s", action)
	}
	if len(platform.sub2APICalls) != 0 {
		t.Fatalf("Sub2API target status must remain unchanged, got %+v", platform.sub2APICalls)
	}
}

func TestActions_Sub2APIDegradeTargetDoesNotReachFailingStatusAPI(t *testing.T) {
	platform := &fakePlatformActioner{sub2APIErr: errors.New("upstream 500")}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)

	session := upstream.Session{Platform: upstream.PlatformSub2API}
	target := AdminProbeTarget{TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1"}
	action, err := dispatcher.DegradeTarget(context.Background(), session, target, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("status API must not be called: %v", err)
	}
	if action != RemoteActionUnsupported || len(platform.sub2APICalls) != 0 {
		t.Fatalf("expected no Sub2API status call, action=%q calls=%+v", action, platform.sub2APICalls)
	}
}

func TestActions_Sub2APIRestoreTargetDoesNotReachFailingStatusAPI(t *testing.T) {
	platform := &fakePlatformActioner{sub2APIErr: errors.New("upstream 500")}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)

	session := upstream.Session{Platform: upstream.PlatformSub2API}
	target := AdminProbeTarget{TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1"}
	action, err := dispatcher.RestoreTarget(context.Background(), session, target, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("status API must not be called: %v", err)
	}
	if action != RemoteActionUnsupported || len(platform.sub2APICalls) != 0 {
		t.Fatalf("expected no Sub2API status call, action=%q calls=%+v", action, platform.sub2APICalls)
	}
}

func TestActions_LegacySub2APIRestoreNeverWritesInactive(t *testing.T) {
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)
	target := AdminProbeTarget{
		TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1",
	}

	action, err := dispatcher.restoreLegacySub2APIStatus(
		upstream.Session{Platform: upstream.PlatformSub2API}, target, "inactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != RemoteActionUnsupported || len(platform.sub2APICalls) != 0 {
		t.Fatalf("legacy cleanup must never disable an account, action=%q calls=%+v", action, platform.sub2APICalls)
	}
}

func TestActions_LegacySub2APIRestoreRequiresExplicitActiveStatus(t *testing.T) {
	for _, status := range []string{"", "unknown", "enabled"} {
		t.Run(status, func(t *testing.T) {
			platform := &fakePlatformActioner{}
			dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)
			target := AdminProbeTarget{
				TargetID: "sub2api:ws1:acc-1", Platform: string(upstream.PlatformSub2API), AccountID: "acc-1",
			}

			action, err := dispatcher.restoreLegacySub2APIStatus(
				upstream.Session{Platform: upstream.PlatformSub2API}, target, status,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action != RemoteActionUnsupported || len(platform.sub2APICalls) != 0 {
				t.Fatalf("ambiguous historical status %q must not reach the status API: action=%q calls=%+v", status, action, platform.sub2APICalls)
			}
		})
	}
}

// TestActions_NewAPITargetRemoteActionDisablesChannel 验证独立 New API target 降级时直接更新
// channel weight/status，不依赖旧 RealConnection。
func TestActions_NewAPITargetRemoteActionDisablesChannel(t *testing.T) {
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(fakeSiteLookup{}, fakeSessionProvider{}, platform)

	session := upstream.Session{Platform: upstream.PlatformNewAPI}
	target := AdminProbeTarget{TargetID: "newapi:ws1:100", Platform: string(upstream.PlatformNewAPI), AccountID: "100"}
	action, err := dispatcher.DegradeTarget(context.Background(), session, target, ConnectionHealthState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "newapi_channel_disabled" {
		t.Fatalf("expected newapi channel disable action, got %s", action)
	}
	if len(platform.sub2APICalls) != 0 || len(platform.calls) != 1 || platform.calls[0].weight != 0 || platform.calls[0].status != 2 {
		t.Fatalf("expected one newapi disable call, sub2api=%+v newapi=%+v", platform.sub2APICalls, platform.calls)
	}
}

func TestActions_NewAPIRestoreUsesCurrentWeight(t *testing.T) {
	sites := fakeSiteLookup{site: &upstream.Site{ID: "site-1", Platform: upstream.PlatformNewAPI}}
	sessions := fakeSessionProvider{session: upstream.Session{Platform: upstream.PlatformNewAPI}}
	platform := &fakePlatformActioner{}
	dispatcher := newRemoteActionDispatcher(sites, sessions, platform)

	conn := my_sites.RealConnection{UpstreamSiteID: "site-1", AdminAccountID: "channel-42"}
	state := ConnectionHealthState{CurrentWeight: 25}
	action, err := dispatcher.Restore(context.Background(), conn, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "newapi_channel_weight_25" {
		t.Fatalf("unexpected remote action: %s", action)
	}
	if len(platform.calls) != 1 || platform.calls[0].weight != 25 || platform.calls[0].status != 1 {
		t.Fatalf("expected weight=25 status=1, got %+v", platform.calls)
	}
}
