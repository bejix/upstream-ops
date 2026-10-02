// 管理面与鉴权相关输入输出 DTO。
package gateway

import "github.com/bejix/upstream-ops/backend/storage"

// AuthResult 鉴权结果：命中的网关密钥及其所属分组。
type AuthResult struct {
	Key   *storage.GatewayKey
	Group *storage.GatewayGroup
}

// CreateGroupInput 创建网关分组。
type CreateGroupInput struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	RateSortDirection     string `json:"rate_sort_direction"`
	RateResortEnabled     *bool  `json:"rate_resort_enabled"`
	ModelMappingJSON      string `json:"model_mapping"`
	ModelsJSON            string `json:"models_json"`
	ModelsMode            string `json:"models_mode"`
	ServiceTierRulesJSON  string `json:"service_tier_rules_json"`
	SystemPromptRulesJSON string `json:"system_prompt_rules_json"`
	RetryEnabled          *bool  `json:"retry_enabled"`
	RetryCount            *int   `json:"retry_count"`
	FailoverEnabled       *bool  `json:"failover_enabled"`
	FailoverMax           *int   `json:"failover_max"`
	FailoverOn4xx         *bool  `json:"failover_on_4xx"`
	CooldownSeconds       *int   `json:"cooldown_seconds"`
	FirstTokenTimeoutSec  *int   `json:"first_token_timeout_sec"`
	// UserAgent 组级统一 UA；路由 mode=group 时使用。
	UserAgent string `json:"user_agent"`
}

// UpdateGroupInput 更新网关分组（指针字段：nil 表示不改）。
type UpdateGroupInput struct {
	Name                  *string `json:"name"`
	Description           *string `json:"description"`
	Status                *string `json:"status"`
	RateSortDirection     *string `json:"rate_sort_direction"`
	RateResortEnabled     *bool   `json:"rate_resort_enabled"`
	ModelMappingJSON      *string `json:"model_mapping"`
	ModelsJSON            *string `json:"models_json"`
	ModelsMode            *string `json:"models_mode"`
	ServiceTierRulesJSON  *string `json:"service_tier_rules_json"`
	SystemPromptRulesJSON *string `json:"system_prompt_rules_json"`
	RetryEnabled          *bool   `json:"retry_enabled"`
	RetryCount            *int    `json:"retry_count"`
	FailoverEnabled       *bool   `json:"failover_enabled"`
	FailoverMax           *int    `json:"failover_max"`
	FailoverOn4xx         *bool   `json:"failover_on_4xx"`
	CooldownSeconds       *int    `json:"cooldown_seconds"`
	FirstTokenTimeoutSec  *int    `json:"first_token_timeout_sec"`
	// UserAgent 空串=清除组级 UA。
	UserAgent *string `json:"user_agent"`
}

// CreateKeyInput 创建网关 API Key。
type CreateKeyInput struct {
	GroupID         uint    `json:"group_id"`
	Name            string  `json:"name"`
	Quota           float64 `json:"quota"`
	IPWhitelistJSON string  `json:"ip_whitelist"`
	IPBlacklistJSON string  `json:"ip_blacklist"`
	CustomKey       string  `json:"custom_key"`
	// KeyLen 自动生成时 sk- 之后的字符长度：16/24/32/48/64（仅 custom_key 为空时生效；默认 48）
	KeyLen int `json:"key_len"`
}

// CreateKeyResult 创建密钥结果；Secret 仅创建时返回一次明文。
type CreateKeyResult struct {
	Key    storage.GatewayKey `json:"key"`
	Secret string             `json:"secret"`
}

// UpdateKeyInput 更新网关 API Key（指针字段：nil 表示不改）。
type UpdateKeyInput struct {
	Name            *string  `json:"name"`
	Status          *string  `json:"status"`
	Quota           *float64 `json:"quota"`
	IPWhitelistJSON *string  `json:"ip_whitelist"`
	IPBlacklistJSON *string  `json:"ip_blacklist"`
	ResetQuotaUsed  *bool    `json:"reset_quota_used"`
}

