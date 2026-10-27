// 上游分组内的模型目录：可选连接器能力。
//
// 与主 Connector 接口解耦：只有实现了 CatalogProvider 的上游类型才能提供
// 「分组 → 模型」映射，调用方用类型断言判断，未实现时降级而不是报错。
package connector

import "context"

// CatalogCapability 描述上游能否给出「分组 → 模型」的精确归属。
type CatalogCapability string

const (
	// CatalogCapabilityGroupModels 上游给出每个模型所属分组（NewAPI 的 enable_groups）。
	CatalogCapabilityGroupModels CatalogCapability = "group_models"
	// CatalogCapabilityPlatformPool 上游只有「平台级模型池」，分组与模型是平行集合，无映射。
	CatalogCapabilityPlatformPool CatalogCapability = "platform_pool"
)

// CatalogDegrade 目录降级原因；normal 表示无降级。
type CatalogDegrade string

const (
	CatalogDegradeNone         CatalogDegrade = ""
	CatalogDegradeAuthExpired  CatalogDegrade = "auth_expired"
	CatalogDegradeFetchFailed  CatalogDegrade = "fetch_failed"
	CatalogDegradeNoPricingAPI CatalogDegrade = "no_pricing_api"
)

// CatalogGroup 一个上游分组。Authorized 表示该分组出现在当前账号的可用分组列表里；
// 为 false 时 Ratio/Description 通常为空（账号无权限，拿不到配置），但模型可能仍然引用了它。
type CatalogGroup struct {
	Name         string  `json:"name"`
	RemoteID     *int64  `json:"remote_id,omitempty"`
	Description  string  `json:"description,omitempty"`
	Ratio        float64 `json:"ratio"`
	HasRatio     bool    `json:"has_ratio"`
	Authorized   bool    `json:"authorized"`
	ModelCount   int     `json:"model_count"`
	Platform     string  `json:"platform,omitempty"`
	ChannelGroup string  `json:"channel_group,omitempty"`
}

// CatalogModel 一个上游模型。Groups 是它所属的全部分组（可能多个）。
//
// BillingBrief 是服务端对 BillingExpr 的可读摘要（如「入×4 · 出×20」），
// 让前端不必重复实现表达式解析；BillingExpr 原文放在 tooltip 里。
type CatalogModel struct {
	Name             string   `json:"name"`
	Groups           []string `json:"groups"`
	Vendor           string   `json:"vendor,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	BillingMode      string   `json:"billing_mode,omitempty"`
	BillingExpr      string   `json:"billing_expr,omitempty"`
	BillingBrief     string   `json:"billing_brief,omitempty"`
	ModelRatio       float64  `json:"model_ratio"`
	CompletionRatio  float64  `json:"completion_ratio"`
	ModelPrice       float64  `json:"model_price"`
	QuotaType        int      `json:"quota_type"`
	CacheRatio       *float64 `json:"cache_ratio,omitempty"`
	CreateCacheRatio *float64 `json:"create_cache_ratio,omitempty"`
	Endpoints        []string `json:"endpoints,omitempty"`
}

// Catalog 一次拉取得到的上游目录。
type Catalog struct {
	Capability     CatalogCapability `json:"capability"`
	Degrade        CatalogDegrade    `json:"degrade"`
	DegradeMessage string            `json:"degrade_message,omitempty"`
	PricingVersion string            `json:"pricing_version,omitempty"`
	Groups         []CatalogGroup    `json:"groups"`
	Models         []CatalogModel    `json:"models"`
	Notes          []string          `json:"notes,omitempty"`
}

// CatalogProvider 由支持上游模型目录的 connector 实现。
type CatalogProvider interface {
	// GetCatalog 拉取上游模型目录。session 为 nil 时应尝试匿名访问。
	GetCatalog(ctx context.Context, ch *Channel, session *AuthSession) (*Catalog, error)
}
