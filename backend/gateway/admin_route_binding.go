package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

func routeKeyError(code, message string) error {
	return &RouteKeyBindingError{Code: code, Message: message}
}

func safeRouteKeyError(err error) string {
	var bindingError *RouteKeyBindingError
	if errors.As(err, &bindingError) {
		return bindingError.Message
	}
	if errors.Is(err, storage.ErrGatewayRouteChanged) {
		return "路由已发生变化，未覆盖当前绑定"
	}
	return "密钥绑定失败，请检查路由配置后重试"
}

func (a *AdminService) BindRouteKey(ctx context.Context, groupID, routeID uint, in BindRouteKeyInput) (*storage.GatewayRoute, error) {
	if in.SourceAPIKeyID <= 0 {
		return nil, routeKeyError("invalid_key_id", "source_api_key_id 必须大于 0")
	}
	a.routeKeyBindingMu.Lock()
	defer a.routeKeyBindingMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := a.Groups.FindByID(groupID); err != nil {
		return nil, err
	}
	route, err := a.Routes.FindByID(routeID)
	if err != nil {
		return nil, err
	}
	if route.GatewayGroupID != groupID {
		return nil, gorm.ErrRecordNotFound
	}
	channel, sourceGroup, err := a.routeKeySource(ctx, route)
	if err != nil {
		return nil, err
	}
	keys, err := a.listCompleteRouteKeys(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	key := a.findAPIKeyByID(keys, in.SourceAPIKeyID)
	if key == nil {
		return nil, routeKeyError("key_not_in_channel", "指定密钥不属于此路由的渠道")
	}
	if err := validateRouteKey(key, sourceGroup, channel.Type); err != nil {
		return nil, err
	}
	return a.bindValidatedRouteKey(ctx, route, key)
}

func (a *AdminService) routeKeySource(ctx context.Context, route *storage.GatewayRoute) (*storage.Channel, *connector.APIKeyGroup, error) {
	if route.NormalizeSourceKind() != storage.GatewayRouteSourceMonitor || route.SourceChannelID == 0 {
		return nil, nil, routeKeyError("invalid_route_source", "仅支持监控渠道路由绑定上游密钥")
	}
	if a.ChannelAPI == nil || a.Channels == nil || a.Cipher == nil {
		return nil, nil, errors.New("route key binding dependencies unavailable")
	}
	channel, err := a.Channels.FindByID(route.SourceChannelID)
	if err != nil {
		return nil, nil, err
	}
	if channel.Type != storage.ChannelTypeNewAPI && channel.Type != storage.ChannelTypeSub2API {
		return nil, nil, routeKeyError("unsupported_channel", "渠道类型不支持密钥绑定")
	}
	groups, err := a.ChannelAPI.ListAPIKeyGroups(ctx, channel.ID)
	if err != nil {
		return nil, nil, routeKeyError("upstream_groups_unavailable", "读取上游分组失败，未修改密钥或路由")
	}
	var matched *connector.APIKeyGroup
	for index := range groups {
		group := &groups[index]
		matches := false
		if route.SourceGroupID != nil {
			matches = *route.SourceGroupID > 0 && group.ID != nil && *group.ID == *route.SourceGroupID
		} else {
			matches = strings.TrimSpace(group.Name) == strings.TrimSpace(route.SourceGroupName)
		}
		if matches {
			if matched != nil {
				return nil, nil, routeKeyError("ambiguous_source_group", "源分组存在歧义，请明确源分组 ID")
			}
			matched = group
		}
	}
	if matched == nil {
		return nil, nil, routeKeyError("source_group_not_found", "路由的源分组不存在，未修改密钥或路由")
	}
	if channel.Type == storage.ChannelTypeSub2API && (matched.ID == nil || *matched.ID <= 0) {
		return nil, nil, routeKeyError("invalid_source_group", "上游源分组缺少有效 ID")
	}
	return channel, matched, nil
}

func (a *AdminService) listCompleteRouteKeys(ctx context.Context, channelID uint) ([]connector.APIKey, error) {
	const requestedPageSize = 100
	var keys []connector.APIKey
	seen := make(map[int64]struct{})
	var first *connector.APIKeyPage
	for pageNumber := 1; pageNumber <= 10000; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := a.ChannelAPI.ListAPIKeys(ctx, channelID, connector.APIKeyQuery{Page: pageNumber, PageSize: requestedPageSize})
		if err != nil {
			return nil, routeKeyError("upstream_keys_unavailable", "读取上游密钥列表失败，未创建或绑定密钥")
		}
		if page == nil || page.Page != pageNumber || page.PageSize <= 0 || page.PageSize > requestedPageSize || page.Total < 0 || page.Pages < 0 || len(page.Items) > page.PageSize {
			return nil, routeKeyError("incomplete_key_inventory", "上游密钥分页无效，未创建或绑定密钥")
		}
		expectedPages := (page.Total + int64(page.PageSize) - 1) / int64(page.PageSize)
		if expectedPages < 1 {
			expectedPages = 1
		}
		if int64(page.Pages) != expectedPages && !(page.Total == 0 && page.Pages == 0) {
			return nil, routeKeyError("incomplete_key_inventory", "上游密钥分页总数不一致，未创建或绑定密钥")
		}
		if first == nil {
			first = page
		} else if first.Total != page.Total || first.PageSize != page.PageSize || first.Pages != page.Pages {
			return nil, routeKeyError("incomplete_key_inventory", "上游密钥列表在分页期间变化，请重新核查")
		}
		for _, key := range page.Items {
			if key.ID <= 0 {
				return nil, routeKeyError("incomplete_key_inventory", "上游密钥列表包含无效 ID")
			}
			if _, exists := seen[key.ID]; exists {
				return nil, routeKeyError("incomplete_key_inventory", "上游密钥分页重复，未创建或绑定密钥")
			}
			seen[key.ID] = struct{}{}
			keys = append(keys, key)
		}
		if int64(pageNumber) == expectedPages {
			if int64(len(keys)) != page.Total {
				return nil, routeKeyError("incomplete_key_inventory", "上游密钥列表不完整，未创建或绑定密钥")
			}
			return keys, nil
		}
		if len(page.Items) != page.PageSize {
			return nil, routeKeyError("incomplete_key_inventory", "上游密钥分页提前结束，未创建或绑定密钥")
		}
	}
	return nil, routeKeyError("incomplete_key_inventory", "上游密钥分页超过安全上限，未创建或绑定密钥")
}

