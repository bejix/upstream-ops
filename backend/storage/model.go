package storage

import (
	"strings"
	"time"
)

// ChannelType 上游渠道类型。
type ChannelType string

const (
	ChannelTypeNewAPI  ChannelType = "newapi"
	ChannelTypeSub2API ChannelType = "sub2api"
)

// CredentialMode 渠道凭据模式：
//   - password: 经典模式，存账号 + 密码，由 Connector 走完整登录流程
//   - token:    跳过登录，存用户已有的 cookie / access_token，直接构造 AuthSession
//
// token 模式不依赖打码 / 不会自动续期，token 失效时表现为 last_error 显示鉴权失败。
type CredentialMode string

const (
	CredentialModePassword CredentialMode = "password"
	CredentialModeToken    CredentialMode = "token"
)

// Channel 上游渠道账号。Password / Turnstile API key 等敏感字段都加密保存。
//
// 注意：会话凭据（access_token / refresh_token / cookie / csrf）单独存放在 AuthSession 表。
//
// CredentialMode + PasswordCipher 的语义重载：
//   - password 模式（默认）：Username + PasswordCipher 存账号密码，由 Connector.Login 用
//   - token    模式：PasswordCipher 存 JSON blob（NewAPI: {cookie,user_id} / Sub2API: {access_token,refresh_token}），
//     channel.Service 解析后直接构造 AuthSession，跳过 Login。Username 字段在 token 模式下保留
//     用户填写的备注（一般是邮箱），仅做展示。
//
// 复用 PasswordCipher 而不新增 TokenCipher 是为了让现有的 GORM 行 / 加密路径 / 迁移流程零变动。
type Channel struct {
	ID                     uint           `gorm:"primaryKey" json:"id"`
	Name                   string         `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Type                   ChannelType    `gorm:"size:32;not null;index" json:"type"`
	SiteURL                string         `gorm:"size:512;not null" json:"site_url"`
	Username               string         `gorm:"size:256;not null" json:"username"`
	SortOrder              int            `gorm:"not null;default:1" json:"sort_order"`
	PasswordCipher         string         `gorm:"size:4096;not null" json:"-"`
	CredentialMode         CredentialMode `gorm:"size:16;not null;default:'password'" json:"credential_mode"`
	LoginExtraParams       string         `gorm:"type:text" json:"login_extra_params"`
	TurnstileEnabled       bool           `gorm:"default:false" json:"turnstile_enabled"`
	IgnoreAnnouncements    bool           `gorm:"default:false" json:"ignore_announcements"`
	SubscriptionEnabled    bool           `gorm:"default:false" json:"subscription_enabled"`
	ProxyEnabled           bool           `gorm:"default:false" json:"proxy_enabled"`
	CaptchaConfigID        *uint          `json:"captcha_config_id,omitempty"`
	BalanceThreshold       float64        `gorm:"default:0" json:"balance_threshold"`
	RechargeMultiplier     *float64       `json:"recharge_multiplier,omitempty"`
	RechargeMultiplierMode string         `gorm:"size:16;not null;default:'divide'" json:"recharge_multiplier_mode"`
	MonitorEnabled         bool           `gorm:"default:true" json:"monitor_enabled"`
	// OnlyCreatedKeyGroupsEnabled 开启后仅展示与监听"已创建密钥的分组"。
	// 影响：渠道卡片 / 分组对话框 / 全局倍率面板的分组列表，以及倍率变动通知。
	// 不影响 RateSnapshot 本地存储（始终保留全部分组），网关、通知设置
	// 等仍可拿到原始全部分组。
	OnlyCreatedKeyGroupsEnabled bool `gorm:"default:false" json:"only_created_key_groups_enabled"`

	// 用户自定义的标签与备注，仅用于展示与筛选，不参与监控逻辑。
	// Tags 的存储格式见 ChannelTags。
	Tags  ChannelTags `gorm:"size:1024;not null;default:''" json:"tags"`
	Notes string      `gorm:"type:text" json:"notes"`

	// 最近一次采集结果（聚合视图，便于列表页直接展示）
	LastBalance   *float64   `json:"last_balance,omitempty"`
	LastBalanceAt *time.Time `json:"last_balance_at,omitempty"`
	TodayCost     *float64   `json:"today_cost,omitempty"`
	TodayCostAt   *time.Time `json:"today_cost_at,omitempty"` // 最近一次写入 TodayCost 的时间，用于跨天判断
	TotalCost     *float64   `json:"total_cost,omitempty"`
	LastError     string     `gorm:"type:text" json:"last_error,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Channel) TableName() string { return "channels" }

// EffectiveTodayCost 返回以 now 所在日期为准的"今日消费"。
//
// TodayCost 只在监控采集成功时刷新；暂停监控（或采集持续失败）后会一直停留在最后一次
// 采样的那天。这里比较 TodayCostAt 与 now 的日期：采样日早于今天时返回 0，否则原样返回。
// TodayCostAt 为空时无法判断，原样返回。
//
// 日期按消费趋势统计同一个时区（trendLocation，Asia/Shanghai）划分，与服务器进程的时区
// 和 now 自带的时区无关，这样"今日消费"归零与趋势图换日发生在同一时刻。
//
// 不回退到 LastBalanceAt：余额可以单独刷新（兑换码充值、消费采集失败等），它是今天
// 并不代表 TodayCost 也是今天采的。升级前的旧数据由 AutoMigrate 一次性回填 TodayCostAt
// （见 backfillChannelTodayCostAt）。
//
// 仅用于 API 输出层，结果不要写回数据库。
func (c Channel) EffectiveTodayCost(now time.Time) *float64 {
	if c.TodayCost == nil || c.TodayCostAt == nil {
		return c.TodayCost
	}
	if dayStart(*c.TodayCostAt).Before(dayStart(now)) {
		zero := 0.0
		return &zero
	}
	return c.TodayCost
}

// AuthSession 渠道登录后保存的凭据，按 ChannelID 一对一关联。
// *Cipher 字段都用 AES-GCM 加密；UserID 是上游账号 ID 字符串（非敏感），明文存放。
type AuthSession struct {
	ChannelID          uint       `gorm:"primaryKey" json:"channel_id"`
	UserID             string     `gorm:"size:64" json:"user_id,omitempty"`
	AccessTokenCipher  string     `gorm:"type:text" json:"-"`
	RefreshTokenCipher string     `gorm:"type:text" json:"-"`
	CookieCipher       string     `gorm:"type:text" json:"-"`
	CSRFTokenCipher    string     `gorm:"size:1024" json:"-"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (AuthSession) TableName() string { return "auth_sessions" }

// CaptchaProviderType 打码平台类型。
type CaptchaProviderType string

const (
	CaptchaCapSolver   CaptchaProviderType = "capsolver"
	CaptchaTwoCaptcha  CaptchaProviderType = "2captcha"
	CaptchaAntiCaptcha CaptchaProviderType = "anticaptcha"
	CaptchaYesCaptcha  CaptchaProviderType = "yescaptcha"
)

// CaptchaConfig 打码平台配置。APIKeyCipher 加密保存，Extra 存放各平台差异化 JSON。
type CaptchaConfig struct {
	ID           uint                `gorm:"primaryKey" json:"id"`
	Name         string              `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Type         CaptchaProviderType `gorm:"size:32;not null;index" json:"type"`
	APIKeyCipher string              `gorm:"size:1024" json:"-"`
	Endpoint     string              `gorm:"size:512" json:"endpoint,omitempty"`
	Extra        string              `gorm:"type:text" json:"extra,omitempty"`
	Enabled      bool                `gorm:"default:true" json:"enabled"`
	ProxyEnabled bool                `gorm:"default:false" json:"proxy_enabled"`
	LastBalance  *float64            `json:"last_balance,omitempty"`
	BalanceUnit  string              `gorm:"size:32" json:"balance_unit,omitempty"`
	BalanceAt    *time.Time          `json:"balance_at,omitempty"`
	BalanceError string              `gorm:"type:text" json:"balance_error,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

func (CaptchaConfig) TableName() string { return "captcha_configs" }

// RateSnapshot 渠道当前观察到的模型 / 分组倍率快照。upsert per (channel_id, model_name)。
// 实际的"变化历史"在 RateChangeLog；此表只保存当前状态。
type RateSnapshot struct {
	ID              uint    `gorm:"primaryKey" json:"id"`
	ChannelID       uint    `gorm:"not null;uniqueIndex:idx_rate_chan_model" json:"channel_id"`
	RemoteGroupID   *int64  `json:"remote_group_id,omitempty"`
	ModelName       string  `gorm:"size:256;not null;uniqueIndex:idx_rate_chan_model" json:"model_name"`
	Description     string  `gorm:"size:512" json:"description,omitempty"`
	Ratio           float64 `gorm:"not null" json:"ratio"`
	CompletionRatio float64 `json:"completion_ratio"`

	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

func (RateSnapshot) TableName() string { return "rate_snapshots" }

// RateChangeLog 倍率变化历史。每次扫描发现差异时写入一行。
type RateChangeLog struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ChannelID          uint      `gorm:"not null;index" json:"channel_id"`
	ModelName          string    `gorm:"size:256;not null;index" json:"model_name"`
	OldRatio           *float64  `json:"old_ratio,omitempty"`
	NewRatio           float64   `gorm:"not null" json:"new_ratio"`
	OldCompletionRatio *float64  `json:"old_completion_ratio,omitempty"`
	NewCompletionRatio float64   `json:"new_completion_ratio"`
	ChangedAt          time.Time `gorm:"not null;index" json:"changed_at"`
}

func (RateChangeLog) TableName() string { return "rate_change_logs" }

// UpstreamAnnouncement 保存从上游渠道同步到的公告。
type UpstreamAnnouncement struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ChannelID       uint       `gorm:"not null;uniqueIndex:idx_announcement_chan_source;index" json:"channel_id"`
	SourceKey       string     `gorm:"size:512;not null;uniqueIndex:idx_announcement_chan_source" json:"source_key"`
	Title           string     `gorm:"size:512" json:"title,omitempty"`
	Content         string     `gorm:"type:text;not null" json:"content"`
	Type            string     `gorm:"size:64" json:"type,omitempty"`
	Link            string     `gorm:"size:512" json:"link,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`
	FirstSeenAt     time.Time  `gorm:"not null;index" json:"first_seen_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (UpstreamAnnouncement) TableName() string { return "upstream_announcements" }

// BalanceSnapshot 周期性余额采样，用于图表展示。
type BalanceSnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ChannelID uint      `gorm:"not null;index" json:"channel_id"`
	Balance   float64   `gorm:"not null" json:"balance"`
	SampledAt time.Time `gorm:"not null;index" json:"sampled_at"`
}

func (BalanceSnapshot) TableName() string { return "balance_snapshots" }

// CostSnapshot 周期性消费采样，用于图表展示。
type CostSnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ChannelID uint      `gorm:"not null;index" json:"channel_id"`
	TodayCost float64   `gorm:"not null" json:"today_cost"`
	SampledAt time.Time `gorm:"not null;index" json:"sampled_at"`
}

func (CostSnapshot) TableName() string { return "cost_snapshots" }

// NotificationChannelType 通知渠道类型。第一版至少 telegram，其它预留。
type NotificationChannelType string

const (
	NotifyTelegram    NotificationChannelType = "telegram"
	NotifyWebhook     NotificationChannelType = "webhook"
	NotifyEmail       NotificationChannelType = "email"
	NotifyWecom       NotificationChannelType = "wecom"
	NotifyDingTalk    NotificationChannelType = "dingtalk"
	NotifyFeishu      NotificationChannelType = "feishu"
	NotifyServerChan3 NotificationChannelType = "serverchan3"
)

// NotificationChannel 通知渠道配置。ConfigCipher 加密保存 JSON 配置（含 token / webhook url / 密码等）。
//
// Subscriptions 是 JSON 数组，记录该渠道关心的上游、事件和分组过滤；为空 / "[]" 表示订阅一切。
// 非敏感数据，明文保存，方便 Dispatcher 直接读取过滤而不解密。
type NotificationChannel struct {
	ID            uint                    `gorm:"primaryKey" json:"id"`
	Name          string                  `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Type          NotificationChannelType `gorm:"size:32;not null;index" json:"type"`
	ConfigCipher  string                  `gorm:"type:text;not null" json:"-"`
	Subscriptions string                  `gorm:"size:4096;not null;default:'[]'" json:"subscriptions"`
	Enabled       bool                    `gorm:"default:true" json:"enabled"`
	ProxyEnabled  bool                    `gorm:"default:false" json:"proxy_enabled"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

func (NotificationChannel) TableName() string { return "notification_channels" }

// NotificationEvent 系统内部触发的通知事件类型。
type NotificationEvent string

const (
	EventBalanceLow                     NotificationEvent = "balance_low"
	EventRateChanged                    NotificationEvent = "rate_changed"
	EventRateStructureChanged           NotificationEvent = "rate_structure_changed"
	EventRateAdded                      NotificationEvent = "rate_added"
	EventRateRemoved                    NotificationEvent = "rate_removed"
	EventAnnouncement                   NotificationEvent = "announcement"
	EventLoginFailed                    NotificationEvent = "login_failed"
	EventCaptchaFailed                  NotificationEvent = "captcha_failed"
	EventMonitorFailed                  NotificationEvent = "monitor_failed"
	EventSubscriptionDailyLow           NotificationEvent = "subscription_daily_remaining_low"
	EventSubscriptionWeeklyLow          NotificationEvent = "subscription_weekly_remaining_low"
	EventSubscriptionMonthlyLow         NotificationEvent = "subscription_monthly_remaining_low"
	EventSubscriptionExpiring           NotificationEvent = "subscription_expiring"
)

// NotificationLog 通知发送记录。
type NotificationLog struct {
	ID                uint              `gorm:"primaryKey" json:"id"`
	ChannelID         uint              `gorm:"not null;index" json:"channel_id"`
	UpstreamChannelID uint              `gorm:"not null;default:0;index" json:"upstream_channel_id,omitempty"`
	Event             NotificationEvent `gorm:"size:64;not null;index" json:"event"`
	Subject           string            `gorm:"size:512;not null" json:"subject"`
	Body              string            `gorm:"type:text" json:"body"`
	Success           bool              `gorm:"not null" json:"success"`
	ErrorMessage      string            `gorm:"type:text" json:"error_message,omitempty"`
	SentAt            time.Time         `gorm:"not null;index" json:"sent_at"`
}

func (NotificationLog) TableName() string { return "notification_logs" }

// NotificationCooldown 跨重启持久化的通知冷却记录。
//
// 业务键 (ChannelID, Event)：标记某渠道某类事件最近一次发送时间。
// Dispatcher 在发送 cooldown-aware 事件（如 balance_low）前查这张表，
// 命中且未过 cooldown 就跳过。
//
// 不和 NotificationLog 合并是因为：
//   - NotificationLog 是审计/历史日志（用户可见、可清理）
//   - NotificationCooldown 是去抖控制平面（仅最新一条、原子 upsert）
//
// ChannelID 这里指的是**上游渠道**（storage.Channel），不是通知渠道。
type NotificationCooldown struct {
	ChannelID  uint              `gorm:"primaryKey" json:"channel_id"`
	Event      NotificationEvent `gorm:"primaryKey;size:64" json:"event"`
	LastSentAt time.Time         `gorm:"not null" json:"last_sent_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

func (NotificationCooldown) TableName() string { return "notification_cooldowns" }

// MonitorJob 监控任务类型。
type MonitorJob string

const (
	MonitorJobLogin   MonitorJob = "login"
	MonitorJobBalance MonitorJob = "balance"
	MonitorJobRates   MonitorJob = "rates"
)

// MonitorLog 每次扫描 / 登录尝试的结果，便于诊断失败。
type MonitorLog struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ChannelID    uint       `gorm:"not null;index" json:"channel_id"`
	Job          MonitorJob `gorm:"size:32;not null;index" json:"job"`
	Success      bool       `gorm:"not null" json:"success"`
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
	StartedAt    time.Time  `gorm:"not null;index" json:"started_at"`
	FinishedAt   time.Time  `json:"finished_at"`
}

func (MonitorLog) TableName() string { return "monitor_logs" }

// ---------- 请求转发网关 ----------

const (
	GatewayKeyStatusActive   = "active"
	GatewayKeyStatusDisabled = "disabled"

	GatewayGroupStatusActive   = "active"
	GatewayGroupStatusDisabled = "disabled"

	GatewayModelsModeAuto   = "auto"
	GatewayModelsModeManual = "manual"
	GatewayModelsModeHybrid = "hybrid"

	// 上游协议（路由 / 直连渠道）
	//   auto | openai_chat | openai_responses | anthropic
	//   openai 为 openai_chat 的历史别名，读写时仍接受
	GatewayUpstreamProtocolAuto            = "auto"
	GatewayUpstreamProtocolOpenAI          = "openai"           // 兼容：等同 openai_chat
	GatewayUpstreamProtocolOpenAIChat      = "openai_chat"      // /v1/chat/completions · messages
	GatewayUpstreamProtocolOpenAIResponses = "openai_responses" // /v1/responses · input
	GatewayUpstreamProtocolAnthropic       = "anthropic"        // /v1/messages

	// GatewayRoute 上游来源：监控渠道 vs 直连提供商（base+key）
	GatewayRouteSourceMonitor  = "monitor"
	GatewayRouteSourceProvider = "provider"

	GatewayProviderAuthBearer  = "bearer"
	GatewayProviderAuthXAPIKey = "x-api-key"
	GatewayProviderAuthBoth    = "both"

	// 路由 User-Agent 策略（组级统一 UA + 路由三选一）：
	//   passthrough — 透传客户端 UA（默认；模型测试/拉模型无客户端时不设 UA）
	//   group       — 使用组 UserAgent（组为空则等同透传）
	//   custom      — 使用路由 UserAgentCustom（自定义为空则等同透传）
	GatewayUserAgentModePassthrough = "passthrough"
	GatewayUserAgentModeGroup       = "group"
	GatewayUserAgentModeCustom      = "custom"

	GatewayRequestTypeUnknown = 0
	GatewayRequestTypeSync    = 1
	GatewayRequestTypeStream  = 2
)

// GatewayProvider 直连上游（Base URL + API Key），不登录、不监控余额。
// 对齐 CLI Proxy openai-compatibility / api-key 形态，可与监控渠道在网关组内混用。
// User-Agent 不在直连渠道配置：由网关组 + 路由 UA 策略统一决定。
type GatewayProvider struct {
	ID                 uint    `gorm:"primaryKey" json:"id"`
	Name               string  `gorm:"size:128;not null;uniqueIndex" json:"name"`
	BaseURL            string  `gorm:"size:512;not null" json:"base_url"`
	APIKeyCipher       string  `gorm:"type:text;not null" json:"-"`
	APIKeyHint         string  `gorm:"size:64;not null;default:''" json:"api_key_hint"`
	UpstreamProtocol   string  `gorm:"size:16;not null;default:'auto'" json:"upstream_protocol"`
	DefaultBillingRate float64 `gorm:"not null;default:1" json:"default_billing_rate"`
	AuthStyle          string  `gorm:"size:16;not null;default:'both'" json:"auth_style"`
	Enabled            bool    `gorm:"not null;default:true;index" json:"enabled"`
	// ProxyEnabled 与监控渠道一致：全局代理开启且本开关打开时，转发走系统代理配置。
	ProxyEnabled     bool      `gorm:"not null;default:false" json:"proxy_enabled"`
	ExtraHeadersJSON string    `gorm:"type:text" json:"extra_headers,omitempty"`
	Notes            string    `gorm:"size:512;not null;default:''" json:"notes,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (GatewayProvider) TableName() string { return "gateway_providers" }

// GatewayGroup 是网关配置单元：路由、模型映射、模型列表归属组；组内可有多把密钥。
type GatewayGroup struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Description string `gorm:"size:512;not null;default:''" json:"description,omitempty"`
	// Position 侧栏展示与管理端列表顺序（越小越靠前）；新建组追加到末尾。
	Position          int    `gorm:"not null;default:0;index" json:"position"`
	Status            string `gorm:"size:16;not null;default:'active';index" json:"status"`
	RateSortDirection string `gorm:"size:16;not null;default:'asc'" json:"rate_sort_direction"`
	// RateResortEnabled 渠道分组价格倍率重排：开启后，倍率扫描结束时按源分组实时倍率
	// 重写路由 position 与 billing_rate_multiplier。
	// 关闭时仅在保存路由 / 改排序方向时落库顺序；运行时仍按实时倍率 SortRoutes。
	RateResortEnabled bool   `gorm:"not null;default:false" json:"rate_resort_enabled"`
	ModelMappingJSON  string `gorm:"type:text" json:"model_mapping,omitempty"`
	ModelsJSON        string `gorm:"type:text" json:"models_json,omitempty"`
	ModelsMode        string `gorm:"size:16;not null;default:'auto'" json:"models_mode"`
	// ServiceTierRulesJSON 保存 OpenAI service_tier 的组级处理规则。
	// 规则只在 OpenAI Chat / Responses 入站请求中生效。
	ServiceTierRulesJSON string `gorm:"type:text" json:"service_tier_rules_json,omitempty"`
	// SystemPromptRulesJSON 按数组顺序保存多条提示词注入规则。
	SystemPromptRulesJSON string `gorm:"type:text" json:"system_prompt_rules_json"`
	// 重试 / 顺延 / 冷却（组级策略）
	// RetryEnabled=false：上游失败直接回显，不重试、不顺延
	RetryEnabled bool `gorm:"not null;default:true" json:"retry_enabled"`
	// 同一路由额外重试次数（不含首次；0=每条路由只打一次）
	RetryCount int `gorm:"not null;default:0" json:"retry_count"`
	// 是否顺延到下一条路由
	FailoverEnabled bool `gorm:"not null;default:true" json:"failover_enabled"`
	// 顺延次数：在首条路由耗尽后，最多再换几条路由
	FailoverMax int `gorm:"not null;default:8" json:"failover_max"`
	// FailoverOn4xx：是否将上游 4xx（含 400/401/403/404 等；429 始终可顺延）纳入重试/顺延。
	// 默认 false：仅网络错误、429、5xx 会重试/顺延；其它 4xx 直接回显。
	FailoverOn4xx bool `gorm:"not null;default:false" json:"failover_on_4xx"`
	// 失败后临时冷却秒数（0=不冷却）；恢复后可再参与调度
	CooldownSeconds int `gorm:"not null;default:30" json:"cooldown_seconds"`
	// 首字/首字节超时（秒）：0=关闭；>0 时等待首字节超过该时间则主动断开并走重试/顺延。
	// 可能造成上游已计费但客户端未收完，从而重复请求增加费用。
	FirstTokenTimeoutSec int `gorm:"not null;default:0" json:"first_token_timeout_sec"`
	// UserAgent 组级统一 User-Agent。路由 mode=group 时使用；留空表示组未配置。
	// 不用 omitempty：空串也要返回，前端编辑回填才能区分「未配置」与「字段缺失」。
	UserAgent string    `gorm:"size:512;not null;default:''" json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (GatewayGroup) TableName() string { return "gateway_groups" }

// GatewayKey 是客户端调用本服务 /v1/* 时使用的请求密钥（归属某个组）。
type GatewayKey struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	GroupID uint   `gorm:"not null;index;default:0" json:"group_id"`
	Name    string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	// OwnerEmail 仅用于兼容已保存的旧版指定用户策略，新规则改用 KeyID。
	OwnerEmail      string     `gorm:"size:320;not null;default:'';index" json:"owner_email,omitempty"`
	KeyHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	KeyPrefix       string     `gorm:"size:32;not null" json:"key_prefix"`
	KeyCipher       string     `gorm:"type:text;not null" json:"-"`
	Status          string     `gorm:"size:16;not null;default:'active';index" json:"status"`
	Quota           float64    `gorm:"not null;default:0" json:"quota"`
	QuotaUsed       float64    `gorm:"not null;default:0" json:"quota_used"`
	IPWhitelistJSON string     `gorm:"type:text" json:"ip_whitelist,omitempty"`
	IPBlacklistJSON string     `gorm:"type:text" json:"ip_blacklist,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (GatewayKey) TableName() string { return "gateway_keys" }

// GatewayRoute 是网关组绑定的一条上游路由（监控渠道或直连 Provider）。
type GatewayRoute struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	GatewayGroupID uint `gorm:"not null;index;uniqueIndex:idx_gateway_route_group_pos" json:"gateway_group_id"`
	Position       int  `gorm:"not null;default:0;uniqueIndex:idx_gateway_route_group_pos" json:"position"`
	// SourceKind: monitor | provider；空视为 monitor（兼容旧数据）
	SourceKind            string  `gorm:"size:16;not null;default:'monitor';index" json:"source_kind"`
	SourceChannelID       uint    `gorm:"not null;index;default:0" json:"source_channel_id"`
	GatewayProviderID     uint    `gorm:"not null;index;default:0" json:"gateway_provider_id"`
	SourceGroupID         *int64  `json:"source_group_id,omitempty"`
	SourceGroupName       string  `gorm:"size:256;not null;default:''" json:"source_group_name,omitempty"`
	Weight                int     `gorm:"default:1" json:"weight"`
	RateConvertMode       string  `gorm:"size:32;not null;default:'raw'" json:"rate_convert_mode"`
	RateConvertValue      float64 `gorm:"default:1" json:"rate_convert_value"`
	BillingRateMultiplier float64 `gorm:"not null;default:1" json:"billing_rate_multiplier"`
	Enabled               bool    `gorm:"default:true" json:"enabled"`
	ModelMappingJSON      string  `gorm:"type:text" json:"model_mapping,omitempty"`
	UpstreamProtocol      string  `gorm:"size:16;not null;default:'auto'" json:"upstream_protocol"`
	Concurrency           int     `gorm:"default:100" json:"concurrency"`
	// UserAgentMode: passthrough | group | custom（见 GatewayUserAgentMode*）
	UserAgentMode string `gorm:"size:16;not null;default:'passthrough'" json:"user_agent_mode"`
	// UserAgentCustom 仅 mode=custom 时生效；转发/模型测试/拉模型共用。
	// 不用 omitempty：空串也要返回，前端编辑回填才能区分。
	UserAgentCustom         string     `gorm:"size:512;not null;default:''" json:"user_agent_custom"`
	SourceAPIKeyID          int64      `gorm:"not null;default:0" json:"source_api_key_id"`
	SourceAPIKeyName        string     `gorm:"size:256;not null;default:''" json:"source_api_key_name"`
	SourceAPIKeyCipher      string     `gorm:"type:text" json:"-"`
	TempUnschedulableUntil  *time.Time `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason string     `gorm:"type:text" json:"temp_unschedulable_reason,omitempty"`
	// TempUnschedulableAt / TempUnschedulableRequestID：最近一次触发暂停的失败请求时间与网关 request_id
	// （保留至手动清除、连续成功自动清除，或下次失败覆盖）
	TempUnschedulableAt        *time.Time `json:"temp_unschedulable_at,omitempty"`
	TempUnschedulableRequestID string     `gorm:"size:64;not null;default:''" json:"temp_unschedulable_request_id,omitempty"`
	// RecoverSuccessStreak：失败残留信息存在期间的连续成功次数；达到阈值后自动清空错误展示
	RecoverSuccessStreak int       `gorm:"not null;default:0" json:"recover_success_streak,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// RouteRecoverSuccessClearStreak 连续成功多少次后自动清除「已恢复/错误」残留展示。
const RouteRecoverSuccessClearStreak = 3

// NormalizeSourceKind 返回规范化来源类型。
func (r *GatewayRoute) NormalizeSourceKind() string {
	if r == nil {
		return GatewayRouteSourceMonitor
	}
	k := strings.ToLower(strings.TrimSpace(r.SourceKind))
	if k == GatewayRouteSourceProvider {
		return GatewayRouteSourceProvider
	}
	// 兼容：未写 kind 但填了 provider id
	if k == "" && r.GatewayProviderID > 0 && r.SourceChannelID == 0 {
		return GatewayRouteSourceProvider
	}
	return GatewayRouteSourceMonitor
}

func (GatewayRoute) TableName() string { return "gateway_routes" }

// GatewayUsageLog 记录每一次网关转发请求的用量与费用参考。
// Source* 字段为请求当时的路由快照：路由保存会换 id 时，历史记录仍可展示上游密钥/源分组。
type GatewayUsageLog struct {
	ID                uint `gorm:"primaryKey" json:"id"`
	GatewayGroupID    uint `gorm:"not null;index;default:0" json:"gateway_group_id"`
	GatewayKeyID      uint `gorm:"not null;index" json:"gateway_key_id"`
	RouteID           uint `gorm:"not null;index" json:"route_id"`
	ChannelID         uint `gorm:"not null;index;default:0" json:"channel_id"`
	GatewayProviderID uint `gorm:"not null;index;default:0" json:"gateway_provider_id"`
	// 路由快照（写入时固化，不依赖 route 表存活）
	ProviderName     string `gorm:"size:128;not null;default:''" json:"provider_name,omitempty"`
	SourceAPIKeyID   int64  `gorm:"not null;default:0" json:"source_api_key_id,omitempty"`
	SourceAPIKeyName string `gorm:"size:256;not null;default:''" json:"source_api_key_name,omitempty"`
	SourceGroupID    *int64 `json:"source_group_id,omitempty"`
	SourceGroupName  string `gorm:"size:256;not null;default:''" json:"source_group_name,omitempty"`
	RequestID        string `gorm:"size:64;not null;index" json:"request_id"`
	// 同一 RequestID 下的尝试序号（从 1 起）；用于使用记录关联
	Attempt int `gorm:"not null;default:1;index" json:"attempt"`
	// primary | retry | failover
	AttemptKind string `gorm:"size:16;not null;default:'primary'" json:"attempt_kind,omitempty"`
	// 本条失败后写入的冷却截止（若有），便于日志展示
	CooldownUntil     *time.Time `json:"cooldown_until,omitempty"`
	RequestedModel    string     `gorm:"size:256;not null;index" json:"requested_model"`
	UpstreamModel     string     `gorm:"size:256" json:"upstream_model,omitempty"`
	ModelMappingChain string     `gorm:"size:512" json:"model_mapping_chain,omitempty"`
	InboundEndpoint   string     `gorm:"size:128" json:"inbound_endpoint,omitempty"`
	UpstreamEndpoint  string     `gorm:"size:128" json:"upstream_endpoint,omitempty"`
	InboundProtocol   string     `gorm:"size:16" json:"inbound_protocol,omitempty"`
	UpstreamProtocol  string     `gorm:"size:16" json:"upstream_protocol,omitempty"`
	ProtocolConverted bool       `gorm:"not null;default:false" json:"protocol_converted"`
	RequestType       int        `gorm:"not null;default:0;index" json:"request_type"`
	ServiceTier       string     `gorm:"size:64" json:"service_tier,omitempty"`
	ReasoningEffort   string     `gorm:"size:32" json:"reasoning_effort,omitempty"`
	BillingMode       string     `gorm:"size:32;not null;default:'token'" json:"billing_mode"`
	// Token 互斥桶（对齐 sub2api）：
	// InputTokens = 不含缓存的「新鲜输入」；CacheRead/Creation 单独计
	InputTokens           int `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens          int `gorm:"not null;default:0" json:"output_tokens"`
	CacheCreationTokens   int `gorm:"not null;default:0" json:"cache_creation_tokens"`
	CacheReadTokens       int `gorm:"not null;default:0" json:"cache_read_tokens"`
	CacheCreation5mTokens int `gorm:"not null;default:0" json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int `gorm:"not null;default:0" json:"cache_creation_1h_tokens"`
	ImageOutputTokens     int `gorm:"not null;default:0" json:"image_output_tokens"`
	// 推理 token（completion_tokens_details.reasoning_tokens），展示用，费用含在 output 单价内
	ReasoningTokens       int     `gorm:"not null;default:0" json:"reasoning_tokens"`
	InputCost             float64 `gorm:"not null;default:0" json:"input_cost"`
	OutputCost            float64 `gorm:"not null;default:0" json:"output_cost"`
	CacheCreationCost     float64 `gorm:"not null;default:0" json:"cache_creation_cost"`
	CacheReadCost         float64 `gorm:"not null;default:0" json:"cache_read_cost"`
	ImageOutputCost       float64 `gorm:"not null;default:0" json:"image_output_cost"`
	TotalCost             float64 `gorm:"not null;default:0" json:"total_cost"`
	ActualCost            float64 `gorm:"not null;default:0" json:"actual_cost"`
	AccountStatsCost      float64 `gorm:"not null;default:0" json:"account_stats_cost"`
	RateMultiplier        float64 `gorm:"not null;default:1" json:"rate_multiplier"`
	BillingRateMultiplier float64 `gorm:"not null;default:1" json:"billing_rate_multiplier"`
	AccountRateMultiplier float64 `gorm:"not null;default:1" json:"account_rate_multiplier"`
	Stream                bool    `gorm:"not null;default:false" json:"stream"`
	StatusCode            int     `gorm:"not null;default:0" json:"status_code"`
	Success               bool    `gorm:"not null;default:false;index" json:"success"`
	// ErrorMessage 短摘要（列表/告警）；ErrorDetail / UpstreamErrorBody 供 debug 追踪
	ErrorMessage         string    `gorm:"type:text" json:"error_message,omitempty"`
	ErrorType            string    `gorm:"size:32;not null;default:'';index" json:"error_type,omitempty"` // transport|http|config|internal
	ErrorDetail          string    `gorm:"type:text" json:"error_detail,omitempty"`
	UpstreamURL          string    `gorm:"size:512" json:"upstream_url,omitempty"`
	UpstreamErrorBody    string    `gorm:"type:text" json:"upstream_error_body,omitempty"`
	UpstreamErrorHeaders string    `gorm:"type:text" json:"upstream_error_headers,omitempty"`
	DurationMS           int64     `gorm:"not null;default:0" json:"duration_ms"`
	FirstTokenMS         *int64    `json:"first_token_ms,omitempty"`
	IPAddress            string    `gorm:"size:64" json:"ip_address,omitempty"`
	UserAgent            string    `gorm:"size:512" json:"user_agent,omitempty"`
	CreatedAt            time.Time `gorm:"not null;index" json:"created_at"`
}

func (GatewayUsageLog) TableName() string { return "gateway_usage_logs" }

// ModelPriceOverride 覆盖内置模型价格。token 单价单位为 USD/token；
// 图片档位为 USD/张；视频档位为 USD/秒。
type ModelPriceOverride struct {
	ID                         uint      `gorm:"primaryKey" json:"id"`
	ModelName                  string    `gorm:"size:256;not null;uniqueIndex" json:"model_name"`
	BillingMode                string    `gorm:"size:32;not null;default:'token'" json:"billing_mode"`
	InputPricePerToken         float64   `gorm:"not null;default:0" json:"input_price_per_token"`
	OutputPricePerToken        float64   `gorm:"not null;default:0" json:"output_price_per_token"`
	CacheCreationPricePerToken float64   `gorm:"not null;default:0" json:"cache_creation_price_per_token"`
	CacheReadPricePerToken     float64   `gorm:"not null;default:0" json:"cache_read_price_per_token"`
	PerRequestPrice            float64   `gorm:"not null;default:0" json:"per_request_price"`
	PricingTiersJSON           string    `gorm:"type:text;not null;default:''" json:"pricing_tiers_json"`
	ImagePrice1K               float64   `gorm:"not null;default:0" json:"image_price_1k"`
	ImagePrice2K               float64   `gorm:"not null;default:0" json:"image_price_2k"`
	ImagePrice4K               float64   `gorm:"not null;default:0" json:"image_price_4k"`
	VideoPrice480P             float64   `gorm:"not null;default:0" json:"video_price_480p"`
	VideoPrice720P             float64   `gorm:"not null;default:0" json:"video_price_720p"`
	VideoPrice1080P            float64   `gorm:"not null;default:0" json:"video_price_1080p"`
	Enabled                    bool      `gorm:"default:true" json:"enabled"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

func (ModelPriceOverride) TableName() string { return "model_price_overrides" }
