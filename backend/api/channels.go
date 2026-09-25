package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bejix/upstream-ops/backend/channel"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/progress"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

func registerChannels(g *gin.RouterGroup, d *Deps) {
	gp := g.Group("/channels")
	gp.GET("", func(c *gin.Context) { listChannels(c, d) })
	gp.POST("", func(c *gin.Context) { createChannel(c, d) })
	gp.POST("/sync-all", func(c *gin.Context) { syncAllChannels(c, d) })
	gp.GET("/:id", func(c *gin.Context) { getChannel(c, d) })
	gp.PUT("/:id", func(c *gin.Context) { updateChannel(c, d) })
	gp.DELETE("/:id", func(c *gin.Context) { deleteChannel(c, d) })
	gp.POST("/:id/clear-login-info", func(c *gin.Context) { clearChannelLoginInfo(c, d) })
	gp.POST("/:id/enable", func(c *gin.Context) { toggleChannel(c, d, true) })
	gp.POST("/:id/disable", func(c *gin.Context) { toggleChannel(c, d, false) })
	gp.POST("/:id/test-login", func(c *gin.Context) { testLogin(c, d) })
	gp.POST("/:id/refresh-balance", func(c *gin.Context) { refreshBalance(c, d) })
	gp.POST("/:id/refresh-rates", func(c *gin.Context) { refreshRates(c, d) })
	gp.POST("/:id/redeem", func(c *gin.Context) { redeemChannel(c, d) })
	gp.GET("/:id/recharge-info", func(c *gin.Context) { channelRechargeInfo(c, d) })
	gp.POST("/:id/recharge", func(c *gin.Context) { createChannelRecharge(c, d) })
	gp.GET("/:id/subscription-info", func(c *gin.Context) { channelSubscriptionInfo(c, d) })
	gp.POST("/:id/subscription", func(c *gin.Context) { createChannelSubscription(c, d) })
	gp.GET("/:id/subscription-usage", func(c *gin.Context) { channelSubscriptionUsage(c, d) })
	gp.GET("/:id/api-keys/groups", func(c *gin.Context) { listChannelAPIKeyGroups(c, d) })
	gp.GET("/:id/api-keys", func(c *gin.Context) { listChannelAPIKeys(c, d) })
	gp.POST("/:id/api-keys", func(c *gin.Context) { createChannelAPIKey(c, d) })
	gp.PUT("/:id/api-keys/:key_id", func(c *gin.Context) { updateChannelAPIKey(c, d) })
	gp.DELETE("/:id/api-keys/:key_id", func(c *gin.Context) { deleteChannelAPIKey(c, d) })
	gp.POST("/:id/api-keys/:key_id/reveal", func(c *gin.Context) { revealChannelAPIKey(c, d) })
	gp.POST("/:id/sync", func(c *gin.Context) { syncChannel(c, d) })
	gp.GET("/:id/rates", func(c *gin.Context) { channelRates(c, d) })
	gp.GET("/:id/balance-history", func(c *gin.Context) { balanceHistory(c, d) })
}

type channelInput struct {
	Name                   string                 `json:"name" binding:"required"`
	Type                   storage.ChannelType    `json:"type" binding:"required"`
	SiteURL                string                 `json:"site_url" binding:"required"`
	Username               string                 `json:"username"`
	SortOrder              int                    `json:"sort_order"`
	Password               string                 `json:"password"`
	CredentialMode         storage.CredentialMode `json:"credential_mode"`
	TokenCredential        string                 `json:"token_credential"` // JSON：token 模式时填写
	LoginExtraParams       string                 `json:"login_extra_params"`
	TurnstileEnabled       bool                   `json:"turnstile_enabled"`
	IgnoreAnnouncements    bool                   `json:"ignore_announcements"`
	SubscriptionEnabled    bool                   `json:"subscription_enabled"`
	ProxyEnabled           bool                   `json:"proxy_enabled"`
	CaptchaConfigID        *uint                  `json:"captcha_config_id"`
	BalanceThreshold       float64                `json:"balance_threshold"`
	RechargeMultiplier     *float64               `json:"recharge_multiplier"`
	RechargeMultiplierMode string                 `json:"recharge_multiplier_mode"`
	MonitorEnabled         bool                   `json:"monitor_enabled"`
	OnlyCreatedKeyGroupsEnabled bool             `json:"only_created_key_groups_enabled"`
	Tags                   []string               `json:"tags"`
	Notes                  string                 `json:"notes"`
}

