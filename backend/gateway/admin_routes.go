// 管理面：路由保存、倍率重排与 Ensure 上游密钥。
package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/bejix/upstream-ops/backend/storage"
)

// ListRoutes 列出路由（库内顺序，不实时拉上游）。
func (a *AdminService) ListRoutes(groupID uint) ([]storage.GatewayRoute, error) {
	list, err := a.Routes.ListByGroupID(groupID)
	if err != nil || len(list) == 0 {
		return list, err
	}
	// 读路径补全源分组名（sub2api 历史路由常只有 id）；有变更则写回，避免持续显示「源 ID: N」
	groupsByChannel := a.loadGroupsByChannel(context.Background(), list)
	var dirty []storage.GatewayRoute
	for i := range list {
		before := strings.TrimSpace(list[i].SourceGroupName)
		a.enrichRouteSourceGroupName(&list[i], groupsByChannel[list[i].SourceChannelID])
		after := strings.TrimSpace(list[i].SourceGroupName)
		if after != "" && after != before && !a.isSourceGroupIDPlaceholder(after) {
			dirty = append(dirty, list[i])
		}
	}
	if len(dirty) > 0 && a.Routes != nil {
		for _, r := range dirty {
			_ = a.Routes.UpdateSourceGroupSnapshot(r.ID, r.SourceGroupID, r.SourceGroupName)
		}
	}
	return list, nil
}

// orderRoutesForGroup 按组倍率排序方向重排（展示与尝试顺序一致），并刷新账号计费倍率。
// 会请求上游源分组（带缓存）；仅保存/重排路径使用，勿用于高频只读列表。

func (a *AdminService) orderRoutesForGroup(groupID uint, list []storage.GatewayRoute) ([]storage.GatewayRoute, error) {
	if len(list) == 0 {
		return list, nil
	}
	dir := "asc"
	if g, err := a.Groups.FindByID(groupID); err == nil && g != nil {
		if d := strings.TrimSpace(g.RateSortDirection); d != "" {
			dir = d
		}
	}
	groupsByChannel := a.loadGroupsByChannel(context.Background(), list)
	ordered := OrderRoutesByRate(list, groupsByChannel, dir)
	// 与同步账号 Apply 一致：用实时源分组换算结果写回 billing_rate_multiplier
	for i := range ordered {
		rate := RateForRoute(&ordered[i], groupsByChannel[ordered[i].SourceChannelID])
		if rate > 0 {
			ordered[i].BillingRateMultiplier = rate
		}
	}
	return ordered, nil
}

// reorderRoutesPersisted 按当前组排序方向重写路由 position / 计费倍率并落库。

func (a *AdminService) reorderRoutesPersisted(groupID uint) error {
	list, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	ordered, err := a.orderRoutesForGroup(groupID, list)
	if err != nil {
		return err
	}
	return a.Routes.SaveForGroup(groupID, ordered)
}

// ResortRoutesOnRateScan 倍率扫描结束后调用：对开启「渠道分组价格倍率重排」的组
// 按实时源分组倍率重写路由顺序与账号计费倍率。

func (a *AdminService) ResortRoutesOnRateScan(ctx context.Context) {
	if a == nil || a.Service == nil || a.Groups == nil || a.Routes == nil {
		return
	}
	_ = ctx
	// 扫描刚更新完渠道倍率，丢掉旧源分组缓存再拉
	a.InvalidateChannelGroupsCache()
	groups, err := a.Groups.List()
	if err != nil {
		if a.Log != nil {
			a.Log.Warn("list gateway groups for rate resort", "err", err)
		}
		return
	}
	for _, g := range groups {
		if !g.RateResortEnabled {
			continue
		}
		if err := a.reorderRoutesPersisted(g.ID); err != nil && a.Log != nil {
			a.Log.Warn("resort gateway routes after rate scan", "group_id", g.ID, "name", g.Name, "err", err)
		}
	}
}

// applyProviderRouteBilling 直连路由倍率落库：
//   - custom：账号计费倍率跟 rate_convert_value（缺省回落 provider 默认）
//   - raw 等非 custom：账号计费倍率强制用 provider.default_billing_rate
//
// 避免 raw 被写成 1 后，升序调度把直连排到最贵、监控渠道反而优先。