func validateRouteKey(key *connector.APIKey, group *connector.APIKeyGroup, channelType storage.ChannelType) error {
	if key == nil || key.ID <= 0 {
		return routeKeyError("invalid_key", "上游未返回有效密钥 ID")
	}
	if group.ID != nil && (key.GroupID == nil || *key.GroupID != *group.ID) {
		return routeKeyError("key_group_mismatch", "密钥与路由源分组 ID 不一致，未修改现有密钥")
	}
	if channelType == storage.ChannelTypeNewAPI {
		if strings.TrimSpace(key.Group) != strings.TrimSpace(group.Name) {
			return routeKeyError("key_group_mismatch", "密钥与路由源分组不一致，未修改现有密钥")
		}
	} else if group.ID == nil || key.GroupID == nil || *key.GroupID != *group.ID {
		return routeKeyError("key_group_mismatch", "密钥与路由源分组 ID 不一致，未修改现有密钥")
	}
	if strings.TrimSpace(key.Status) != "active" {
		return routeKeyError("inactive_key", "指定密钥当前不可用，未修改密钥状态")
	}
	now := time.Now()
	if (channelType == storage.ChannelTypeNewAPI && key.ExpiredTime != -1 && key.ExpiredTime <= now.Unix()) || (key.ExpiredTime > 0 && key.ExpiredTime <= now.Unix()) || (key.ExpiresAt != nil && !key.ExpiresAt.After(now)) {
		return routeKeyError("expired_key", "指定密钥已过期，未修改有效期")
	}
	return nil
}

