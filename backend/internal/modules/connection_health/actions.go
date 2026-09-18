package connection_health

import (
	"context"
	"fmt"
	"strings"

	"transithub/backend/internal/modules/my_sites"
	"transithub/backend/internal/modules/upstream"
)

// RemoteActionRunner 是自动降级/恢复对上游平台的远端动作接口，按平台类型分派到具体实现。
// 所有实现都必须是 panic-safe 的：远端调用失败绝不能让调度器崩溃。
//
// Degrade/Restore 服务旧 real_connections 对接链路路径；DegradeTarget/RestoreTarget 服务当前
// 分组健康的独立探活 targetId 路径（见 admin_targets.go 的 probeTargetOnce），不依赖
// real_connections，避免为了调用远端动作而伪造一条 RealConnection。
type RemoteActionRunner interface {
	Degrade(ctx context.Context, conn my_sites.RealConnection, state ConnectionHealthState) (remoteAction string, err error)
	Restore(ctx context.Context, conn my_sites.RealConnection, state ConnectionHealthState) (remoteAction string, err error)
	DegradeTarget(ctx context.Context, session upstream.Session, target AdminProbeTarget, state ConnectionHealthState) (remoteAction string, err error)
	RestoreTarget(ctx context.Context, session upstream.Session, target AdminProbeTarget, state ConnectionHealthState) (remoteAction string, err error)
	ApplyTargetState(ctx context.Context, session upstream.Session, target AdminProbeTarget, weight *int, status string) (remoteAction string, err error)
}

// PlatformActioner 是 connection_health 对 upstream.PlatformService 远端降级能力的窄依赖，
// 避免直接依赖 PlatformService 的其余大量方法。
type PlatformActioner interface {
	UpdateNewAPIChannelWeightStatus(session upstream.Session, channelID string, weight int, status int) error
	// UpdateSub2APIAdminAccountStatus 只用于升级后恢复旧版健康监控已经关闭的账号。
	// 新的降级/恢复决策禁止修改 Sub2API status，只能通过 priority 同步器调整调度顺序。
	UpdateSub2APIAdminAccountStatus(session upstream.Session, accountID string, status string) error
}

// SessionProvider 复用 my_sites 已登录并自动刷新的 admin 会话，不重复实现登录逻辑。
type SessionProvider interface {
	RequireSession(ctx context.Context, userID string, adminAccountID string) (upstream.Session, error)
}

// RemoteActionUnsupported 是没有已验证安全接口时的统一标记，绝不发明未经证实的远端请求。
const RemoteActionUnsupported = "unsupported"

// Sub2API status 标记仅保留用于识别旧版历史事件和执行一次性恢复。
// 当前监控路径不得再产生 inactive/active 动作。
const (
	RemoteActionSub2APIStatusInactive       = "sub2api_account_status_inactive"
	RemoteActionSub2APIStatusActive         = "sub2api_account_status_active"
	RemoteActionSub2APIStatusInactiveFailed = "sub2api_account_status_inactive_failed"
	RemoteActionSub2APIStatusActiveFailed   = "sub2api_account_status_active_failed"
	RemoteActionNewAPIUpdateFailed          = "newapi_channel_update_failed"
)

// remoteActionDispatcher 按连接所在上游站点的平台类型（new-api / sub2api）分派远端动作。
// NewAPI 保留 channel weight/status 动作；Sub2API 状态写入在这一层被硬性禁止，
// 分组监控的降级只能由 priority_strategy.go 的带快照同步器执行。
type remoteActionDispatcher struct {
	sites    SiteLookup
	sessions SessionProvider
	platform PlatformActioner
}

func newRemoteActionDispatcher(sites SiteLookup, sessions SessionProvider, platform PlatformActioner) *remoteActionDispatcher {
	return &remoteActionDispatcher{sites: sites, sessions: sessions, platform: platform}
}

