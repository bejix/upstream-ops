// Package connector 定义上游渠道连接器接口与公共类型，由 newapi / sub2api 等子包注册具体实现。
//
// 使用方法：
//
//	import _ "github.com/bejix/upstream-ops/backend/connector/newapi"
//	import _ "github.com/bejix/upstream-ops/backend/connector/sub2api"
//
//	c, err := connector.For("newapi")
//	session, err := c.Login(ctx, channel)
package connector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// SessionRefreshError records whether a refresh failure is safe to recover from
// by creating a new password login session.
type SessionRefreshError struct {
	Err                error
	AllowLoginFallback bool
}

func (e *SessionRefreshError) Error() string { return e.Err.Error() }
func (e *SessionRefreshError) Unwrap() error { return e.Err }

func WrapSessionRefreshError(err error, allowLoginFallback bool) error {
	if err == nil {
		return nil
	}
	return &SessionRefreshError{Err: err, AllowLoginFallback: allowLoginFallback}
}

// ShouldLoginAfterRefreshError preserves legacy fallback for connectors that
// do not classify their refresh failures, while allowing strict connectors to
// protect upstream session quotas.
func ShouldLoginAfterRefreshError(err error) bool {
	var refreshErr *SessionRefreshError
	if !errors.As(err, &refreshErr) {
		return true
	}
	return refreshErr.AllowLoginFallback
}

// ChannelType 渠道类型枚举，与 storage.ChannelType 同步。
type ChannelType string

const (
	TypeNewAPI  ChannelType = "newapi"
	TypeSub2API ChannelType = "sub2api"
)

// Channel 已解密的渠道连接信息，由 channel 层负责构造。
type Channel struct {
	ID                     uint
	Name                   string
	Type                   ChannelType
	SiteURL                string
	Username               string
	Password               string
	LoginExtraParams       map[string]any
	TurnstileEnabled       bool
	ProxyURL               string
	RechargeMultiplier     *float64
	RechargeMultiplierMode string
	// TurnstileToken 由调用方在 Login 前预先求解打码后填入；为空则直接发起登录。
	TurnstileToken string
}

const (
	RechargeMultiplierModeDivide   = "divide"
	RechargeMultiplierModeMultiply = "multiply"
)

func NormalizeRechargeMultiplierMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case RechargeMultiplierModeMultiply:
		return RechargeMultiplierModeMultiply
	default:
		return RechargeMultiplierModeDivide
	}
}

func ApplyRechargeMultiplier(value float64, multiplier *float64, mode string) float64 {
	if multiplier == nil || *multiplier <= 0 {
		return value
	}
	if NormalizeRechargeMultiplierMode(mode) == RechargeMultiplierModeMultiply {
		return round4(value * *multiplier)
	}
	return round4(value / *multiplier)
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

// AuthSession 登录后产生的会话凭据。明文，由 channel 层负责加密落库。
type AuthSession struct {
	// UserID 上游账号 ID 字符串。NewAPI 必须在后续请求头里附带 `New-Api-User: <id>`。
	// 不是机密信息，channel 层按明文存。
	UserID       string
	AccessToken  string
	RefreshToken string
	Cookie       string
	CSRFToken    string
	ExpiresAt    time.Time
}

// BalanceResult 一次余额采集结果。Balance 已经换算成显示单位（一般是 USD 等值）。
type BalanceResult struct {
	Balance   float64
	SampledAt time.Time
}

// CostResult 一次消费采集结果。单位统一为展示货币（通常是 USD）。
type CostResult struct {
	TodayCost float64
	TotalCost float64
}

// RateResult 一条倍率记录。ModelName 在两个上游分别是"分组名"，Description 是该分组的描述（来自上游接口）。
type RateResult struct {
	GroupID         *int64
	ModelName       string
	Description     string
	Ratio           float64
	CompletionRatio float64
}

// AnnouncementResult 一条从上游同步到的公告。
type AnnouncementResult struct {
	SourceKey       string
	Title           string
	Content         string
	Type            string
	Link            string
	PublishedAt     *time.Time
	SourceUpdatedAt *time.Time
}

// RedeemResult 一次兑换的统一结果。
type RedeemResult struct {
	Message        string   `json:"message"`
	Type           string   `json:"type"`
	Value          float64  `json:"value"`
	NewBalance     *float64 `json:"new_balance,omitempty"`
	NewConcurrency *int     `json:"new_concurrency,omitempty"`
	GroupName      *string  `json:"group_name,omitempty"`
	ValidityDays   *int     `json:"validity_days,omitempty"`
}

type RechargeMethod struct {
	Type      string  `json:"type"`
	Name      string  `json:"name"`
	MinAmount float64 `json:"min_amount"`
	MaxAmount float64 `json:"max_amount"`
}

type RechargeInfo struct {
	AmountLabel       string           `json:"amount_label"`
	AmountStep        float64          `json:"amount_step"`
	MinAmount         float64          `json:"min_amount"`
	MaxAmount         float64          `json:"max_amount"`
	PresetAmounts     []float64        `json:"preset_amounts"`
	HelpText          string           `json:"help_text,omitempty"`
	HelpImageURL      string           `json:"help_image_url,omitempty"`
	AlipayForceQRCode bool             `json:"alipay_force_qrcode"`
	Methods           []RechargeMethod `json:"methods"`
}

type RechargeRequest struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
	IsMobile      bool    `json:"is_mobile"`
}