func (a *AdminService) bindValidatedRouteKey(ctx context.Context, route *storage.GatewayRoute, key *connector.APIKey) (*storage.GatewayRoute, error) {
	secret, err := a.ChannelAPI.RevealAPIKey(ctx, route.SourceChannelID, key.ID)
	if err != nil {
		return nil, routeKeyError("upstream_key_unavailable", "读取指定上游密钥失败，未修改路由")
	}
	if secret == "" || strings.ContainsAny(secret, "*…") || strings.Contains(secret, "...") || strings.IndexFunc(secret, func(value rune) bool { return unicode.IsSpace(value) || unicode.IsControl(value) }) >= 0 {
		return nil, routeKeyError("invalid_upstream_key", "上游返回空值、掩码或无效密钥，未修改路由")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if route.SourceAPIKeyID == key.ID && route.SourceAPIKeyName == key.Name && route.SourceAPIKeyCipher != "" {
		previous, decryptErr := a.Cipher.Decrypt(route.SourceAPIKeyCipher)
		if decryptErr == nil && previous == secret {
			current, readErr := a.Routes.FindByID(route.ID)
			if errors.Is(readErr, gorm.ErrRecordNotFound) {
				return nil, storage.ErrGatewayRouteChanged
			}
			if readErr != nil {
				return nil, readErr
			}
			if !storage.SameGatewayRouteSource(*route, *current) || route.GatewayGroupID != current.GatewayGroupID || route.SourceAPIKeyID != current.SourceAPIKeyID || route.SourceAPIKeyName != current.SourceAPIKeyName || route.SourceAPIKeyCipher != current.SourceAPIKeyCipher || !route.UpdatedAt.Equal(current.UpdatedAt) {
				return nil, storage.ErrGatewayRouteChanged
			}
			return current, nil
		}
	}
	cipherText, err := a.Cipher.Encrypt(secret)
	if err != nil {
		return nil, errors.New("encrypt upstream route key failed")
	}
	if err := a.Routes.BindSourceKeyIfUnchanged(route, key.ID, key.Name, cipherText); err != nil {
		return nil, err
	}
	a.invalidateModelsCache(route.GatewayGroupID)
	return a.Routes.FindByID(route.ID)
}

func (a *AdminService) ensureSourceAPIKeyLocked(ctx context.Context, groupID uint, route *storage.GatewayRoute) error {
	if route.GatewayGroupID != groupID {
		return gorm.ErrRecordNotFound
	}
	if route.NormalizeSourceKind() == storage.GatewayRouteSourceProvider || !route.Enabled || route.SourceAPIKeyID > 0 || strings.TrimSpace(route.SourceAPIKeyCipher) != "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	channel, sourceGroup, err := a.routeKeySource(ctx, route)
	if err != nil {
		return err
	}
	keys, err := a.listCompleteRouteKeys(ctx, channel.ID)
	if err != nil {
		return err
	}
	keyName := a.stableUpstreamKeyName(channel.ID, route.SourceGroupID, route.SourceGroupName)
	var selected *connector.APIKey
	for index := range keys {
		if strings.TrimSpace(keys[index].Name) == keyName {
			if selected != nil {
				return routeKeyError("ambiguous_key_name", "同名上游密钥不唯一，请指定密钥 ID 精确绑定")
			}
			selected = &keys[index]
		}
	}
	created := false
	if selected == nil {
		selected, err = a.ChannelAPI.CreateAPIKey(ctx, channel.ID, connector.APIKeyCreateRequest{
			Name:           keyName,
			Group:          strings.TrimSpace(sourceGroup.Name),
			GroupID:        sourceGroup.ID,
			UnlimitedQuota: boolPtr(channel.Type == storage.ChannelTypeNewAPI),
			ExpiredTime:    int64PtrIf(channel.Type == storage.ChannelTypeNewAPI, -1),
		})
		if err != nil {
			return routeKeyError("upstream_create_uncertain", "创建上游密钥失败或结果未确认，请先核查稳定名称密钥，勿直接重复创建")
		}
		created = true
		if selected == nil || selected.ID <= 0 {
			return routeKeyError("upstream_create_uncertain", "上游未返回新密钥 ID，请先核查稳定名称密钥")
		}
		if strings.TrimSpace(selected.Name) != keyName || a.findAPIKeyByID(keys, selected.ID) != nil {
			return routeKeyError("upstream_create_uncertain", fmt.Sprintf("创建返回的密钥 #%d 无法确认身份，请核查后精确绑定", selected.ID))
		}
		createdID := selected.ID
		freshKeys, listErr := a.listCompleteRouteKeys(ctx, channel.ID)
		if listErr != nil {
			return routeKeyError("created_key_not_bound", fmt.Sprintf("新建上游密钥 #%d 尚未完整回读确认，已保留，请核查后复用", createdID))
		}
		selected = nil
		for index := range freshKeys {
			if strings.TrimSpace(freshKeys[index].Name) != keyName {
				continue
			}
			if selected != nil || freshKeys[index].ID != createdID {
				return routeKeyError("created_key_not_bound", fmt.Sprintf("新建上游密钥 #%d 的稳定名称出现歧义，未绑定，请精确核查", createdID))
			}
			selected = &freshKeys[index]
		}
		if selected == nil {
			return routeKeyError("created_key_not_bound", fmt.Sprintf("新建上游密钥 #%d 未在完整目录中找到，未绑定，请核查后复用", createdID))
		}
	}
	if err := validateRouteKey(selected, sourceGroup, channel.Type); err != nil {
		if created {
			return routeKeyError("created_key_not_bound", fmt.Sprintf("新建上游密钥 #%d 校验失败，已保留且未修改路由，请核查后精确绑定", selected.ID))
		}
		return err
	}
	_, err = a.bindValidatedRouteKey(ctx, route, selected)
	if err != nil && created {
		return &RouteKeyBindingError{Code: "created_key_not_bound", Message: fmt.Sprintf("新建上游密钥 #%d 尚未绑定，已保留；请重新核查路由后复用该密钥", selected.ID), cause: err}
	}
	return err
}