type channelUpdateInput struct {
	Name                   *string                 `json:"name"`
	SiteURL                *string                 `json:"site_url"`
	Username               *string                 `json:"username"`
	SortOrder              *int                    `json:"sort_order"`
	Password               *string                 `json:"password"`
	CredentialMode         *storage.CredentialMode `json:"credential_mode"`
	TokenCredential        *string                 `json:"token_credential"`
	LoginExtraParams       *string                 `json:"login_extra_params"`
	TurnstileEnabled       *bool                   `json:"turnstile_enabled"`
	IgnoreAnnouncements    *bool                   `json:"ignore_announcements"`
	SubscriptionEnabled    *bool                   `json:"subscription_enabled"`
	ProxyEnabled           *bool                   `json:"proxy_enabled"`
	CaptchaConfigID        *uint                   `json:"captcha_config_id"`
	BalanceThreshold       *float64                `json:"balance_threshold"`
	RechargeMultiplier     *float64                `json:"recharge_multiplier"`
	RechargeMultiplierMode *string                 `json:"recharge_multiplier_mode"`
	MonitorEnabled         *bool                   `json:"monitor_enabled"`
	OnlyCreatedKeyGroupsEnabled *bool             `json:"only_created_key_groups_enabled"`
	Tags                   *[]string               `json:"tags"` // 省略或 null 表示不修改，[] 表示清空
	Notes                  *string                 `json:"notes"`
}

type channelOutput struct {
	storage.Channel
	UserID string `json:"user_id,omitempty"`
}

type channelRedeemInput struct {
	Code string `json:"code"`
}

type channelRechargeInput struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
	IsMobile      bool    `json:"is_mobile"`
}