func (d *remoteActionDispatcher) Degrade(ctx context.Context, conn my_sites.RealConnection, state ConnectionHealthState) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("remote degrade panic recovered: %v", r)
		}
	}()

	site, siteErr := d.sites.GetSite(ctx, conn.UpstreamSiteID)
	if siteErr != nil || site == nil {
		return RemoteActionUnsupported, siteErr
	}

	switch site.Platform {
	case upstream.PlatformNewAPI:
		return d.degradeNewAPI(ctx, conn)
	case upstream.PlatformSub2API:
		return d.degradeSub2API(ctx, conn)
	default:
		return RemoteActionUnsupported, nil
	}
}

func (d *remoteActionDispatcher) Restore(ctx context.Context, conn my_sites.RealConnection, state ConnectionHealthState) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("remote restore panic recovered: %v", r)
		}
	}()

	site, siteErr := d.sites.GetSite(ctx, conn.UpstreamSiteID)
	if siteErr != nil || site == nil {
		return RemoteActionUnsupported, siteErr
	}

	switch site.Platform {
	case upstream.PlatformNewAPI:
		return d.restoreNewAPI(ctx, conn, state)
	case upstream.PlatformSub2API:
		return d.restoreSub2API(ctx, conn)
	default:
		return RemoteActionUnsupported, nil
	}
}

// DegradeTarget / RestoreTarget 服务当前分组健康的独立探活 targetId 路径：不依赖
// real_connections，直接用调用方已经持有的 session + AdminProbeTarget 发起远端动作。
func (d *remoteActionDispatcher) DegradeTarget(ctx context.Context, session upstream.Session, target AdminProbeTarget, state ConnectionHealthState) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("remote degrade target panic recovered: %v", r)
		}
	}()
	if target.AccountID == "" {
		return RemoteActionUnsupported, nil
	}
	if target.Platform == string(upstream.PlatformNewAPI) {
		if err := d.platform.UpdateNewAPIChannelWeightStatus(session, target.AccountID, 0, 2); err != nil {
			return RemoteActionNewAPIUpdateFailed, err
		}
		return "newapi_channel_disabled", nil
	}
	if target.Platform != string(upstream.PlatformSub2API) {
		return RemoteActionUnsupported, nil
	}
	return RemoteActionUnsupported, nil
}

func (d *remoteActionDispatcher) RestoreTarget(ctx context.Context, session upstream.Session, target AdminProbeTarget, state ConnectionHealthState) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("remote restore target panic recovered: %v", r)
		}
	}()
	if target.AccountID == "" {
		return RemoteActionUnsupported, nil
	}
	if target.Platform == string(upstream.PlatformNewAPI) {
		weight := state.CurrentWeight
		status := 1
		if weight <= 0 {
			status = 2
		}
		if err := d.platform.UpdateNewAPIChannelWeightStatus(session, target.AccountID, weight, status); err != nil {
			return RemoteActionNewAPIUpdateFailed, err
		}
		return fmt.Sprintf("newapi_channel_weight_%d", weight), nil
	}
	if target.Platform != string(upstream.PlatformSub2API) {
		return RemoteActionUnsupported, nil
	}
	return RemoteActionUnsupported, nil
}

// ApplyTargetState 写入账号级聚合决策。旧 real_connections 仍使用 Degrade/Restore；新的
// admin 分组健康链路通过这里精确恢复接管前保存的启停状态和权重。
func (d *remoteActionDispatcher) ApplyTargetState(ctx context.Context, session upstream.Session, target AdminProbeTarget, weight *int, status string) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("apply target state panic recovered: %v", r)
		}
	}()
	if target.AccountID == "" {
		return RemoteActionUnsupported, nil
	}
	if target.Platform == string(upstream.PlatformNewAPI) {
		resolvedWeight := 100
		if weight != nil {
			resolvedWeight = *weight
		}
		resolvedStatus := 1
		if status == "2" || status == "disabled" || status == "inactive" {
			resolvedStatus = 2
		}
		if err := d.platform.UpdateNewAPIChannelWeightStatus(session, target.AccountID, resolvedWeight, resolvedStatus); err != nil {
			return RemoteActionNewAPIUpdateFailed, err
		}
		if resolvedStatus == 2 && resolvedWeight == 0 {
			return "newapi_channel_disabled", nil
		}
		return fmt.Sprintf("newapi_channel_weight_%d", resolvedWeight), nil
	}
	if target.Platform != string(upstream.PlatformSub2API) {
		return RemoteActionUnsupported, nil
	}
	return RemoteActionUnsupported, nil
}

