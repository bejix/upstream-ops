// Package api 注册所有 HTTP 路由，组装各业务 handler。
//
// 单用户场景下走 HMAC token 鉴权：账号密码写在 config 里，登录后下发 token。
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bejix/upstream-ops/backend/channel"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/gateway"
	"github.com/bejix/upstream-ops/backend/notify"
	"github.com/bejix/upstream-ops/backend/runtimeconfig"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type monitorService interface {
	RefreshBalance(ctx context.Context, c *storage.Channel) error
	RefreshRates(ctx context.Context, c *storage.Channel) error
	CheckSubscriptionUsageAlerts(ctx context.Context, c *storage.Channel) error
}

type channelService interface {
	Create(in channel.CreateInput) (*storage.Channel, error)
	Update(id uint, in channel.UpdateInput) (*storage.Channel, error)
	Delete(id uint) error
	ClearLoginInfo(id uint) (*storage.Channel, error)
	TestLogin(ctx context.Context, channelID uint) error
	RedeemCode(ctx context.Context, channelID uint, code string) (*connector.RedeemResult, error)
	GetRechargeInfo(ctx context.Context, channelID uint) (*connector.RechargeInfo, error)
	CreateRecharge(ctx context.Context, channelID uint, req connector.RechargeRequest) (*connector.RechargeLaunch, error)
	GetSubscriptionInfo(ctx context.Context, channelID uint) (*connector.SubscriptionInfo, error)
	CreateSubscription(ctx context.Context, channelID uint, req connector.SubscriptionRequest) (*connector.SubscriptionLaunch, error)
	GetSubscriptionUsage(ctx context.Context, channelID uint) (*connector.SubscriptionUsageInfo, error)
	ListAPIKeys(ctx context.Context, channelID uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error)
	ListAPIKeyGroups(ctx context.Context, channelID uint) ([]connector.APIKeyGroup, error)
	// GetUpstreamCatalog 拉取「分组 → 模型」上游目录（带 5 分钟缓存，fresh=true 强制刷新）。
	GetUpstreamCatalog(ctx context.Context, channelID uint, fresh bool) (*connector.Catalog, error)
	// ListAPIKeyGroupSet 拉取该渠道"已创建密钥的分组集合"，用于"仅显示已创建密钥的分组"开关。
	ListAPIKeyGroupSet(ctx context.Context, channelID uint) (*channel.APIKeyGroupSet, error)
	CreateAPIKey(ctx context.Context, channelID uint, req connector.APIKeyCreateRequest) (*connector.APIKey, error)
	UpdateAPIKey(ctx context.Context, channelID uint, keyID int64, req connector.APIKeyUpdateRequest) (*connector.APIKey, error)
	DeleteAPIKey(ctx context.Context, channelID uint, keyID int64) error
	RevealAPIKey(ctx context.Context, channelID uint, keyID int64) (string, error)
}

// Deps 把所有 handler 需要的依赖打包传入。
type Deps struct {
	DB            *gorm.DB
	Cipher        *crypto.Cipher
	Runtime       *runtimeconfig.Manager
	Channels      *storage.Channels
	Sessions      *storage.AuthSessions
	Captchas      *storage.Captchas
	Notifies      *storage.Notifications
	Announcements *storage.UpstreamAnnouncements
	Rates         *storage.Rates
	MonLogs       *storage.MonitorLogs
	ChannelSvc    channelService
	Monitor       monitorService
	Dispatcher    *notify.Dispatcher
	Gateway       *gateway.Service
	GatewayGroups *storage.GatewayGroups
	GatewayKeys   *storage.GatewayKeys
	GatewayUsage  *storage.GatewayUsageLogs
	ModelPrices   *storage.ModelPriceOverrides
	Log           *slog.Logger

	// Frontend 可选：传入嵌入的前端 dist 文件系统。nil 表示不挂载（本地开发用 vite dev server）。
	Frontend fs.FS
}

// Register 把所有路由挂到给定 gin engine。
func Register(r *gin.Engine, d *Deps) {
	r.GET("/healthz", func(c *gin.Context) {
		sqlDB, err := d.DB.DB()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "down", "err": err.Error()})
			return
		}
		if err := sqlDB.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_down", "err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 公开网关：/v1/*（不走管理端鉴权）。管理页 SPA 使用 /gateway，勿占用。
	if d.Gateway != nil {
		if d.Frontend != nil {
			gateway.RegisterPublicWithoutRoot(r, d.Gateway)
		} else {
			gateway.RegisterPublic(r, d.Gateway)
		}
	}

	api := r.Group("/api")
	if d.Runtime != nil {
		api.Use(d.Runtime.AuthMiddleware())
	}
	{
		registerVersion(api, d)
		registerAuth(api, d)
		registerChannels(api, d)
		registerCaptchas(api, d)
		registerNotifications(api, d)
		registerAnnouncements(api, d)
		registerRates(api, d)
		registerMonitorLogs(api, d)
		registerDashboard(api, d)
		registerSettings(api, d)
		registerGatewayAdmin(api, d)
	}

	if d.Frontend != nil {
		registerFrontend(r, d.Frontend)
	}
}

// registerFrontend 把嵌入的前端 dist 挂在根路径，并处理 SPA fallback：
//
//   - GET /assets/*  → 直接返回文件
//   - GET /          → 返回 index.html
//   - GET /channels  → 返回 index.html（React Router 客户端路由）
//
// /api/*、/healthz 都已被前面的具体路由占了，不会走到这里。
// 安全起见仍然做一次前缀拦截，避免任何意外情况下"未鉴权读 index.html"压到 /api 上。
func registerFrontend(r *gin.Engine, dist fs.FS) {
	fileServer := http.FileServer(http.FS(dist))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 永远不让 SPA fallback 覆盖 API / 健康检查 / 公开转发路径。
		// /gateway 是前端管理页，必须允许 SPA fallback。
		if strings.HasPrefix(path, "/api/") || path == "/healthz" ||
			strings.HasPrefix(path, "/v1/") || path == "/v1" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		// 文件存在就直接 serve，否则回落到 index.html。
		clean := strings.TrimPrefix(path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(dist, clean); err != nil {
			c.Request.URL.Path = "/"
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

// fail 统一错误响应。
func fail(c *gin.Context, status int, err error) {
	c.JSON(status, gin.H{"error": err.Error()})
}