type channelSubscriptionInput struct {
	PlanID        string `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
	IsMobile      bool   `json:"is_mobile"`
}

type channelAPIKeyCreateInput = connector.APIKeyCreateRequest
type channelAPIKeyUpdateInput = connector.APIKeyUpdateRequest

func listChannels(c *gin.Context, d *Deps) {
	if c.Query("page") != "" || c.Query("page_size") != "" {
		page, pageSize, err := parseChannelPageQuery(c)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		filter, err := parseChannelListFilter(c)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		list, total, err := d.Channels.ListPage(page, pageSize, filter)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		pages := 1
		if total > 0 && pageSize != -1 {
			pages = int((total + int64(pageSize) - 1) / int64(pageSize))
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"items":     channelOutputs(d, list),
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"pages":     pages,
		}})
		return
	}

	list, err := d.Channels.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": channelOutputs(d, list)})
}

func createChannel(c *gin.Context, d *Deps) {
	var in channelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	tags, err := normalizeChannelTagsInput(in.Tags)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	notes, err := normalizeChannelNotesInput(in.Notes)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	created, err := d.ChannelSvc.Create(channel.CreateInput{
		Name:                   in.Name,
		Type:                   in.Type,
		SiteURL:                in.SiteURL,
		Username:               in.Username,
		SortOrder:              in.SortOrder,
		Password:               in.Password,
		CredentialMode:         in.CredentialMode,
		TokenCredential:        in.TokenCredential,
		LoginExtraParams:       in.LoginExtraParams,
		TurnstileEnabled:       in.TurnstileEnabled,
		IgnoreAnnouncements:    in.IgnoreAnnouncements,
		SubscriptionEnabled:    in.Type == storage.ChannelTypeSub2API && in.SubscriptionEnabled,
		ProxyEnabled:           in.ProxyEnabled,
		CaptchaConfigID:        in.CaptchaConfigID,
		BalanceThreshold:       in.BalanceThreshold,
		RechargeMultiplier:     in.RechargeMultiplier,
		RechargeMultiplierMode: in.RechargeMultiplierMode,
		MonitorEnabled:         in.MonitorEnabled,
		OnlyCreatedKeyGroupsEnabled: in.OnlyCreatedKeyGroupsEnabled,
		Tags:                   tags,
		Notes:                  notes,
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": channelOutputFor(d, *created)})
}

// normalizeChannelTagsInput 规范化并校验渠道标签（个数 / 单个长度按规范化后的结果计算）。
func normalizeChannelTagsInput(tags []string) ([]string, error) {
	tags = storage.NormalizeChannelTags(tags)
	if len(tags) > storage.MaxChannelTags {
		return nil, fmt.Errorf("标签最多 %d 个", storage.MaxChannelTags)
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > storage.MaxChannelTagRunes {
			return nil, fmt.Errorf("单个标签最多 %d 个字符：%s", storage.MaxChannelTagRunes, tag)
		}
	}
	return tags, nil
}

// normalizeChannelNotesInput 去掉首尾空白后校验备注长度。
func normalizeChannelNotesInput(notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > storage.MaxChannelNotesRunes {
		return "", fmt.Errorf("备注最多 %d 个字符", storage.MaxChannelNotesRunes)
	}
	return notes, nil
}

// costNow 输出层判断"今日消费"是否跨天使用的当前时间，测试可替换。
var costNow = time.Now

func channelOutputs(d *Deps, list []storage.Channel) []channelOutput {
	now := costNow()
	out := make([]channelOutput, 0, len(list))
	for _, ch := range list {
		out = append(out, channelOutputAt(d, ch, now))
	}
	return out
}

func channelOutputFor(d *Deps, ch storage.Channel) channelOutput {
	return channelOutputAt(d, ch, costNow())
}

// channelOutputAt 组装渠道输出；TodayCost 按 now 做跨天归零（只影响响应，不回写数据库）。
func channelOutputAt(d *Deps, ch storage.Channel, now time.Time) channelOutput {
	out := channelOutput{Channel: ch}
	out.TodayCost = ch.EffectiveTodayCost(now)
	out.UserID = channelUserID(d, &ch)
	return out
}

func channelUserID(d *Deps, ch *storage.Channel) string {
	if d == nil || ch == nil || ch.Type != storage.ChannelTypeNewAPI {
		return ""
	}
	if ch.CredentialMode == storage.CredentialModeToken && d.Cipher != nil && ch.PasswordCipher != "" {
		raw, err := d.Cipher.Decrypt(ch.PasswordCipher)
		if err == nil {
			var cred channel.NewAPITokenCredential
			if json.Unmarshal([]byte(raw), &cred) == nil {
				if userID := strings.TrimSpace(cred.UserID); userID != "" {
					return userID
				}
			}
		}
	}
	if d.Sessions != nil {
		session, err := d.Sessions.FindByChannel(ch.ID)
		if err == nil && session != nil {
			return strings.TrimSpace(session.UserID)
		}
	}
	return ""
}

func getChannel(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ch, err := d.Channels.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": channelOutputFor(d, *ch)})
}

func updateChannel(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in channelUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var tags *[]string
	if in.Tags != nil {
		normalized, err := normalizeChannelTagsInput(*in.Tags)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		tags = &normalized
	}
	var notes *string
	if in.Notes != nil {
		normalized, err := normalizeChannelNotesInput(*in.Notes)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		notes = &normalized
	}
	subscriptionEnabled := in.SubscriptionEnabled
	if subscriptionEnabled != nil {
		current, err := d.Channels.FindByID(id)
		if err != nil {
			fail(c, http.StatusNotFound, err)
			return
		}
		enabled := current.Type == storage.ChannelTypeSub2API && *subscriptionEnabled
		subscriptionEnabled = &enabled
	}
	updated, err := d.ChannelSvc.Update(id, channel.UpdateInput{
		Name:                   in.Name,
		SiteURL:                in.SiteURL,
		Username:               in.Username,
		SortOrder:              in.SortOrder,
		Password:               in.Password,
		CredentialMode:         in.CredentialMode,
		TokenCredential:        in.TokenCredential,
		LoginExtraParams:       in.LoginExtraParams,
		TurnstileEnabled:       in.TurnstileEnabled,
		IgnoreAnnouncements:    in.IgnoreAnnouncements,
		SubscriptionEnabled:    subscriptionEnabled,
		ProxyEnabled:           in.ProxyEnabled,
		CaptchaConfigID:        in.CaptchaConfigID,
		BalanceThreshold:       in.BalanceThreshold,
		RechargeMultiplier:     in.RechargeMultiplier,
		RechargeMultiplierMode: in.RechargeMultiplierMode,
		MonitorEnabled:         in.MonitorEnabled,
		OnlyCreatedKeyGroupsEnabled: in.OnlyCreatedKeyGroupsEnabled,
		Tags:                   tags,
		Notes:                  notes,
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": channelOutputFor(d, *updated)})
}

func deleteChannel(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := d.ChannelSvc.Delete(id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func clearChannelLoginInfo(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	updated, err := d.ChannelSvc.ClearLoginInfo(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": channelOutputFor(d, *updated)})
}

func toggleChannel(c *gin.Context, d *Deps, enabled bool) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	_, err = d.ChannelSvc.Update(id, channel.UpdateInput{MonitorEnabled: &enabled})
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "monitor_enabled": enabled})
}

func testLogin(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ch, err := d.Channels.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}

	obs := setupSSE(c)
	ctx := progress.WithObserver(c.Request.Context(), channelScopedObserver{
		base:        obs,
		channelID:   ch.ID,
		channelName: ch.Name,
		index:       1,
		total:       1,
	})

	if err := d.ChannelSvc.TestLogin(ctx, id); err != nil {
		progress.Fail(ctx, progress.StageError, err.Error())
		return
	}
	progress.OK(ctx, progress.StageDone, "登录测试成功")
}

func refreshBalance(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ch, err := d.Channels.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	if err := d.Monitor.RefreshBalance(c.Request.Context(), ch); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func refreshRates(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ch, err := d.Channels.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	if err := d.Monitor.RefreshRates(c.Request.Context(), ch); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func redeemChannel(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if _, err := d.Channels.FindByID(id); err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}

	var in channelRedeemInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	res, err := d.ChannelSvc.RedeemCode(c.Request.Context(), id, in.Code)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func channelRechargeInfo(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	info, err := d.ChannelSvc.GetRechargeInfo(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func createChannelRecharge(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in channelRechargeInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if in.Amount <= 0 {
		fail(c, http.StatusBadRequest, fmt.Errorf("充值金额必须大于 0"))
		return
	}
	if in.PaymentMethod != "alipay" && in.PaymentMethod != "wxpay" {
		fail(c, http.StatusBadRequest, fmt.Errorf("仅支持 alipay 或 wxpay"))
		return
	}
	res, err := d.ChannelSvc.CreateRecharge(c.Request.Context(), id, connector.RechargeRequest{
		Amount:        in.Amount,
		PaymentMethod: in.PaymentMethod,
		IsMobile:      in.IsMobile,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func channelSubscriptionInfo(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	info, err := d.ChannelSvc.GetSubscriptionInfo(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func createChannelSubscription(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in channelSubscriptionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.PlanID) == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("请选择订阅套餐"))
		return
	}
	if strings.TrimSpace(in.PaymentMethod) == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("请选择支付方式"))
		return
	}
	res, err := d.ChannelSvc.CreateSubscription(c.Request.Context(), id, connector.SubscriptionRequest{
		PlanID:        in.PlanID,
		PaymentMethod: in.PaymentMethod,
		IsMobile:      in.IsMobile,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func channelSubscriptionUsage(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	info, err := d.ChannelSvc.GetSubscriptionUsage(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func listChannelAPIKeys(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	page, pageSize, err := parsePageQuery(c)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	res, err := d.ChannelSvc.ListAPIKeys(c.Request.Context(), id, connector.APIKeyQuery{
		Page:     page,
		PageSize: pageSize,
		Search:   c.Query("search"),
		Status:   c.Query("status"),
		GroupID:  c.Query("group_id"),
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func listChannelAPIKeyGroups(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	res, err := d.ChannelSvc.ListAPIKeyGroups(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func createChannelAPIKey(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in channelAPIKeyCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("密钥名称不能为空"))
		return
	}
	res, err := d.ChannelSvc.CreateAPIKey(c.Request.Context(), id, connector.APIKeyCreateRequest(in))
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func updateChannelAPIKey(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	keyID, err := int64Param(c, "key_id")
	if err != nil || keyID <= 0 {
		fail(c, http.StatusBadRequest, fmt.Errorf("密钥 ID 无效"))
		return
	}
	var in channelAPIKeyUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	res, err := d.ChannelSvc.UpdateAPIKey(c.Request.Context(), id, keyID, connector.APIKeyUpdateRequest(in))
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func deleteChannelAPIKey(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	keyID, err := int64Param(c, "key_id")
	if err != nil || keyID <= 0 {
		fail(c, http.StatusBadRequest, fmt.Errorf("密钥 ID 无效"))
		return
	}
	if err := d.ChannelSvc.DeleteAPIKey(c.Request.Context(), id, keyID); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func revealChannelAPIKey(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	keyID, err := int64Param(c, "key_id")
	if err != nil || keyID <= 0 {
		fail(c, http.StatusBadRequest, fmt.Errorf("密钥 ID 无效"))
		return
	}
	key, err := d.ChannelSvc.RevealAPIKey(c.Request.Context(), id, keyID)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"key": key}})
}

func channelRates(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	list, err := d.Rates.ListByChannel(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	// ?only_with_keys=1：仅返回"已创建密钥的分组"。
	// 该参数由前端按渠道开关 channel.only_created_key_groups_enabled 拼接，
	// 网关 / 通知设置等全量场景不传该参数，仍返回完整 RateSnapshot。
	if onlyWithKeys := c.Query("only_with_keys"); onlyWithKeys == "1" || strings.EqualFold(onlyWithKeys, "true") {
		set, err := d.ChannelSvc.ListAPIKeyGroupSet(c.Request.Context(), id)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		if set.Empty() {
			// 没有任何已创建密钥 → 返回空数组（开关语义就是"只显示有密钥的分组"）。
			c.JSON(http.StatusOK, gin.H{"data": []storage.RateSnapshot{}})
			return
		}
		filtered := make([]storage.RateSnapshot, 0, len(list))
		for _, snap := range list {
			if set.ContainsSnapshot(snap) {
				filtered = append(filtered, snap)
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": filtered})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func balanceHistory(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	list, err := d.Rates.BalanceHistory(id, limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func uintParam(c *gin.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	return uint(id), err
}

func int64Param(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(c.Param(name), 10, 64)
}

func parsePageQuery(c *gin.Context) (int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		return 0, 0, fmt.Errorf("page 必须是正整数")
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize < 1 {
		return 0, 0, fmt.Errorf("page_size 必须是正整数")
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, nil
}

func parseChannelPageQuery(c *gin.Context) (int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		return 0, 0, fmt.Errorf("page 必须是正整数")
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize == 0 || pageSize < -1 {
		return 0, 0, fmt.Errorf("page_size 必须是正整数或 -1")
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, nil
}

// parseChannelListFilter 解析分页列表的 q / status / tag / sort / order 参数，
// 未知的 status / sort / order 或超长的 q / tag 返回错误（由调用方转成 400）。
func parseChannelListFilter(c *gin.Context) (storage.ChannelListFilter, error) {
	return storage.ChannelListFilter{
		Query:  c.Query("q"),
		Status: c.Query("status"),
		Tag:    c.Query("tag"),
		Sort:   c.Query("sort"),
		Order:  c.Query("order"),
	}.Normalize()
}

// setupSSE 给 ResponseWriter 设上 text/event-stream 头，返回一个就绪的 sseObserver。
// 调用方接下来一般是：
//
//	obs := setupSSE(c)
//	ctx := progress.WithObserver(c.Request.Context(), obs)
//	// ... 业务逻辑里的 progress.Start / OK / Fail 会被实时 stream 出去 ...
//	obs.Emit(progress.Event{Stage: progress.StageDone, Message: "完成"})
func setupSSE(c *gin.Context) *sseObserver {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache, no-transform")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // disable nginx-style proxy buffering
	c.Writer.WriteHeader(http.StatusOK)

	obs := &sseObserver{w: c.Writer}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		obs.flush = flusher.Flush
	}
	return obs
}

// sseObserver 把 progress.Event 序列化成 SSE 格式写入 ResponseWriter。
// 因为 gin 的 Handler 在一个 goroutine 中跑，而 emit 可能从下游同步 / 异步发起，
// 这里加锁保证 writer 串行写。
type sseObserver struct {
	mu     sync.Mutex
	w      io.Writer
	flush  func()
	closed bool
}

func (o *sseObserver) Emit(ev progress.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// SSE: "data: <json>\n\n"
	if _, err := io.WriteString(o.w, "data: "); err != nil {
		o.closed = true
		return
	}
	if _, err := o.w.Write(payload); err != nil {
		o.closed = true
		return
	}
	if _, err := io.WriteString(o.w, "\n\n"); err != nil {
		o.closed = true
		return
	}
	if o.flush != nil {
		o.flush()
	}
}

type channelScopedObserver struct {
	base        progress.Observer
	channelID   uint
	channelName string
	index       int
	total       int
}

func (o channelScopedObserver) Emit(ev progress.Event) {
	ev.ChannelID = o.channelID
	ev.ChannelName = o.channelName
	ev.Index = o.index
	ev.Total = o.total
	o.base.Emit(ev)
}

// syncChannel 通过 SSE 把整个同步过程的子步骤实时推给前端。
//
//	GET / POST /api/channels/:id/sync
//	响应 Content-Type: text/event-stream，每条事件形如
//	  data: {"stage":"login","message":"登录上游…","time":"..."}
//
// 前端用 fetch + ReadableStream 读取，按 "\n\n" 切片解析。
func syncChannel(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	ch, err := d.Channels.FindByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}

	obs := setupSSE(c)
	ctx := progress.WithObserver(c.Request.Context(), channelScopedObserver{
		base:        obs,
		channelID:   ch.ID,
		channelName: ch.Name,
		index:       1,
		total:       1,
	})

	// 串行执行：先余额，再订阅告警，再倍率。任一步失败仍尝试下一个，但用 done 表示整体状态。
	balErr := d.Monitor.RefreshBalance(ctx, ch)
	var subErr error
	if balErr == nil {
		subErr = d.Monitor.CheckSubscriptionUsageAlerts(ctx, ch)
	}
	rateErr := d.Monitor.RefreshRates(ctx, ch)

	switch {
	case balErr != nil || subErr != nil || rateErr != nil:
		progress.Fail(ctx, progress.StageError, joinErrorMessages(balErr, subErr, rateErr))
	default:
		progress.OK(ctx, progress.StageDone, "同步完成")
	}
}

func syncAllChannels(c *gin.Context, d *Deps) {
	list, err := d.Channels.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	obs := setupSSE(c)
	baseCtx := c.Request.Context()
	total := len(list)
	var successCount, failedCount int

	if total == 0 {
		obs.Emit(progress.Event{
			Stage:   progress.StageDone,
			Message: "批量同步完成：成功 0，失败 0",
			Time:    time.Now(),
		})
		return
	}

	for i := range list {
		ch := list[i]
		scoped := channelScopedObserver{
			base:        obs,
			channelID:   ch.ID,
			channelName: ch.Name,
			index:       i + 1,
			total:       total,
		}
		ctx := progress.WithObserver(baseCtx, scoped)

		if err := d.Monitor.RefreshBalance(ctx, &ch); err != nil {
			failedCount++
			scoped.Emit(progress.Event{
				Stage:   progress.StageError,
				Message: fmt.Sprintf("同步失败：%v", err),
				Time:    time.Now(),
			})
			continue
		}
		if err := d.Monitor.CheckSubscriptionUsageAlerts(ctx, &ch); err != nil {
			failedCount++
			scoped.Emit(progress.Event{
				Stage:   progress.StageError,
				Message: fmt.Sprintf("订阅检查失败：%v", err),
				Time:    time.Now(),
			})
			continue
		}

		successCount++
		scoped.Emit(progress.Event{
			Stage:   progress.StageDone,
			Message: "同步完成",
			Time:    time.Now(),
		})
	}

	summary := fmt.Sprintf("批量同步完成：成功 %d，失败 %d", successCount, failedCount)
	stage := progress.StageDone
	if failedCount > 0 {
		stage = progress.StageError
	}
	obs.Emit(progress.Event{
		Stage:   stage,
		Message: summary,
		Time:    time.Now(),
	})
}

func joinErrorMessages(errs ...error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, " | ")
}