// restoreLegacySub2APIStatus 只处理升级前已由本模块写入的 status 快照。
// 新决策路径不能调用它；它的唯一用途是重新启用系统曾经关闭的账号。
func (d *remoteActionDispatcher) restoreLegacySub2APIStatus(session upstream.Session, target AdminProbeTarget, status string) (remoteAction string, err error) {
	defer func() {
		if r := recover(); r != nil {
			remoteAction = RemoteActionUnsupported
			err = fmt.Errorf("restore legacy sub2api status panic recovered: %v", r)
		}
	}()
	if target.Platform != string(upstream.PlatformSub2API) || target.AccountID == "" {
		return RemoteActionUnsupported, nil
	}
	// This compatibility path may only undo a status write made by an older
	// version. Never let malformed or unexpected historical data turn it into
	// another account-disable path.
	if strings.ToLower(strings.TrimSpace(status)) != "active" {
		return RemoteActionUnsupported, nil
	}
	resolvedStatus := "active"
	if err := d.platform.UpdateSub2APIAdminAccountStatus(session, target.AccountID, resolvedStatus); err != nil {
		return RemoteActionSub2APIStatusActiveFailed, err
	}
	return RemoteActionSub2APIStatusActive, nil
}

func (d *remoteActionDispatcher) degradeNewAPI(ctx context.Context, conn my_sites.RealConnection) (string, error) {
	// new-api 场景下 RealConnection.AdminAccountID 存的是创建真实对接时回查得到的 channel ID
	// （见 my_sites.Service.RealConnect），不是转发子账号 ID。
	channelID := conn.AdminAccountID
	if channelID == "" {
		return RemoteActionUnsupported, nil
	}
	session, err := d.sessions.RequireSession(ctx, conn.UserID, conn.WorkspaceAdminAccountID)
	if err != nil {
		return RemoteActionUnsupported, err
	}
	if err := d.platform.UpdateNewAPIChannelWeightStatus(session, channelID, 0, 2); err != nil {
		return RemoteActionUnsupported, err
	}
	return "newapi_channel_disabled", nil
}

func (d *remoteActionDispatcher) restoreNewAPI(ctx context.Context, conn my_sites.RealConnection, state ConnectionHealthState) (string, error) {
	channelID := conn.AdminAccountID
	if channelID == "" {
		return RemoteActionUnsupported, nil
	}
	session, err := d.sessions.RequireSession(ctx, conn.UserID, conn.WorkspaceAdminAccountID)
	if err != nil {
		return RemoteActionUnsupported, err
	}
	weight := state.CurrentWeight
	status := 1
	if weight <= 0 {
		// 权重仍为 0 时不解除远端禁用，避免观察期误放流量。
		status = 2
	}
	if err := d.platform.UpdateNewAPIChannelWeightStatus(session, channelID, weight, status); err != nil {
		return RemoteActionUnsupported, err
	}
	return fmt.Sprintf("newapi_channel_weight_%d", weight), nil
}

// 旧 real_connections 路径没有可靠的原 priority 快照，因此同样禁止改写
// Sub2API status，也不在这里猜测 priority。
func (d *remoteActionDispatcher) degradeSub2API(ctx context.Context, conn my_sites.RealConnection) (string, error) {
	return RemoteActionUnsupported, nil
}

func (d *remoteActionDispatcher) restoreSub2API(ctx context.Context, conn my_sites.RealConnection) (string, error) {
	return RemoteActionUnsupported, nil
}
