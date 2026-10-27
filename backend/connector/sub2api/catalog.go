// 上游模型目录（Sub2API）：该上游不提供「分组 → 模型」映射。
//
// 实测（2026-10）：/api/pricing、/api/v1/models、/api/v1/pricing 均为 404；
// /api/v1/channels/available 的结构是「平台 → {分组列表, 模型列表}」，分组与模型是
// 同一平台下的两个平行数组，模型对象里没有 group 字段。因此只能给出平台级模型池，
// 不能回答「这个分组包含哪些模型」。
//
// 这里显式实现 CatalogProvider 并返回「不支持」，让上层能区分
// 「还没实现」与「上游根本没有这个能力」。
package sub2api

import (
	"context"

	"github.com/bejix/upstream-ops/backend/connector"
)

// GetCatalog Sub2API 无分组级模型映射，固定返回降级结果。
func (c *Client) GetCatalog(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.Catalog, error) {
	return &connector.Catalog{
		Capability: connector.CatalogCapabilityPlatformPool,
		Degrade:    connector.CatalogDegradeNoPricingAPI,
		DegradeMessage: "Sub2API 不提供「分组 → 模型」映射：" +
			"上游 /api/v1/channels/available 的分组与模型是同一平台下的平行集合，无归属关系",
		Notes: []string{
			"该站点类型没有公开的定价/模型目录接口，无法列出分组内模型",
		},
	}, nil
}