func (a *AdminService) SaveRoutes(groupID uint, inputs []RouteInput) ([]storage.GatewayRoute, error) {
	group, err := a.Groups.FindByID(groupID)
	if err != nil {
		return nil, err
	}
	list := make([]storage.GatewayRoute, 0, len(inputs))
	for i, in := range inputs {
		kind := strings.ToLower(strings.TrimSpace(in.SourceKind))
		if kind == "" {
			if in.GatewayProviderID > 0 && in.SourceChannelID == 0 {
				kind = storage.GatewayRouteSourceProvider
			} else {
				kind = storage.GatewayRouteSourceMonitor
			}
		}
		up := a.normalizeUpstreamProtocol(in.UpstreamProtocol)
		uaMode := NormalizeUserAgentMode(in.UserAgentMode)
		uaCustom := strings.TrimSpace(in.UserAgentCustom)
		if uaMode != storage.GatewayUserAgentModeCustom {
			uaCustom = ""
		}
		route := storage.GatewayRoute{
			ID:                    in.ID,
			SourceKind:            kind,
			SourceChannelID:       in.SourceChannelID,
			GatewayProviderID:     in.GatewayProviderID,
			SourceGroupID:         in.SourceGroupID,
			SourceGroupName:       strings.TrimSpace(in.SourceGroupName),
			Weight:                in.Weight,
			RateConvertMode:       in.RateConvertMode,
			RateConvertValue:      in.RateConvertValue,
			BillingRateMultiplier: in.BillingRateMultiplier,
			Enabled:               in.Enabled,
			ModelMappingJSON:      strings.TrimSpace(in.ModelMappingJSON),
			UpstreamProtocol:      up,
			Concurrency:           in.Concurrency,
			UserAgentMode:         uaMode,
			UserAgentCustom:       uaCustom,
		}
		if kind == storage.GatewayRouteSourceProvider {
			if in.GatewayProviderID == 0 {
				return nil, fmt.Errorf("route[%d]: gateway_provider_id is required", i)
			}
			if a.Providers != nil {
				p, err := a.Providers.FindByID(in.GatewayProviderID)
				if err != nil {
					return nil, fmt.Errorf("route[%d]: provider %d not found", i, in.GatewayProviderID)
				}
				// 非 custom：账号计费倍率用 provider.default_billing_rate，避免 raw 落库 1 导致调度错位
				a.applyProviderRouteBilling(&route, p)
			}
			route.SourceChannelID = 0
			route.SourceGroupID = nil
			route.SourceGroupName = ""
		} else {
			if in.SourceChannelID == 0 {
				return nil, fmt.Errorf("route[%d]: source_channel_id is required", i)
			}
			route.GatewayProviderID = 0
		}
		list = append(list, route)
	}
	// 对齐同步账号：保存时按倍率重排，列表顺序 = 尝试顺序
	dir := group.RateSortDirection
	if dir == "" {
		dir = "asc"
	}
	groupsByChannel := a.loadGroupsByChannel(context.Background(), list)
	// sub2api 等有 group id 的渠道：补全空/占位的源分组名称，避免 UI 只显示「源 ID: N」
	for i := range list {
		a.enrichRouteSourceGroupName(&list[i], groupsByChannel[list[i].SourceChannelID])
	}
	list = OrderRoutesByRate(list, groupsByChannel, dir)
	if err := a.Routes.SaveForGroup(groupID, list); err != nil {
		return nil, err
	}
	a.invalidateModelsCache(groupID)
	return a.ListRoutes(groupID)
}

// EnsureKeyRouteResult 单条路由 ensure 结果（失败跳过，不中断其它路由）。