type RechargeLaunch struct {
	Mode       string            `json:"mode"`
	QRCode     string            `json:"qr_code,omitempty"`
	PayURL     string            `json:"pay_url,omitempty"`
	FormAction string            `json:"form_action,omitempty"`
	FormFields map[string]string `json:"form_fields,omitempty"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
}

type SubscriptionMethod struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type SubscriptionPlan struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Price           float64  `json:"price"`
	Currency        string   `json:"currency,omitempty"`
	Validity        string   `json:"validity,omitempty"`
	GroupName       string   `json:"group_name,omitempty"`
	Quota           float64  `json:"quota,omitempty"`
	DailyLimitUSD   *float64 `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD  *float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyLimitUSD *float64 `json:"monthly_limit_usd,omitempty"`
	Features        []string `json:"features,omitempty"`
	PaymentMethods  []string `json:"payment_methods,omitempty"`
}

type SubscriptionInfo struct {
	Plans   []SubscriptionPlan   `json:"plans"`
	Methods []SubscriptionMethod `json:"methods"`
}

type SubscriptionRequest struct {
	PlanID        string `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
	IsMobile      bool   `json:"is_mobile"`
}

type SubscriptionLaunch = RechargeLaunch

type SubscriptionUsageWindow struct {
	LimitUSD         float64    `json:"limit_usd"`
	UsedUSD          float64    `json:"used_usd"`
	RemainingUSD     float64    `json:"remaining_usd"`
	RemainingPercent float64    `json:"remaining_percent"`
	UsedPercent      float64    `json:"used_percent"`
	WindowStart      *time.Time `json:"window_start,omitempty"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
	ResetsInSeconds  int64      `json:"resets_in_seconds"`
}

type SubscriptionUsage struct {
	ID            int64                    `json:"id"`
	GroupID       int64                    `json:"group_id"`
	GroupName     string                   `json:"group_name"`
	Status        string                   `json:"status"`
	StartsAt      *time.Time               `json:"starts_at,omitempty"`
	ExpiresAt     *time.Time               `json:"expires_at,omitempty"`
	ExpiresInDays int                      `json:"expires_in_days"`
	Daily         *SubscriptionUsageWindow `json:"daily,omitempty"`
	Weekly        *SubscriptionUsageWindow `json:"weekly,omitempty"`
	Monthly       *SubscriptionUsageWindow `json:"monthly,omitempty"`
}

type SubscriptionUsageInfo struct {
	Items []SubscriptionUsage `json:"items"`
}

type APIKeyQuery struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Search   string `json:"search"`
	Status   string `json:"status"`
	GroupID  string `json:"group_id"`
}

type APIKeyPage struct {
	Items    []APIKey `json:"items"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Pages    int      `json:"pages"`
}

type APIKeyGroup struct {
	ID          *int64  `json:"id,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Ratio       float64 `json:"ratio"`
}

