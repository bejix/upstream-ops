// Package gateway 实现 OpenAI / Anthropic / Responses 兼容的请求转发网关。
//
// 分层：
//   - Service：组装根，持有仓储与配置，对外 API 通过薄委托保持兼容；
//   - AdminService：管理面（分组 / 密钥 / 路由 / 直连渠道 / 模型同步）；
//   - Runtime：数据面（鉴权 / 转发 / 流式 / 公开端点 / 用量落库）。
//
// 协议互转见子包 protocol；运行时默认值见 config.DefaultGateway*，可由设置页 gateway 段覆盖。
package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
)

// 使用记录 attempt_kind
const (
	attemptKindPrimary  = "primary"
	attemptKindRetry    = "retry"
	attemptKindFailover = "failover"
)

// ChannelAPI 上游密钥管理能力（由 channel.Service 实现）。
type ChannelAPI interface {
	ListAPIKeys(ctx context.Context, channelID uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error)
	ListAPIKeyGroups(ctx context.Context, channelID uint) ([]connector.APIKeyGroup, error)
	CreateAPIKey(ctx context.Context, channelID uint, req connector.APIKeyCreateRequest) (*connector.APIKey, error)
	RevealAPIKey(ctx context.Context, channelID uint, keyID int64) (string, error)
}

// Service 网关领域服务组装根。
// 管理面操作见 Admin；数据面转发见 Runtime；公开方法多为委托以保持调用方兼容。
type Service struct {
	Groups     *storage.GatewayGroups
	Keys       *storage.GatewayKeys
	Routes     *storage.GatewayRoutes
	Providers  *storage.GatewayProviders
	Usage      *storage.GatewayUsageLogs
	Prices     *storage.ModelPriceOverrides
	Channels   *storage.Channels
	ChannelAPI ChannelAPI
	Cipher     *crypto.Cipher
	Pricing    *PricingCatalog
	Log        *slog.Logger

	// Admin 管理面；嵌入 *Service 共享依赖。
	Admin *AdminService
	// Runtime 数据面；嵌入 *Service 共享依赖。
	Runtime *Runtime

	mu          sync.RWMutex
	proxyConfig config.ProxyConfig
	upstream    config.UpstreamConfig
	gatewayCfg  config.GatewayConfig

	modelsCacheMu     sync.Mutex
	modelsCache       map[uint]modelsCacheEntry // keyed by group id
	routeKeyBindingMu sync.Mutex

	// 源分组列表缓存（ListAPIKeyGroups 远程调用昂贵；列表接口不再实时拉，运行时/保存仍可复用缓存）
	channelGroupsCacheMu sync.Mutex
	channelGroupsCache   map[uint]channelGroupsCacheEntry // keyed by channel id
}

type modelsCacheEntry struct {
	at   time.Time
	body []byte
}

type channelGroupsCacheEntry struct {
	at     time.Time
	groups []connector.APIKeyGroup
}

// NewService 构造网关服务并初始化 Admin / Runtime。
func NewService(
	groups *storage.GatewayGroups,
	keys *storage.GatewayKeys,
	routes *storage.GatewayRoutes,
	usage *storage.GatewayUsageLogs,
	prices *storage.ModelPriceOverrides,
	channels *storage.Channels,
	channelAPI ChannelAPI,
	cipher *crypto.Cipher,
	log *slog.Logger,
) *Service {
	s := &Service{
		Groups:             groups,
		Keys:               keys,
		Routes:             routes,
		Usage:              usage,
		Prices:             prices,
		Channels:           channels,
		ChannelAPI:         channelAPI,
		Cipher:             cipher,
		Pricing:            NewPricingCatalog(prices),
		Log:                log,
		gatewayCfg:         config.GatewayConfig{}.WithDefaults(),
		modelsCache:        map[uint]modelsCacheEntry{},
		channelGroupsCache: map[uint]channelGroupsCacheEntry{},
	}
	s.Admin = &AdminService{Service: s}
	s.Runtime = &Runtime{Service: s}
	return s
}

// SetProviders 注入直连渠道仓储（main 组装时调用，保持 NewService 签名兼容）。
func (s *Service) SetProviders(p *storage.GatewayProviders) {
	s.Providers = p
}

// ListDefaultPrices 返回内置模型价目（管理端只读）。
func (s *Service) ListDefaultPrices(query string) []DefaultPriceItem {
	if s.Pricing == nil {
		return nil
	}
	return s.Pricing.ListDefaults(query)
}

func (s *Service) UpdateProxyConfig(cfg config.ProxyConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxyConfig = cfg
}

func (s *Service) UpdateUpstreamConfig(cfg config.UpstreamConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upstream = cfg.WithDefaults()
}

func (s *Service) UpdateGatewayConfig(cfg config.GatewayConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gatewayCfg = cfg.WithDefaults()
}

// StartPricing 启动模型价格远程同步。内置价目始终可用，远程失败不会中断网关。
func (s *Service) StartPricing(cfg config.PricingConfig) {
	if s != nil && s.Pricing != nil {
		s.Pricing.Start(cfg, s.Log)
	}
}

// UpdatePricingConfig 应用新配置并重启价格同步任务。
func (s *Service) UpdatePricingConfig(cfg config.PricingConfig) {
	s.StartPricing(cfg)
}

// Stop 停止网关后台任务。
func (s *Service) Stop() {
	if s != nil && s.Pricing != nil {
		s.Pricing.Stop()
	}
}