// CreateProviderInput 创建直连渠道（GatewayProvider）。
type CreateProviderInput struct {
	Name               string  `json:"name"`
	BaseURL            string  `json:"base_url"`
	APIKey             string  `json:"api_key"`
	UpstreamProtocol   string  `json:"upstream_protocol"`
	DefaultBillingRate float64 `json:"default_billing_rate"`
	AuthStyle          string  `json:"auth_style"`
	Enabled            *bool   `json:"enabled"`
	ProxyEnabled       *bool   `json:"proxy_enabled"`
	ExtraHeadersJSON   string  `json:"extra_headers"`
	Notes              string  `json:"notes"`
}

// UpdateProviderInput 更新直连渠道（指针字段：nil 表示不改；APIKey 空或省略=不改）。
type UpdateProviderInput struct {
	Name               *string  `json:"name"`
	BaseURL            *string  `json:"base_url"`
	APIKey             *string  `json:"api_key"`
	UpstreamProtocol   *string  `json:"upstream_protocol"`
	DefaultBillingRate *float64 `json:"default_billing_rate"`
	AuthStyle          *string  `json:"auth_style"`
	Enabled            *bool    `json:"enabled"`
	ProxyEnabled       *bool    `json:"proxy_enabled"`
	ExtraHeadersJSON   *string  `json:"extra_headers"`
	Notes              *string  `json:"notes"`
}

// RouteInput 保存路由时的单条输入。
type RouteInput struct {
	ID                    uint    `json:"id"`
	SourceKind            string  `json:"source_kind"`
	SourceChannelID       uint    `json:"source_channel_id"`
	GatewayProviderID     uint    `json:"gateway_provider_id"`
	SourceGroupID         *int64  `json:"source_group_id"`
	SourceGroupName       string  `json:"source_group_name"`
	Weight                int     `json:"weight"`
	RateConvertMode       string  `json:"rate_convert_mode"`
	RateConvertValue      float64 `json:"rate_convert_value"`
	BillingRateMultiplier float64 `json:"billing_rate_multiplier"`
	Enabled               bool    `json:"enabled"`
	ModelMappingJSON      string  `json:"model_mapping"`
	UpstreamProtocol      string  `json:"upstream_protocol"`
	Concurrency           int     `json:"concurrency"`
	// UserAgentMode: passthrough | group | custom
	UserAgentMode   string `json:"user_agent_mode"`
	UserAgentCustom string `json:"user_agent_custom"`
}

type BindRouteKeyInput struct {
	SourceAPIKeyID int64 `json:"source_api_key_id"`
}

type RouteKeyBindingError struct {
	Code    string
	Message string
	cause   error
}

func (e *RouteKeyBindingError) Error() string {
	return e.Message
}

func (e *RouteKeyBindingError) Unwrap() error {
	return e.cause
}