type APIKey struct {
	ID                 int64      `json:"id"`
	Key                string     `json:"key"`
	Name               string     `json:"name"`
	Status             string     `json:"status"`
	Group              string     `json:"group,omitempty"`
	GroupName          string     `json:"group_name,omitempty"`
	GroupDescription   string     `json:"group_description,omitempty"`
	GroupRatio         float64    `json:"group_ratio"`
	GroupID            *int64     `json:"group_id,omitempty"`
	Quota              float64    `json:"quota"`
	QuotaUsed          float64    `json:"quota_used"`
	UnlimitedQuota     bool       `json:"unlimited_quota"`
	ExpiredTime        int64      `json:"expired_time"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CreatedAt          *time.Time `json:"created_at,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	AllowIPs           string     `json:"allow_ips,omitempty"`
	IPWhitelist        []string   `json:"ip_whitelist,omitempty"`
	IPBlacklist        []string   `json:"ip_blacklist,omitempty"`
	ModelLimitsEnabled bool       `json:"model_limits_enabled"`
	ModelLimits        string     `json:"model_limits,omitempty"`
	CrossGroupRetry    bool       `json:"cross_group_retry"`
	RateLimit5h        float64    `json:"rate_limit_5h"`
	RateLimit1d        float64    `json:"rate_limit_1d"`
	RateLimit7d        float64    `json:"rate_limit_7d"`
	Usage5h            float64    `json:"usage_5h"`
	Usage1d            float64    `json:"usage_1d"`
	Usage7d            float64    `json:"usage_7d"`
}

type APIKeyCreateRequest struct {
	Name               string   `json:"name"`
	CustomKey          string   `json:"custom_key,omitempty"`
	Group              string   `json:"group,omitempty"`
	GroupID            *int64   `json:"group_id,omitempty"`
	RemainQuota        *int     `json:"remain_quota,omitempty"`
	Quota              *float64 `json:"quota,omitempty"`
	UnlimitedQuota     *bool    `json:"unlimited_quota,omitempty"`
	ExpiredTime        *int64   `json:"expired_time,omitempty"`
	ExpiresInDays      *int     `json:"expires_in_days,omitempty"`
	ModelLimitsEnabled *bool    `json:"model_limits_enabled,omitempty"`
	ModelLimits        string   `json:"model_limits,omitempty"`
	AllowIPs           string   `json:"allow_ips,omitempty"`
	IPWhitelist        []string `json:"ip_whitelist,omitempty"`
	IPBlacklist        []string `json:"ip_blacklist,omitempty"`
	CrossGroupRetry    *bool    `json:"cross_group_retry,omitempty"`
	RateLimit5h        *float64 `json:"rate_limit_5h,omitempty"`
	RateLimit1d        *float64 `json:"rate_limit_1d,omitempty"`
	RateLimit7d        *float64 `json:"rate_limit_7d,omitempty"`
}

type APIKeyUpdateRequest struct {
	Name                *string  `json:"name,omitempty"`
	Group               *string  `json:"group,omitempty"`
	GroupID             *int64   `json:"group_id,omitempty"`
	Status              *string  `json:"status,omitempty"`
	RemainQuota         *int     `json:"remain_quota,omitempty"`
	Quota               *float64 `json:"quota,omitempty"`
	UnlimitedQuota      *bool    `json:"unlimited_quota,omitempty"`
	ExpiredTime         *int64   `json:"expired_time,omitempty"`
	ExpiresAt           *string  `json:"expires_at,omitempty"`
	ModelLimitsEnabled  *bool    `json:"model_limits_enabled,omitempty"`
	ModelLimits         *string  `json:"model_limits,omitempty"`
	AllowIPs            *string  `json:"allow_ips,omitempty"`
	IPWhitelist         []string `json:"ip_whitelist,omitempty"`
	IPBlacklist         []string `json:"ip_blacklist,omitempty"`
	CrossGroupRetry     *bool    `json:"cross_group_retry,omitempty"`
	RateLimit5h         *float64 `json:"rate_limit_5h,omitempty"`
	RateLimit1d         *float64 `json:"rate_limit_1d,omitempty"`
	RateLimit7d         *float64 `json:"rate_limit_7d,omitempty"`
	ResetQuota          *bool    `json:"reset_quota,omitempty"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage,omitempty"`
}

