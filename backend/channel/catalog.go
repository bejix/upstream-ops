// 渠道 · 上游模型目录：把 connector.CatalogProvider 包成带缓存的服务方法。
package channel

import (
	"context"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
)

// catalogTTL 上游目录缓存有效期。
//
// 与 apiKeyGroupSetTTL(30s) 不同，目录数据（模型清单、分组倍率）变化远没那么频繁，
// 而一次请求可能带有上百个模型；选 5 分钟既能让用户「点开就有」，
// 又不至于长时间展示过期价格。手动刷新（?fresh=1）可绕过缓存。
const catalogTTL = 5 * time.Minute

type catalogCacheEntry struct {
	catalog   *connector.Catalog
	fetchedAt time.Time
}

// GetUpstreamCatalog 返回渠道的上游模型目录（含分组与模型的归属关系）。
//
// fresh=true 时跳过缓存强制拉取。缓存命中不产生上游请求，因此前端切分组可以放心调用。
//
// 返回值语义：
//   - catalog 非 nil 且 Degrade==""：正常数据
//   - catalog 非 nil 且 Degrade!=""：上游给出了降级原因（如 Sub2API 无此能力），仍应展示分组配置
//   - catalog != nil 同时 err != nil：拉取失败但带有错误摘要，前端展示错误 + 重试
func (s *Service) GetUpstreamCatalog(ctx context.Context, channelID uint, fresh bool) (*connector.Catalog, error) {
	if !fresh {
		if c, ok := s.loadCatalogCache(channelID); ok {
			return c, nil
		}
	}

	c, resolved, conn, session, err := s.prepareConnectorCall(ctx, channelID)
	if err != nil {
		// 连登录都失败：仍返回带降级的空目录，让前端能展示「需要重新登录」而不是纯报错。
		return &connector.Catalog{
			Capability:     connector.CatalogCapabilityGroupModels,
			Degrade:        connector.CatalogDegradeAuthExpired,
			DegradeMessage: err.Error(),
		}, err
	}

	provider, ok := conn.(connector.CatalogProvider)
	if !ok {
		cat := &connector.Catalog{
			Capability:     connector.CatalogCapabilityPlatformPool,
			Degrade:        connector.CatalogDegradeNoPricingAPI,
			DegradeMessage: "该上游类型（" + string(c.Type) + "）未实现上游模型目录能力",
		}
		s.storeCatalogCache(channelID, cat)
		return cat, nil
	}

	cat, catErr := provider.GetCatalog(ctx, resolved, session)
	if cat == nil {
		cat = &connector.Catalog{
			Capability:     connector.CatalogCapabilityGroupModels,
			Degrade:        connector.CatalogDegradeFetchFailed,
			DegradeMessage: errString(catErr),
		}
	}
	if catErr != nil {
		// 失败不写缓存：下次进来重试上游，避免把偶发错误固化 5 分钟。
		return cat, catErr
	}
	_ = s.Channels.SetLastError(c.ID, "")
	s.storeCatalogCache(channelID, cat)
	return cat, nil
}

// InvalidateUpstreamCatalog 清掉某渠道的目录缓存（渠道配置变更后调用）。
func (s *Service) InvalidateUpstreamCatalog(channelID uint) {
	if s == nil {
		return
	}
	s.catalogCacheMu.Lock()
	delete(s.catalogCache, channelID)
	s.catalogCacheMu.Unlock()
}

func (s *Service) loadCatalogCache(channelID uint) (*connector.Catalog, bool) {
	s.catalogCacheMu.Lock()
	defer s.catalogCacheMu.Unlock()
	entry, ok := s.catalogCache[channelID]
	if !ok || entry.catalog == nil {
		return nil, false
	}
	if time.Since(entry.fetchedAt) > catalogTTL {
		delete(s.catalogCache, channelID)
		return nil, false
	}
	return entry.catalog, true
}

func (s *Service) storeCatalogCache(channelID uint, cat *connector.Catalog) {
	s.catalogCacheMu.Lock()
	if s.catalogCache == nil {
		s.catalogCache = make(map[uint]*catalogCacheEntry)
	}
	s.catalogCache[channelID] = &catalogCacheEntry{catalog: cat, fetchedAt: time.Now()}
	s.catalogCacheMu.Unlock()
}
