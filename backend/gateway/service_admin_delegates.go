// Service → AdminService 薄委托，保持对外 API 兼容。
package gateway

import (
	"context"

	"github.com/bejix/upstream-ops/backend/storage"
)

// CreateGroup 创建网关分组。
func (s *Service) CreateGroup(in CreateGroupInput) (*storage.GatewayGroup, error) {
	return s.admin().CreateGroup(in)
}

// UpdateGroup 更新网关分组。
func (s *Service) UpdateGroup(id uint, in UpdateGroupInput) (*storage.GatewayGroup, error) {
	return s.admin().UpdateGroup(id, in)
}

// DeleteGroup 删除网关分组及其关联资源。
func (s *Service) DeleteGroup(id uint) error {
	return s.admin().DeleteGroup(id)
}

// ListGroups 列出全部网关分组。
func (s *Service) ListGroups() ([]storage.GatewayGroup, error) {
	return s.admin().ListGroups()
}

// ReorderGroups 按给定 id 顺序重排分组。
func (s *Service) ReorderGroups(ids []uint) error {
	return s.admin().ReorderGroups(ids)
}

// GetGroup 按 id 获取分组。
func (s *Service) GetGroup(id uint) (*storage.GatewayGroup, error) {
	return s.admin().GetGroup(id)
}

// CreateKey 创建网关 API Key（明文仅返回一次）。
func (s *Service) CreateKey(in CreateKeyInput) (*CreateKeyResult, error) {
	return s.admin().CreateKey(in)
}

// UpdateKey 更新网关 API Key。
func (s *Service) UpdateKey(id uint, in UpdateKeyInput) (*storage.GatewayKey, error) {
	return s.admin().UpdateKey(id, in)
}

// DeleteKey 删除网关 API Key。
func (s *Service) DeleteKey(id uint) error {
	return s.admin().DeleteKey(id)
}

// RevealKey 解密并返回密钥明文（管理端）。
func (s *Service) RevealKey(id uint) (string, error) {
	return s.admin().RevealKey(id)
}

// ListKeysByGroup 列出某分组下的密钥。
func (s *Service) ListKeysByGroup(groupID uint) ([]storage.GatewayKey, error) {
	return s.admin().ListKeysByGroup(groupID)
}

// ListProviders 分页列出直连渠道。
func (s *Service) ListProviders(q storage.GatewayProviderQuery) (*storage.GatewayProviderPage, error) {
	return s.admin().ListProviders(q)
}

// ListProviderOptions 列出直连渠道选项（下拉用）。
func (s *Service) ListProviderOptions(query string) ([]storage.GatewayProvider, error) {
	return s.admin().ListProviderOptions(query)
}

// CreateProvider 创建直连渠道。
func (s *Service) CreateProvider(in CreateProviderInput) (*storage.GatewayProvider, error) {
	return s.admin().CreateProvider(in)
}

// UpdateProvider 更新直连渠道。
func (s *Service) UpdateProvider(id uint, in UpdateProviderInput) (*storage.GatewayProvider, error) {
	return s.admin().UpdateProvider(id, in)
}

// DeleteProvider 删除直连渠道。
func (s *Service) DeleteProvider(id uint) error {
	return s.admin().DeleteProvider(id)
}

// RevealProviderKey 解密并返回直连渠道 API Key 明文。
func (s *Service) RevealProviderKey(id uint) (string, error) {
	return s.admin().RevealProviderKey(id)
}

// ListRoutes 列出分组路由（库内 position 顺序）。
func (s *Service) ListRoutes(groupID uint) ([]storage.GatewayRoute, error) {
	return s.admin().ListRoutes(groupID)
}

func (s *Service) orderRoutesForGroup(groupID uint, list []storage.GatewayRoute) ([]storage.GatewayRoute, error) {
	return s.admin().orderRoutesForGroup(groupID, list)
}

func (s *Service) reorderRoutesPersisted(groupID uint) error {
	return s.admin().reorderRoutesPersisted(groupID)
}

// ResortRoutesOnRateScan 倍率扫描后对开启重排的组重写路由顺序。
func (s *Service) ResortRoutesOnRateScan(ctx context.Context) {
	s.admin().ResortRoutesOnRateScan(ctx)
}

// SaveRoutes 整组保存路由并按策略重排。
func (s *Service) SaveRoutes(groupID uint, inputs []RouteInput) ([]storage.GatewayRoute, error) {
	return s.admin().SaveRoutes(groupID, inputs)
}

// EnsureRouteKeys 为分组各路由 Ensure/绑定上游 API Key。
func (s *Service) EnsureRouteKeys(ctx context.Context, groupID uint) (*EnsureKeysResult, error) {
	return s.admin().EnsureRouteKeys(ctx, groupID)
}

func (s *Service) BindRouteKey(ctx context.Context, groupID, routeID uint, in BindRouteKeyInput) (*storage.GatewayRoute, error) {
	return s.admin().BindRouteKey(ctx, groupID, routeID, in)
}

func (s *Service) ensureRouteKeyResult(ctx context.Context, groupID uint, r *storage.GatewayRoute) EnsureKeyRouteResult {
	return s.admin().ensureRouteKeyResult(ctx, groupID, r)
}

func (s *Service) ensureSourceAPIKey(ctx context.Context, groupID uint, route *storage.GatewayRoute) error {
	return s.admin().ensureSourceAPIKey(ctx, groupID, route)
}

// ClearRoutePause 清除路由临时不可调度状态。
func (s *Service) ClearRoutePause(routeID uint) error {
	return s.admin().ClearRoutePause(routeID)
}

func (s *Service) collectGroupModels(ctx context.Context, groupID uint) (preview []ModelPreviewItem, routeResults []ModelSyncRouteResult, err error) {
	return s.admin().collectGroupModels(ctx, groupID)
}

func (s *Service) pullRouteModels(ctx context.Context, group *storage.GatewayGroup, route storage.GatewayRoute) routeModelPull {
	return s.admin().pullRouteModels(ctx, group, route)
}

// PreviewGroupModels 预览分组可聚合的模型清单（不落库）。
func (s *Service) PreviewGroupModels(ctx context.Context, groupID uint) ([]ModelPreviewItem, error) {
	return s.admin().PreviewGroupModels(ctx, groupID)
}

// SyncGroupModels 从各路由拉取模型并写回分组 models_json。
func (s *Service) SyncGroupModels(ctx context.Context, groupID uint, in SyncGroupModelsInput) (*ModelSyncResult, error) {
	return s.admin().SyncGroupModels(ctx, groupID, in)
}

// TestGroupModel 向路由发最小请求探测模型是否可用。
func (s *Service) TestGroupModel(ctx context.Context, groupID uint, in TestModelInput) ([]ModelTestResult, error) {
	return s.admin().TestGroupModel(ctx, groupID, in)
}

func (s *Service) probeRouteModel(parent context.Context, group *storage.GatewayGroup, route storage.GatewayRoute, requestedModel string, groupMapping map[string]string) ModelTestResult {
	return s.admin().probeRouteModel(parent, group, route, requestedModel, groupMapping)
}

func (s *Service) invalidateModelsCache(groupID uint) {
	s.admin().invalidateModelsCache(groupID)
}