// Connector 上游连接器统一接口。
//
//   - GetTurnstileSiteKey  从上游公开接口读取 Turnstile site key（无需鉴权）
//   - Login                登录获取 session
//   - CheckAuth            使用现有 session 做一次轻量校验，确认未过期
//   - GetBalance           拉取当前余额
//   - GetRates             拉取当前所有可见的倍率
//   - RedeemCode           在线兑换兑换码
type Connector interface {
	// GetTurnstileSiteKey 返回上游当前的 Turnstile site key。
	// 站点没有开启 Turnstile 时返回 ""（不视作错误）。
	GetTurnstileSiteKey(ctx context.Context, channel *Channel) (string, error)

	Login(ctx context.Context, channel *Channel) (*AuthSession, error)
	CheckAuth(ctx context.Context, channel *Channel, session *AuthSession) error
	GetBalance(ctx context.Context, channel *Channel, session *AuthSession) (*BalanceResult, error)
	GetCosts(ctx context.Context, channel *Channel, session *AuthSession) (*CostResult, error)
	GetRates(ctx context.Context, channel *Channel, session *AuthSession) ([]RateResult, error)
	GetAnnouncements(ctx context.Context, channel *Channel, session *AuthSession) ([]AnnouncementResult, error)
	RedeemCode(ctx context.Context, channel *Channel, session *AuthSession, code string) (*RedeemResult, error)
	GetRechargeInfo(ctx context.Context, channel *Channel, session *AuthSession) (*RechargeInfo, error)
	CreateRecharge(ctx context.Context, channel *Channel, session *AuthSession, req RechargeRequest) (*RechargeLaunch, error)
	GetSubscriptionInfo(ctx context.Context, channel *Channel, session *AuthSession) (*SubscriptionInfo, error)
	CreateSubscription(ctx context.Context, channel *Channel, session *AuthSession, req SubscriptionRequest) (*SubscriptionLaunch, error)
	GetSubscriptionUsage(ctx context.Context, channel *Channel, session *AuthSession) (*SubscriptionUsageInfo, error)
	ListAPIKeys(ctx context.Context, channel *Channel, session *AuthSession, query APIKeyQuery) (*APIKeyPage, error)
	ListAPIKeyGroups(ctx context.Context, channel *Channel, session *AuthSession) ([]APIKeyGroup, error)
	CreateAPIKey(ctx context.Context, channel *Channel, session *AuthSession, req APIKeyCreateRequest) (*APIKey, error)
	UpdateAPIKey(ctx context.Context, channel *Channel, session *AuthSession, id int64, req APIKeyUpdateRequest) (*APIKey, error)
	DeleteAPIKey(ctx context.Context, channel *Channel, session *AuthSession, id int64) error
	RevealAPIKey(ctx context.Context, channel *Channel, session *AuthSession, id int64) (string, error)
}

// SessionRefresher 是支持 refresh_token 续期的 connector 可选能力。
type SessionRefresher interface {
	RefreshSession(ctx context.Context, channel *Channel, session *AuthSession) (*AuthSession, error)
}

type ProxySetter interface {
	SetProxy(proxyURL string)
}

type HTTPConfig struct {
	Timeout   time.Duration
	UserAgent string
}

type HTTPConfigSetter interface {
	SetHTTPConfig(cfg HTTPConfig)
}

// Factory 构造一个全新的 Connector 实例。
type Factory func() Connector

var (
	mu       sync.RWMutex
	registry = map[ChannelType]Factory{}
)

// Register 由子包在 init() 中调用，注册其类型对应的 Connector 构造器。
func Register(t ChannelType, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[t] = f
}

// For 按 ChannelType 取一个新的 Connector。未注册返回错误。
func For(t ChannelType) (Connector, error) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := registry[t]
	if !ok {
		return nil, fmt.Errorf("connector %q is not registered (did you forget the blank import?)", t)
	}
	return f(), nil
}