func (a *AdminService) EnsureRouteKeys(ctx context.Context, groupID uint) (*EnsureKeysResult, error) {
	a.routeKeyBindingMu.Lock()
	defer a.routeKeyBindingMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := a.Groups.FindByID(groupID); err != nil {
		return nil, err
	}
	routes, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return nil, err
	}
	results := make([]EnsureKeyRouteResult, len(routes))
	for i := range routes {
		results[i] = a.ensureRouteKeyResult(ctx, groupID, &routes[i])
	}

	list, err := a.Routes.ListByGroupID(groupID)
	if err != nil {
		return nil, err
	}
	out := &EnsureKeysResult{Items: list, Routes: results}
	for _, r := range results {
		if r.Skipped {
			out.SkipCount++
		} else if r.OK {
			out.OKCount++
		} else {
			out.FailCount++
		}
	}
	return out, nil
}

func (a *AdminService) ensureRouteKeyResult(
	ctx context.Context,
	groupID uint,
	r *storage.GatewayRoute,
) EnsureKeyRouteResult {
	rr := EnsureKeyRouteResult{
		RouteID:    r.ID,
		SourceKind: r.NormalizeSourceKind(),
		ChannelID:  r.SourceChannelID,
		ProviderID: r.GatewayProviderID,
	}
	if r.NormalizeSourceKind() == storage.GatewayRouteSourceProvider {
		rr.Skipped = true
		rr.SkipReason = "直连渠道无需 ensure（密钥在直连配置中）"
		if a.Providers != nil && r.GatewayProviderID > 0 {
			if p, e := a.Providers.FindByID(r.GatewayProviderID); e == nil && p != nil {
				rr.ProviderName = p.Name
				rr.Label = p.Name
			}
		}
		if rr.Label == "" {
			rr.Label = fmt.Sprintf("直连 #%d", r.GatewayProviderID)
		}
		return rr
	}
	chName := ""
	if !r.Enabled {
		rr.Skipped = true
		rr.SkipReason = "路由已停用"
		return rr
	}
	if r.SourceAPIKeyID > 0 || strings.TrimSpace(r.SourceAPIKeyCipher) != "" {
		rr.Skipped = true
		rr.KeyName = r.SourceAPIKeyName
		rr.SkipReason = "路由已绑定密钥"
		return rr
	}
	if ch, e := a.Channels.FindByID(r.SourceChannelID); e == nil && ch != nil {
		chName = ch.Name
		rr.ChannelName = ch.Name
	}
	rr.Label = a.formatChannelGroupLabel(chName, r.SourceGroupName, r.SourceChannelID)
	rr.KeyName = a.stableUpstreamKeyName(r.SourceChannelID, r.SourceGroupID, r.SourceGroupName)

	if err := a.ensureSourceAPIKeyLocked(ctx, groupID, r); err != nil {
		rr.Error = safeRouteKeyError(err)
		return rr
	}
	rr.OK = true
	// ensure 后名称以库为准
	if updated, e := a.Routes.FindByID(r.ID); e == nil && updated != nil {
		if n := strings.TrimSpace(updated.SourceAPIKeyName); n != "" {
			rr.KeyName = n
		}
	}
	return rr
}

// stableUpstreamKeyName 上游 API Key 统一命名（跨网关组复用）。
//
// 规则：同一「监控渠道 + 源分组」始终同一名字，避免多网关组各建一把 Key。
//
//	uops-ch{渠道ID}-sg{源分组ID}          // 有 remote group id
//	uops-ch{渠道ID}-sgn-{源分组名slug}    // 仅有分组名
//	uops-ch{渠道ID}-default               // 未绑定源分组
//
// 不再使用 upstream-ops-gw-g{组}-r{路由}（会按路由裂变）。

func (a *AdminService) ensureSourceAPIKey(ctx context.Context, groupID uint, route *storage.GatewayRoute) error {
	a.routeKeyBindingMu.Lock()
	defer a.routeKeyBindingMu.Unlock()
	return a.ensureSourceAPIKeyLocked(ctx, groupID, route)
}

// ClearRoutePause 清除路由暂停。
func (a *AdminService) ClearRoutePause(routeID uint) error {
	return a.Routes.ClearTempUnschedulable(routeID)
}

// ---------- models list ----------

// ModelSource 模型在某条路由上的来源（渠道 + 源分组）。