// EnsureKeyRouteResult 单条路由 Ensure 上游密钥的结果。
type EnsureKeyRouteResult struct {
	RouteID      uint   `json:"route_id"`
	SourceKind   string `json:"source_kind"`
	ChannelID    uint   `json:"channel_id,omitempty"`
	ChannelName  string `json:"channel_name,omitempty"`
	ProviderID   uint   `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Label        string `json:"label"`
	KeyName      string `json:"key_name,omitempty"`
	OK           bool   `json:"ok"`
	Skipped      bool   `json:"skipped"`
	Error        string `json:"error,omitempty"`
	SkipReason   string `json:"skip_reason,omitempty"`
}

// EnsureKeysResult 组内全部路由 Ensure 密钥的汇总。
type EnsureKeysResult struct {
	Items     []storage.GatewayRoute `json:"items"`
	Routes    []EnsureKeyRouteResult `json:"routes"`
	OKCount   int                    `json:"ok_count"`
	FailCount int                    `json:"fail_count"`
	SkipCount int                    `json:"skip_count"`
}

// ModelSource 模型来源标注（某路由/渠道/源分组）。
type ModelSource struct {
	RouteID         uint   `json:"route_id,omitempty"`
	ChannelID       uint   `json:"channel_id"`
	ChannelName     string `json:"channel_name,omitempty"`
	SourceGroupID   *int64 `json:"source_group_id,omitempty"`
	SourceGroupName string `json:"source_group_name,omitempty"`
}

// ModelListItem 组内模型清单项（sync 来自上游，custom 为手工）。
type ModelListItem struct {
	ID         string        `json:"id"`
	Source     string        `json:"source"` // sync | custom
	ChannelIDs []uint        `json:"channel_ids,omitempty"`
	Sources    []ModelSource `json:"sources,omitempty"`
}

// ModelPreviewItem 模型预览项（尚未落库的合并结果）。
type ModelPreviewItem struct {
	ID         string        `json:"id"`
	ChannelIDs []uint        `json:"channel_ids"`
	Sources    []ModelSource `json:"sources"`
}

// ModelSyncRouteResult 单条路由同步模型结果（失败跳过，不中断整体）。
type ModelSyncRouteResult struct {
	RouteID      uint   `json:"route_id"`
	SourceKind   string `json:"source_kind"`
	ChannelID    uint   `json:"channel_id,omitempty"`
	ChannelName  string `json:"channel_name,omitempty"`
	ProviderID   uint   `json:"provider_id,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Label        string `json:"label"`
	OK           bool   `json:"ok"`
	Skipped      bool   `json:"skipped"`
	ModelCount   int    `json:"model_count"`
	Error        string `json:"error,omitempty"`
	SkipReason   string `json:"skip_reason,omitempty"`
}

// ModelSyncResult 组模型同步结果。
type ModelSyncResult struct {
	Group      *storage.GatewayGroup  `json:"group"`
	ModelCount int                    `json:"model_count"`
	Routes     []ModelSyncRouteResult `json:"routes"`
	OKCount    int                    `json:"ok_count"`
	FailCount  int                    `json:"fail_count"`
	SkipCount  int                    `json:"skip_count"`
}

// routeModelPull 单路由拉模型中间结果（并发拉取后主协程按路由序合并去重）。
type routeModelPull struct {
	rr     ModelSyncRouteResult
	models []string
	src    ModelSource
	merge  bool // true 时把 models/src 并入 byModel
}

// SyncGroupModelsInput 同步组模型时可附带前端本地尚未落库的自定义模型。
type SyncGroupModelsInput struct {
	// CustomModels 与库内 models_json 中 source=custom 的项合并保留。
	CustomModels []ModelListItem `json:"custom_models"`
}

// TestModelInput 模型可用性探测参数。
type TestModelInput struct {
	Model   string `json:"model"`
	RouteID *uint  `json:"route_id"` // 指定则只测该路由；省略则测所有可调度路由
}

// ModelTestResult 单条路由探测结果。
type ModelTestResult struct {
	RouteID           uint   `json:"route_id"`
	SourceKind        string `json:"source_kind,omitempty"`
	ChannelID         uint   `json:"channel_id"`
	ChannelName       string `json:"channel_name"`
	GatewayProviderID uint   `json:"gateway_provider_id,omitempty"`
	SourceGroupID     *int64 `json:"source_group_id,omitempty"`
	SourceGroupName   string `json:"source_group_name,omitempty"`
	Label             string `json:"label"` // 渠道-分组 或 直连渠道名
	RequestedModel    string `json:"requested_model"`
	UpstreamModel     string `json:"upstream_model"`
	UpstreamPath      string `json:"upstream_path,omitempty"`
	OK                bool   `json:"ok"`
	StatusCode        int    `json:"status_code"`
	LatencyMS         int64  `json:"latency_ms"`
	Error             string `json:"error,omitempty"`
}
