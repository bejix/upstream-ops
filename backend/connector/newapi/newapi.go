// Package newapi 实现对 NewAPI 风格上游站点的 connector，参考 docs/USER_BALANCE_GROUP_RATE_AUTH_API_CN-newapi.md。
package newapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/go-resty/resty/v2"
)

func init() {
	connector.Register(connector.TypeNewAPI, func() connector.Connector { return New() })
}

// newAPIRefreshCookieName 新版 NewAPI 登录 / 刷新接口用于承载刷新令牌的 Cookie 名称。
// 与上游常量 service.RefreshCookieName 保持一致，值本身即为 opaque 刷新令牌。
const newAPIRefreshCookieName = "new_api_refresh"

// Client NewAPI connector 实现。
type Client struct {
	http *resty.Client
}

func New() *Client {
	c := resty.New().
		SetTimeout(30*time.Second).
		SetHeader("User-Agent", "upstream-ops/0.1").
		SetHeader("Accept", "application/json")
	return &Client{http: c}
}

func (c *Client) SetProxy(proxyURL string) {
	if strings.TrimSpace(proxyURL) == "" {
		return
	}
	c.http.SetProxy(proxyURL)
}

func (c *Client) SetHTTPConfig(cfg connector.HTTPConfig) {
	if cfg.Timeout > 0 {
		c.http.SetTimeout(cfg.Timeout)
	}
	if strings.TrimSpace(cfg.UserAgent) != "" {
		c.http.SetHeader("User-Agent", cfg.UserAgent)
	}
}

// newapiResp NewAPI 统一响应外壳：{ success, message, data }。
type newapiResp struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) GetTurnstileSiteKey(ctx context.Context, ch *connector.Channel) (string, error) {
	body, err := c.getJSON(ctx, strings.TrimRight(ch.SiteURL, "/")+"/api/status", nil)
	if err != nil {
		return "", fmt.Errorf("newapi status: %w", err)
	}
	var status struct {
		TurnstileCheck   bool   `json:"turnstile_check"`
		TurnstileSiteKey string `json:"turnstile_site_key"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return "", fmt.Errorf("newapi status decode: %w", err)
	}
	if !status.TurnstileCheck {
		return "", nil
	}
	return status.TurnstileSiteKey, nil
}

func (c *Client) Login(ctx context.Context, ch *connector.Channel) (*connector.AuthSession, error) {
	site, origin, err := refreshSite(ch.SiteURL)
	if err != nil {
		site = strings.TrimRight(ch.SiteURL, "/")
		origin = site
	}
	body := map[string]any{
		"username": ch.Username,
		"password": ch.Password,
	}
	for k, v := range ch.LoginExtraParams {
		body[k] = v
	}
	req := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Origin", origin).
		SetHeader("Referer", origin+"/").
		SetBody(body)
	if ch.TurnstileToken != "" {
		req.SetQueryParam("turnstile", ch.TurnstileToken)
	}

	resp, err := req.Post(site + "/api/user/login")
	if err != nil {
		return nil, fmt.Errorf("newapi login http: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("newapi login: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	var wrapped newapiResp
	if err := json.Unmarshal(resp.Body(), &wrapped); err != nil {
		return nil, fmt.Errorf("newapi login decode: %w", err)
	}
	if !wrapped.Success {
		return nil, fmt.Errorf("newapi login: %s", wrapped.Message)
	}

	// 新版 NewAPI (controller/user.go setupLoginAtAuthVersion) 返回：
	//   data: { access_token, token_type, access_expires_at, session, user: { id, ... } }
	// 鉴权方式改为 `Authorization: Bearer <jwt>`，session cookie 仅用于刷新。
	// 老版本 NewAPI 仍以 `Set-Cookie: session=...` + `data.id` 形式响应对接，
	// 这里保留双模式解析：优先 access_token，回退 cookie + data.id。
	var data struct {
		Require2FA      bool   `json:"require_2fa"`
		ID              int64  `json:"id"`
		AccessToken     string `json:"access_token"`
		AccessExpiresAt int64  `json:"access_expires_at"`
		User            struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(wrapped.Data, &data)
	if data.Require2FA {
		return nil, errors.New("newapi account requires 2FA; please disable it for monitoring accounts")
	}

	// 优先新版 access_token 鉴权路径：登录响应里只要带了 access_token就走 JWT 模式。
	if token := strings.TrimSpace(data.AccessToken); token != "" {
		if data.User.ID == 0 {
			// 新版响应里用户 id 必须嵌在 data.user.id；缺失说明协议不符。
			return nil, errors.New("newapi login: missing user id in response")
		}
		expires := time.Now().Add(14 * time.Minute)
		if data.AccessExpiresAt > 0 {
			expires = time.Unix(data.AccessExpiresAt, 0)
		}
		// 新版登录在同一次响应里通过 Set-Cookie: new_api_refresh=<rotated> 下发刷新令牌；
		// 该 cookie 仅用于后续 /api/user/auth/refresh 续期，不参与业务鉴权，单独存放 RefreshToken。
		refreshToken := pickCookieValue(resp.Cookies(), newAPIRefreshCookieName)
		return &connector.AuthSession{
			UserID:       strconv.FormatInt(data.User.ID, 10),
			AccessToken:  token,
			RefreshToken: refreshToken,
			ExpiresAt:    expires,
		}, nil
	}

	// 回退老版本 cookie 鉴权路径。
	cookie := joinCookies(resp.Cookies())
	if cookie == "" {
		return nil, errors.New("newapi login: no session cookie returned")
	}
	if data.ID == 0 {
		// 用户 id 是后续 New-Api-User 头的必需值；缺失说明响应格式不对。
		return nil, errors.New("newapi login: missing user id in response")
	}
	// NewAPI session 默认有效期较长，保守按 7 天估算；CheckAuth 会兜底失效检测。
	return &connector.AuthSession{
		UserID:    strconv.FormatInt(data.ID, 10),
		Cookie:    cookie,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}, nil
}

func (c *Client) CheckAuth(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) error {
	if session == nil || !newAPIHasAuth(session) {
		return errors.New("missing newapi credential: 需要提供 Cookie 或系统访问令牌")
	}
	_, err := c.getJSON(ctx, strings.TrimRight(ch.SiteURL, "/")+"/api/user/self", session)
	return err
}

// RefreshSession 实现 connector.SessionRefresher。
//
// 调用上游 `POST /api/user/auth/refresh`（controller/auth_session.go RefreshAuth）：
//   - 请求体为空，刷新令牌通过 Cookie: new_api_refresh=<RefreshToken> 提交；
//     Origin 由 channel.SiteURL 推导，用于新版 NewAPI 的同源 Cookie 校验；
//     可选 X-Auth-Session 头携带 SID 做越权校验，我们没有显式跟踪 SID 故不发送，
//     由服务器按 cookie 自身记录的 SID 校验。
//   - 成功响应 shape 与 Login 一致：data.{access_token, token_type, access_expires_at, user.id, session}；
//     新的 refresh token 通过 Set-Cookie: new_api_refresh=<rotated> 下发（cookie-only，不在 JSON body）。
//
// 没有 RefreshToken（老版本 cookie-only 会话或 token 直配模式）时返回错误，
// 让 channel/service.go 退回到重新登录路径（见 sub2api 同样的 SessionRefresher 实现）。
func (c *Client) RefreshSession(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.AuthSession, error) {
	if session == nil || strings.TrimSpace(session.RefreshToken) == "" {
		return nil, connector.WrapSessionRefreshError(errors.New("newapi refresh: missing refresh token"), false)
	}
	site, origin, err := refreshSite(ch.SiteURL)
	if err != nil {
		return nil, connector.WrapSessionRefreshError(err, false)
	}
	req := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("Origin", origin).
		SetHeader("Referer", origin+"/").
		SetHeader("Cookie", newAPIRefreshCookieName+"="+strings.TrimSpace(session.RefreshToken))
	if strings.TrimSpace(session.UserID) != "" {
		// 与业务鉴权头一致的行为：New-Api-User 不会在新版 middleware 里被读取，
		// 但旧版下游节点回放兼容时可能依赖，同 Login 后的鉴权头一起带上不会有害。
		req.SetHeader("New-Api-User", session.UserID)
	}

	resp, err := req.Post(site + "/api/user/auth/refresh")
	if err != nil {
		return nil, connector.WrapSessionRefreshError(fmt.Errorf("newapi refresh http: %w", err), false)
	}
	if resp.IsError() {
		allowFallback := resp.StatusCode() == http.StatusUnauthorized
		return nil, connector.WrapSessionRefreshError(fmt.Errorf("newapi refresh: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body())), allowFallback)
	}

	var wrapped newapiResp
	if err := json.Unmarshal(resp.Body(), &wrapped); err != nil {
		return nil, connector.WrapSessionRefreshError(fmt.Errorf("newapi refresh decode: %w", err), false)
	}
	if !wrapped.Success {
		return nil, connector.WrapSessionRefreshError(fmt.Errorf("newapi refresh: %s", wrapped.Message), false)
	}

	var data struct {
		AccessToken     string `json:"access_token"`
		AccessExpiresAt int64  `json:"access_expires_at"`
		User            struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(wrapped.Data, &data); err != nil {
		return nil, connector.WrapSessionRefreshError(fmt.Errorf("newapi refresh data decode: %w", err), false)
	}
	if strings.TrimSpace(data.AccessToken) == "" || data.User.ID == 0 {
		return nil, connector.WrapSessionRefreshError(errors.New("newapi refresh: missing access_token or user id in response"), false)
	}

	expires := time.Now().Add(14 * time.Minute)
	if data.AccessExpiresAt > 0 {
		expires = time.Unix(data.AccessExpiresAt, 0)
	}
	refreshed := *session
	refreshed.AccessToken = strings.TrimSpace(data.AccessToken)
	if rotated := pickCookieValue(resp.Cookies(), newAPIRefreshCookieName); rotated != "" {
		refreshed.RefreshToken = rotated
	}
	refreshed.ExpiresAt = expires
	// 刷新响应里 user.id 与原 session.UserID 应一致；若上游因账号合并等出现偏差，
	// 以最新响应为准，避免后续 New-Api-User 头错配。
	refreshed.UserID = strconv.FormatInt(data.User.ID, 10)
	// Cookie 仅老版本鉴权路径使用；refresh 不维护它，清空以免误用旧 cookie。
	refreshed.Cookie = ""
	return &refreshed, nil
}

func refreshSite(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", fmt.Errorf("invalid NewAPI site URL: %q", raw)
	}
	return strings.TrimRight(u.String(), "/"), u.Scheme + "://" + u.Host, nil
}

// newAPIHasAuth 判断 session 是否带有 NewAPI 通过鉴权所必需的凭据。
// NewAPI 支持三种鉴权凭据：
//   - access_token（登录后获得的 dashboard JWT）
//   - 系统访问令牌（user.access_token，PAT）
//   - 老版本浏览器 session：Cookie 头（典型值 session=xxxxx; ...）
//
// 见 applyNewAPIAuth。
func newAPIHasAuth(session *connector.AuthSession) bool {
	if session == nil {
		return false
	}
	return strings.TrimSpace(session.AccessToken) != "" || strings.TrimSpace(session.Cookie) != ""
}

// applyNewAPIAuth 把当前 session 的鉴权头挂到 resty 请求上。
//   - 优先 Authorization: Bearer <AccessToken>，覆盖新版登录返回的 dashboard JWT 和手动配置的 PAT；
//     NewAPI middleware 会自动识别 JWT 与 PAT 两条路径（middleware/auth.go authorizationToken）。
//   - 没有 AccessToken 但有 Cookie 时回退老版本浏览器 session 鉴权；
//   - New-Api-User 仍始终带上：旧版 session 鉴权必备，PAT/JWT 路径下无害。
func applyNewAPIAuth(req *resty.Request, session *connector.AuthSession) {
	if session == nil {
		return
	}
	if token := strings.TrimSpace(session.AccessToken); token != "" {
		req.SetHeader("Authorization", "Bearer "+token)
	} else if cookie := strings.TrimSpace(session.Cookie); cookie != "" {
		req.SetHeader("Cookie", cookie)
	}
	if strings.TrimSpace(session.UserID) != "" {
		req.SetHeader("New-Api-User", session.UserID)
	}
}

func (c *Client) GetBalance(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.BalanceResult, error) {
	site := strings.TrimRight(ch.SiteURL, "/")
	statusBody, err := c.getJSON(ctx, site+"/api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("newapi status: %w", err)
	}
	var status struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
		Price        float64 `json:"price"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		return nil, fmt.Errorf("newapi status decode: %w", err)
	}
	if status.QuotaPerUnit <= 0 {
		status.QuotaPerUnit = 500000
	}

	selfBody, err := c.getJSON(ctx, site+"/api/user/self", session)
	if err != nil {
		return nil, fmt.Errorf("newapi self: %w", err)
	}
	var self struct {
		Quota float64 `json:"quota"`
	}
	if err := json.Unmarshal(selfBody, &self); err != nil {
		return nil, fmt.Errorf("newapi self decode: %w", err)
	}
	balance := c.quotaToUSD(self.Quota, status.QuotaPerUnit)
	multiplier := newAPIRechargeMultiplier(ch, status.Price)
	return &connector.BalanceResult{
		Balance:   connector.ApplyRechargeMultiplier(balance, multiplier, ch.RechargeMultiplierMode),
		SampledAt: time.Now(),
	}, nil
}

func (c *Client) GetCosts(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.CostResult, error) {
	site := strings.TrimRight(ch.SiteURL, "/")
	statusBody, err := c.getJSON(ctx, site+"/api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("newapi status: %w", err)
	}
	var status struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
		Price        float64 `json:"price"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		return nil, fmt.Errorf("newapi status decode: %w", err)
	}
	if status.QuotaPerUnit <= 0 {
		status.QuotaPerUnit = 500000
	}

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	end := now.Unix()
	logBody, err := c.getJSON(ctx, site+"/api/log/self/stat?type=0&token_name=&model_name=&start_timestamp="+strconv.FormatInt(start, 10)+"&end_timestamp="+strconv.FormatInt(end, 10)+"&group=", session)
	if err != nil {
		return nil, fmt.Errorf("newapi self stat: %w", err)
	}
	var todayStat struct {
		Quota float64 `json:"quota"`
	}
	if err := json.Unmarshal(logBody, &todayStat); err != nil {
		return nil, fmt.Errorf("newapi self stat decode: %w", err)
	}

	usageBody, err := c.getJSON(ctx, site+"/api/user/self", session)
	if err != nil {
		return nil, fmt.Errorf("newapi self total: %w", err)
	}
	var usage struct {
		UsedQuota float64 `json:"used_quota"`
	}
	if err := json.Unmarshal(usageBody, &usage); err != nil {
		return nil, fmt.Errorf("newapi self total decode: %w", err)
	}

	todayCost := c.quotaToUSD(todayStat.Quota, status.QuotaPerUnit)
	totalCost := c.quotaToUSD(usage.UsedQuota, status.QuotaPerUnit)
	multiplier := newAPIRechargeMultiplier(ch, status.Price)
	return &connector.CostResult{
		TodayCost: connector.ApplyRechargeMultiplier(todayCost, multiplier, ch.RechargeMultiplierMode),
		TotalCost: connector.ApplyRechargeMultiplier(totalCost, multiplier, ch.RechargeMultiplierMode),
	}, nil
}

func (c *Client) GetRates(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) ([]connector.RateResult, error) {
	body, err := c.getJSON(ctx, strings.TrimRight(ch.SiteURL, "/")+"/api/user/self/groups", session)
	if err != nil {
		return nil, fmt.Errorf("newapi groups: %w", err)
	}
	// data: { "default": { "ratio": 1, "desc": "..." }, "auto": { "ratio": "自动", ... } }
	raw := map[string]struct {
		Ratio json.RawMessage `json:"ratio"`
		Desc  string          `json:"desc"`
	}{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("newapi groups decode: %w", err)
	}
	out := make([]connector.RateResult, 0, len(raw))
	for name, v := range raw {
		var ratio float64
		if err := json.Unmarshal(v.Ratio, &ratio); err != nil {
			// "auto" 组的 ratio 是字符串 "自动"，跳过。
			continue
		}
		out = append(out, connector.RateResult{
			ModelName:   name,
			Description: v.Desc,
			Ratio:       ratio,
		})
	}
	return out, nil
}

func (c *Client) GetAnnouncements(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) ([]connector.AnnouncementResult, error) {
	site := strings.TrimRight(ch.SiteURL, "/")
	var items []connector.AnnouncementResult

	if body, err := c.getJSON(ctx, site+"/api/status", nil); err == nil {
		var status struct {
			Announcements []struct {
				ID              any             `json:"id"`
				Title           string          `json:"title"`
				Content         string          `json:"content"`
				Type            string          `json:"type"`
				Link            string          `json:"link"`
				PublishDate     string          `json:"publishDate"`
				PublishedAt     string          `json:"published_at"`
				CreatedAt       string          `json:"created_at"`
				UpdatedAt       string          `json:"updated_at"`
				SourceUpdatedAt string          `json:"source_updated_at"`
				Extra           json.RawMessage `json:"extra"`
			} `json:"announcements"`
		}
		if err := json.Unmarshal(body, &status); err == nil {
			for _, a := range status.Announcements {
				publishedAt := parseAnnouncementTime(a.PublishDate, a.PublishedAt, a.CreatedAt)
				updatedAt := parseAnnouncementTime(a.SourceUpdatedAt, a.UpdatedAt)
				items = append(items, connector.AnnouncementResult{
					SourceKey:       newAPIAnnouncementSourceKey(a.ID, a.Title, a.Content, a.Type, a.PublishDate, a.PublishedAt, a.CreatedAt, a.UpdatedAt),
					Title:           strings.TrimSpace(a.Title),
					Content:         strings.TrimSpace(a.Content),
					Type:            strings.TrimSpace(a.Type),
					Link:            strings.TrimSpace(a.Link),
					PublishedAt:     publishedAt,
					SourceUpdatedAt: updatedAt,
				})
			}
		}
	}

	if body, err := c.getRaw(ctx, site+"/api/notice", nil); err == nil {
		var wrapped newapiResp
		if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Success {
			text := strings.TrimSpace(newAPIStringFromRaw(wrapped.Data))
			if text != "" {
				items = append(items, connector.AnnouncementResult{
					SourceKey: hashAnnouncementKey("notice", text),
					Title:     "公告",
					Content:   text,
					Type:      "notice",
				})
			}
		}
	}

	return dedupeAnnouncements(items), nil
}

func (c *Client) RedeemCode(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, code string) (*connector.RedeemResult, error) {
	site := strings.TrimRight(ch.SiteURL, "/")
	statusBody, err := c.getJSON(ctx, site+"/api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("newapi status: %w", err)
	}
	var status struct {
		QuotaPerUnit float64 `json:"quota_per_unit"`
		Price        float64 `json:"price"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		return nil, fmt.Errorf("newapi status decode: %w", err)
	}
	if status.QuotaPerUnit <= 0 {
		status.QuotaPerUnit = 500000
	}

	req := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"key": code})
	applyNewAPIAuth(req, session)
	resp, err := req.Post(site + "/api/user/topup")
	if err != nil {
		return nil, fmt.Errorf("newapi redeem http: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("newapi redeem: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	var wrapped newapiResp
	if err := json.Unmarshal(resp.Body(), &wrapped); err != nil {
		return nil, fmt.Errorf("newapi redeem decode: %w", err)
	}
	if !wrapped.Success {
		return nil, fmt.Errorf("newapi redeem: %s", wrapped.Message)
	}

	var quota float64
	if err := json.Unmarshal(wrapped.Data, &quota); err != nil {
		return nil, fmt.Errorf("newapi redeem data: %w", err)
	}
	value := quota / status.QuotaPerUnit
	multiplier := newAPIRechargeMultiplier(ch, status.Price)
	return &connector.RedeemResult{
		Message: "兑换成功",
		Type:    "balance",
		Value:   connector.ApplyRechargeMultiplier(value, multiplier, ch.RechargeMultiplierMode),
	}, nil
}

func (c *Client) GetRechargeInfo(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.RechargeInfo, error) {
	body, err := c.getJSON(ctx, strings.TrimRight(ch.SiteURL, "/")+"/api/user/topup/info", session)
	if err != nil {
		return nil, fmt.Errorf("newapi topup info: %w", err)
	}
	var raw struct {
		PayMethods    json.RawMessage `json:"pay_methods"`
		MinTopup      float64         `json:"min_topup"`
		AmountOptions json.RawMessage `json:"amount_options"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("newapi topup info decode: %w", err)
	}
	payMethods, err := parseNewAPIPayMethods(raw.PayMethods)
	if err != nil {
		return nil, fmt.Errorf("newapi topup methods decode: %w", err)
	}
	amountOptions, err := parseNewAPIAmountOptions(raw.AmountOptions)
	if err != nil {
		return nil, fmt.Errorf("newapi amount options decode: %w", err)
	}
	methods := make([]connector.RechargeMethod, 0, len(payMethods))
	for _, m := range payMethods {
		t := strings.TrimSpace(m.Type)
		if t != "alipay" && t != "wxpay" {
			continue
		}
		minAmount := raw.MinTopup
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(m.MinTopup), 64); err == nil && parsed > 0 {
			minAmount = parsed
		}
		methods = append(methods, connector.RechargeMethod{
			Type:      t,
			Name:      strings.TrimSpace(m.Name),
			MinAmount: minAmount,
		})
	}
	if len(methods) == 0 {
		return nil, errors.New("上游未配置可用的支付宝/微信支付方式")
	}
	if raw.MinTopup <= 0 {
		raw.MinTopup = methods[0].MinAmount
	}
	if raw.MinTopup <= 0 {
		raw.MinTopup = 1
	}
	if len(amountOptions) == 0 && raw.MinTopup > 0 {
		amountOptions = []float64{raw.MinTopup}
	}
	return &connector.RechargeInfo{
		AmountLabel:   "充值数量",
		AmountStep:    1,
		MinAmount:     raw.MinTopup,
		PresetAmounts: amountOptions,
		Methods:       methods,
	}, nil
}

func (c *Client) CreateRecharge(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, req connector.RechargeRequest) (*connector.RechargeLaunch, error) {
	if req.PaymentMethod != "alipay" && req.PaymentMethod != "wxpay" {
		return nil, errors.New("newapi 仅支持 alipay 或 wxpay")
	}
	if req.Amount <= 0 || math.Trunc(req.Amount) != req.Amount {
		return nil, errors.New("newapi 充值数量必须是正整数")
	}
	r := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"amount":         int64(req.Amount),
			"payment_method": req.PaymentMethod,
		})
	applyNewAPIAuth(r, session)
	resp, err := r.Post(strings.TrimRight(ch.SiteURL, "/") + "/api/user/pay")
	if err != nil {
		return nil, fmt.Errorf("newapi create recharge http: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("newapi create recharge: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	var wrapped struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		URL     string          `json:"url"`
	}
	if err := json.Unmarshal(resp.Body(), &wrapped); err != nil {
		return nil, fmt.Errorf("newapi create recharge decode: %w", err)
	}
	if !wrapped.Success && !strings.EqualFold(strings.TrimSpace(wrapped.Message), "success") {
		msg := newAPIStringFromRaw(wrapped.Data)
		if msg == "" || strings.EqualFold(msg, "null") {
			msg = strings.TrimSpace(wrapped.Message)
		}
		if msg == "" {
			msg = "newapi 发起支付失败"
		}
		return nil, errors.New(msg)
	}
	fields := map[string]string{}
	if len(wrapped.Data) > 0 && string(wrapped.Data) != "null" {
		var rawFields map[string]any
		if err := json.Unmarshal(wrapped.Data, &rawFields); err != nil {
			return nil, fmt.Errorf("newapi create recharge data: %w", err)
		}
		for k, v := range rawFields {
			fields[k] = fmt.Sprint(v)
		}
	}
	action := strings.TrimSpace(wrapped.URL)
	if action == "" || len(fields) == 0 {
		return nil, errors.New("newapi 返回的支付表单信息不完整")
	}
	return &connector.RechargeLaunch{
		Mode:       "form",
		FormAction: action,
		FormFields: fields,
	}, nil
}

func (c *Client) GetSubscriptionInfo(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.SubscriptionInfo, error) {
	return nil, errors.New("newapi 不支持订阅购买")
}

func (c *Client) CreateSubscription(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, req connector.SubscriptionRequest) (*connector.SubscriptionLaunch, error) {
	return nil, errors.New("newapi 不支持订阅购买")
}

func (c *Client) GetSubscriptionUsage(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.SubscriptionUsageInfo, error) {
	return nil, errors.New("newapi 不支持订阅用量")
}

func (c *Client) ListAPIKeys(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	page, pageSize := normalizeAPIKeyPage(query.Page, query.PageSize)
	site := strings.TrimRight(ch.SiteURL, "/")
	params := url.Values{}
	params.Set("p", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(pageSize))
	path := "/api/token/"
	search := strings.TrimSpace(query.Search)
	if search != "" {
		path = "/api/token/search"
		params.Set("keyword", search)
		params.Set("token", search)
	}
	body, err := c.getJSON(ctx, site+path+"?"+params.Encode(), session)
	if err != nil {
		return nil, fmt.Errorf("newapi api keys: %w", err)
	}
	var raw struct {
		Items    []newAPIToken `json:"items"`
		Total    int64         `json:"total"`
		Page     int           `json:"page"`
		PageSize int           `json:"page_size"`
		Pages    int           `json:"pages"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("newapi api keys decode: %w", err)
	}
	groups, _ := c.newAPIGroupMap(ctx, ch, session)
	items := make([]connector.APIKey, 0, len(raw.Items))
	for _, item := range raw.Items {
		key := item.toConnector()
		if g, ok := groups[key.Group]; ok {
			key.GroupName = g.Name
			key.GroupDescription = g.Description
			key.GroupRatio = g.Ratio
		}
		items = append(items, key)
	}
	if raw.Page <= 0 {
		raw.Page = page
	}
	if raw.PageSize <= 0 {
		raw.PageSize = pageSize
	}
	if raw.Pages <= 0 {
		raw.Pages = pagesFromTotal(raw.Total, raw.PageSize)
	}
	return &connector.APIKeyPage{
		Items:    items,
		Total:    raw.Total,
		Page:     raw.Page,
		PageSize: raw.PageSize,
		Pages:    raw.Pages,
	}, nil
}

func (c *Client) ListAPIKeyGroups(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) ([]connector.APIKeyGroup, error) {
	groups, err := c.newAPIGroupMap(ctx, ch, session)
	if err != nil {
		return nil, err
	}
	out := make([]connector.APIKeyGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	return out, nil
}

func (c *Client) CreateAPIKey(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, req connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("密钥名称不能为空")
	}
	beforeIDs := map[int64]struct{}{}
	if before, err := c.ListAPIKeys(ctx, ch, session, connector.APIKeyQuery{Page: 1, PageSize: 100}); err == nil {
		for _, item := range before.Items {
			if item.ID > 0 {
				beforeIDs[item.ID] = struct{}{}
			}
		}
	}
	body := buildNewAPICreateToken(req)
	restyReq := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)
	applyNewAPIAuth(restyReq, session)
	resp, err := restyReq.Post(strings.TrimRight(ch.SiteURL, "/") + "/api/token/")
	if err != nil {
		return nil, fmt.Errorf("newapi create api key http: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("newapi create api key: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	data, err := decodeNewAPIWriteData(resp.Body(), "newapi create api key")
	if err != nil {
		return nil, err
	}
	if key := newAPIKeyFromCreateData(data, req); key != nil && key.ID > 0 {
		return key, nil
	}
	page, err := c.ListAPIKeys(ctx, ch, session, connector.APIKeyQuery{Page: 1, PageSize: 20, Search: req.Name})
	if err != nil {
		return nil, fmt.Errorf("newapi create api key lookup: %w", err)
	}
	name := strings.TrimSpace(req.Name)
	for i := range page.Items {
		if strings.TrimSpace(page.Items[i].Name) == name && page.Items[i].ID > 0 {
			return &page.Items[i], nil
		}
	}
	page, err = c.ListAPIKeys(ctx, ch, session, connector.APIKeyQuery{Page: 1, PageSize: 100})
	if err != nil {
		return nil, fmt.Errorf("newapi create api key list lookup: %w", err)
	}
	for i := range page.Items {
		if strings.TrimSpace(page.Items[i].Name) == name && page.Items[i].ID > 0 {
			return &page.Items[i], nil
		}
	}
	for i := range page.Items {
		if page.Items[i].ID <= 0 {
			continue
		}
		if _, existed := beforeIDs[page.Items[i].ID]; !existed {
			return &page.Items[i], nil
		}
	}
	return nil, errors.New("newapi create api key: missing key id")
}

func (c *Client) UpdateAPIKey(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, id int64, req connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	if id <= 0 {
		return nil, errors.New("密钥 ID 无效")
	}
	body := buildNewAPIUpdateToken(id, req)
	restyReq := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)
	applyNewAPIAuth(restyReq, session)
	resp, err := restyReq.Put(strings.TrimRight(ch.SiteURL, "/") + "/api/token/")
	if err != nil {
		return nil, fmt.Errorf("newapi update api key http: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("newapi update api key: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	data, err := decodeNewAPIWriteData(resp.Body(), "newapi update api key")
	if err != nil {
		return nil, err
	}
	var token newAPIToken
	if len(data) > 0 && string(data) != "null" {
		_ = json.Unmarshal(data, &token)
	}
	if token.ID == 0 {
		token.ID = int(id)
		if v, ok := body["name"].(string); ok {
			token.Name = v
		}
		if v, ok := body["status"].(int); ok {
			token.Status = v
		}
	}
	out := token.toConnector()
	return &out, nil
}

func (c *Client) DeleteAPIKey(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, id int64) error {
	if id <= 0 {
		return errors.New("密钥 ID 无效")
	}
	delReq := c.http.R().SetContext(ctx)
	applyNewAPIAuth(delReq, session)
	resp, err := delReq.Delete(strings.TrimRight(ch.SiteURL, "/") + "/api/token/" + strconv.FormatInt(id, 10))
	if err != nil {
		return fmt.Errorf("newapi delete api key http: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("newapi delete api key: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	return decodeNewAPIWrite(resp.Body(), "newapi delete api key")
}

func (c *Client) RevealAPIKey(ctx context.Context, ch *connector.Channel, session *connector.AuthSession, id int64) (string, error) {
	if id <= 0 {
		return "", errors.New("密钥 ID 无效")
	}
	revealReq := c.http.R().SetContext(ctx)
	applyNewAPIAuth(revealReq, session)
	resp, err := revealReq.Post(strings.TrimRight(ch.SiteURL, "/") + "/api/token/" + strconv.FormatInt(id, 10) + "/key")
	if err != nil {
		return "", fmt.Errorf("newapi reveal api key http: %w", err)
	}
	if resp.IsError() {
		return "", fmt.Errorf("newapi reveal api key: %w", connector.HTTPStatusError(resp.StatusCode(), resp.Body()))
	}
	data, err := decodeNewAPIWriteData(resp.Body(), "newapi reveal api key")
	if err != nil {
		return "", err
	}
	var raw struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("newapi reveal api key data: %w", err)
	}
	if strings.TrimSpace(raw.Key) == "" {
		return "", errors.New("newapi 未返回完整密钥")
	}
	return raw.Key, nil
}

type newAPIPayMethod struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	MinTopup string `json:"min_topup"`
}

func parseNewAPIPayMethods(raw json.RawMessage) ([]newAPIPayMethod, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var methods []newAPIPayMethod
	if err := json.Unmarshal(raw, &methods); err == nil {
		return methods, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(encoded), &methods); err != nil {
		return nil, err
	}
	return methods, nil
}

func parseNewAPIAmountOptions(raw json.RawMessage) ([]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var options []float64
	if err := json.Unmarshal(raw, &options); err == nil {
		return options, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(encoded), &options); err != nil {
		return nil, err
	}
	return options, nil
}

func newAPIStringFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func rawObjectToStringMap(raw json.RawMessage) (map[string]string, error) {
	fields := map[string]string{}
	if len(raw) == 0 || string(raw) == "null" {
		return fields, nil
	}
	var rawFields map[string]any
	if err := json.Unmarshal(raw, &rawFields); err != nil {
		return nil, err
	}
	for k, v := range rawFields {
		fields[k] = fmt.Sprint(v)
	}
	return fields, nil
}

func stringFieldFromRaw(raw json.RawMessage, field string) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	value, ok := data[field]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func decodeNewAPIPaymentResponse(body []byte, prefix string) (json.RawMessage, string, string, error) {
	var wrapped struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		URL     string          `json:"url"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, "", "", fmt.Errorf("%s decode: %w", prefix, err)
	}
	if !wrapped.Success && !strings.EqualFold(strings.TrimSpace(wrapped.Message), "success") {
		msg := strings.TrimSpace(wrapped.Message)
		if msg == "" {
			msg = newAPIStringFromRaw(wrapped.Data)
		}
		if msg == "" || strings.EqualFold(msg, "null") {
			msg = prefix + " failed"
		}
		return nil, "", "", errors.New(msg)
	}
	return wrapped.Data, strings.TrimSpace(wrapped.URL), strings.TrimSpace(wrapped.Message), nil
}

func (c *Client) getJSON(ctx context.Context, url string, session *connector.AuthSession) ([]byte, error) {
	body, err := c.getRaw(ctx, url, session)
	if err != nil {
		return nil, err
	}
	var wrapped newapiResp
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !wrapped.Success {
		return nil, errors.New(wrapped.Message)
	}
	return wrapped.Data, nil
}

func (c *Client) postJSON(ctx context.Context, url string, session *connector.AuthSession, body any) ([]byte, error) {
	req := c.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)
	applyNewAPIAuth(req, session)
	resp, err := req.Post(url)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, connector.HTTPStatusError(resp.StatusCode(), resp.Body())
	}
	return resp.Body(), nil
}

func (c *Client) getRaw(ctx context.Context, url string, session *connector.AuthSession) ([]byte, error) {
	req := c.http.R().SetContext(ctx)
	applyNewAPIAuth(req, session)
	// NewAPI 即便用 session 鉴权也要求带 New-Api-User 头（"unauthorized, New-Api-User header not provided"）。
	resp, err := req.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, connector.HTTPStatusError(resp.StatusCode(), resp.Body())
	}
	return resp.Body(), nil
}

func (c *Client) quotaToUSD(quota float64, quotaPerUnit float64) float64 {
	return round4(quota / quotaPerUnit)
}

func newAPIRechargeMultiplier(ch *connector.Channel, price float64) *float64 {
	if ch.RechargeMultiplier != nil && *ch.RechargeMultiplier > 0 {
		return ch.RechargeMultiplier
	}
	if price <= 0 {
		return nil
	}
	multiplier := 1 / price
	return &multiplier
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func joinCookies(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// pickCookieValue 从响应 Set-Cookie 列表里取出指定名称 cookie 的值；
// 同名多次出现时取最后一个（服务器轮换刷新令牌时最新值在末尾）。
func pickCookieValue(cookies []*http.Cookie, name string) string {
	var value string
	for _, c := range cookies {
		if c.Name == name {
			value = c.Value
		}
	}
	return value
}

func parseAnnouncementTime(values ...string) *time.Time {
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "0" {
			continue
		}
		if t, ok := parseFlexibleTime(raw); ok {
			return &t
		}
	}
	return nil
}

func parseFlexibleTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case n > 1e12:
			return time.UnixMilli(n), true
		case n > 1e9:
			return time.Unix(n, 0), true
		}
	}
	return time.Time{}, false
}

func newAPIAnnouncementSourceKey(id any, parts ...string) string {
	if id != nil {
		switch v := id.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return "id:" + s
			}
		case float64:
			if v != 0 {
				return "id:" + strconv.FormatInt(int64(v), 10)
			}
		default:
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" && s != "0" {
				return "id:" + s
			}
		}
	}
	return hashAnnouncementKey(parts...)
}

func hashAnnouncementKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(strings.TrimSpace(p)))
		h.Write([]byte{0})
	}
	return "hash:" + hex.EncodeToString(h.Sum(nil))
}

func dedupeAnnouncements(items []connector.AnnouncementResult) []connector.AnnouncementResult {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]connector.AnnouncementResult, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.SourceKey)
		if key == "" {
			key = hashAnnouncementKey(item.Title, item.Content, item.Type, item.Link)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		item.SourceKey = key
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PublishedAt == nil || out[j].PublishedAt == nil {
			return i < j
		}
		return out[i].PublishedAt.After(*out[j].PublishedAt)
	})
	return out
}

func (c *Client) newAPIGroupMap(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (map[string]connector.APIKeyGroup, error) {
	body, err := c.getJSON(ctx, strings.TrimRight(ch.SiteURL, "/")+"/api/user/self/groups", session)
	if err != nil {
		return nil, fmt.Errorf("newapi api key groups: %w", err)
	}
	raw := map[string]struct {
		Ratio json.RawMessage `json:"ratio"`
		Desc  string          `json:"desc"`
	}{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("newapi api key groups decode: %w", err)
	}
	out := make(map[string]connector.APIKeyGroup, len(raw))
	for name, v := range raw {
		var ratio float64
		if err := json.Unmarshal(v.Ratio, &ratio); err != nil {
			continue
		}
		out[name] = connector.APIKeyGroup{
			Name:        name,
			Description: v.Desc,
			Ratio:       ratio,
		}
	}
	return out, nil
}

type newAPIToken struct {
	ID                 int     `json:"id"`
	Key                string  `json:"key"`
	Status             int     `json:"status"`
	Name               string  `json:"name"`
	CreatedTime        int64   `json:"created_time"`
	AccessedTime       int64   `json:"accessed_time"`
	ExpiredTime        int64   `json:"expired_time"`
	RemainQuota        int     `json:"remain_quota"`
	UsedQuota          int     `json:"used_quota"`
	UnlimitedQuota     bool    `json:"unlimited_quota"`
	ModelLimitsEnabled bool    `json:"model_limits_enabled"`
	ModelLimits        string  `json:"model_limits"`
	AllowIPs           *string `json:"allow_ips"`
	Group              string  `json:"group"`
	CrossGroupRetry    bool    `json:"cross_group_retry"`
}

func (t newAPIToken) toConnector() connector.APIKey {
	var createdAt *time.Time
	if t.CreatedTime > 0 {
		v := time.Unix(t.CreatedTime, 0)
		createdAt = &v
	}
	var lastUsedAt *time.Time
	if t.AccessedTime > 0 {
		v := time.Unix(t.AccessedTime, 0)
		lastUsedAt = &v
	}
	allowIPs := ""
	if t.AllowIPs != nil {
		allowIPs = *t.AllowIPs
	}
	return connector.APIKey{
		ID:                 int64(t.ID),
		Key:                t.Key,
		Name:               t.Name,
		Status:             newAPITokenStatusToString(t.Status),
		Group:              t.Group,
		Quota:              float64(t.RemainQuota),
		QuotaUsed:          float64(t.UsedQuota),
		UnlimitedQuota:     t.UnlimitedQuota,
		ExpiredTime:        t.ExpiredTime,
		CreatedAt:          createdAt,
		LastUsedAt:         lastUsedAt,
		AllowIPs:           allowIPs,
		ModelLimitsEnabled: t.ModelLimitsEnabled,
		ModelLimits:        t.ModelLimits,
		CrossGroupRetry:    t.CrossGroupRetry,
	}
}

func buildNewAPICreateToken(req connector.APIKeyCreateRequest) map[string]any {
	body := map[string]any{
		"name":                 strings.TrimSpace(req.Name),
		"expired_time":         valueOr(req.ExpiredTime, int64(-1)),
		"remain_quota":         valueOr(req.RemainQuota, 0),
		"unlimited_quota":      valueOr(req.UnlimitedQuota, false),
		"model_limits_enabled": valueOr(req.ModelLimitsEnabled, false),
		"model_limits":         req.ModelLimits,
		"allow_ips":            req.AllowIPs,
		"group":                req.Group,
		"cross_group_retry":    valueOr(req.CrossGroupRetry, false),
	}
	if strings.TrimSpace(req.CustomKey) != "" {
		body["custom_key"] = strings.TrimSpace(req.CustomKey)
	}
	return body
}

func newAPIKeyFromCreateData(data json.RawMessage, req connector.APIKeyCreateRequest) *connector.APIKey {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var token newAPIToken
	if err := json.Unmarshal(data, &token); err == nil && token.ID > 0 {
		key := token.toConnector()
		fillNewAPICreatedKeyDefaults(&key, req)
		return &key
	}
	var id int64
	if err := json.Unmarshal(data, &id); err == nil && id > 0 {
		key := newAPIKeyFromCreateRequest(id, req)
		return &key
	}
	var idText string
	if err := json.Unmarshal(data, &idText); err == nil {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(idText), 10, 64); parseErr == nil && parsed > 0 {
			key := newAPIKeyFromCreateRequest(parsed, req)
			return &key
		}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		for _, field := range []string{"id", "token_id", "key_id"} {
			if id, ok := newAPIIDFromRaw(raw[field]); ok {
				key := newAPIKeyFromCreateRequest(id, req)
				return &key
			}
		}
	}
	return nil
}

func newAPIIDFromRaw(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil && id > 0 {
		return id, true
	}
	var idText string
	if err := json.Unmarshal(raw, &idText); err == nil {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(idText), 10, 64); parseErr == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func newAPIKeyFromCreateRequest(id int64, req connector.APIKeyCreateRequest) connector.APIKey {
	return connector.APIKey{
		ID:                 id,
		Name:               strings.TrimSpace(req.Name),
		Status:             "active",
		Group:              req.Group,
		Quota:              float64(valueOr(req.RemainQuota, 0)),
		UnlimitedQuota:     valueOr(req.UnlimitedQuota, false),
		ExpiredTime:        valueOr(req.ExpiredTime, int64(-1)),
		ModelLimitsEnabled: valueOr(req.ModelLimitsEnabled, false),
		ModelLimits:        req.ModelLimits,
		AllowIPs:           req.AllowIPs,
		CrossGroupRetry:    valueOr(req.CrossGroupRetry, false),
	}
}

func fillNewAPICreatedKeyDefaults(key *connector.APIKey, req connector.APIKeyCreateRequest) {
	if strings.TrimSpace(key.Name) == "" {
		key.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(key.Status) == "" || key.Status == "unknown" {
		key.Status = "active"
	}
	if strings.TrimSpace(key.Group) == "" {
		key.Group = req.Group
	}
}

func buildNewAPIUpdateToken(id int64, req connector.APIKeyUpdateRequest) map[string]any {
	body := map[string]any{"id": int(id)}
	if req.Name != nil {
		body["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Status != nil {
		body["status"] = newAPITokenStatusFromString(*req.Status)
	}
	if req.Group != nil {
		body["group"] = strings.TrimSpace(*req.Group)
	}
	if req.RemainQuota != nil {
		body["remain_quota"] = *req.RemainQuota
	}
	if req.UnlimitedQuota != nil {
		body["unlimited_quota"] = *req.UnlimitedQuota
	}
	if req.ExpiredTime != nil {
		body["expired_time"] = *req.ExpiredTime
	}
	if req.ModelLimitsEnabled != nil {
		body["model_limits_enabled"] = *req.ModelLimitsEnabled
	}
	if req.ModelLimits != nil {
		body["model_limits"] = *req.ModelLimits
	}
	if req.AllowIPs != nil {
		body["allow_ips"] = *req.AllowIPs
	}
	if req.CrossGroupRetry != nil {
		body["cross_group_retry"] = *req.CrossGroupRetry
	}
	return body
}

func decodeNewAPIWrite(body []byte, prefix string) error {
	_, err := decodeNewAPIWriteData(body, prefix)
	return err
}

func decodeNewAPIWriteData(body []byte, prefix string) (json.RawMessage, error) {
	var wrapped newapiResp
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("%s decode: %w", prefix, err)
	}
	if !wrapped.Success {
		msg := strings.TrimSpace(wrapped.Message)
		if msg == "" {
			msg = newAPIStringFromRaw(wrapped.Data)
		}
		if msg == "" {
			msg = prefix + " failed"
		}
		return nil, errors.New(msg)
	}
	return wrapped.Data, nil
}

func newAPITokenStatusToString(status int) string {
	switch status {
	case 1:
		return "active"
	case 2:
		return "disabled"
	case 3:
		return "expired"
	case 4:
		return "quota_exhausted"
	default:
		return "unknown"
	}
}

func newAPITokenStatusFromString(status string) int {
	switch strings.TrimSpace(status) {
	case "active":
		return 1
	case "disabled":
		return 2
	case "expired":
		return 3
	case "quota_exhausted":
		return 4
	default:
		return 0
	}
}

func normalizeAPIKeyPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func pagesFromTotal(total int64, pageSize int) int {
	if pageSize <= 0 {
		pageSize = 20
	}
	pages := int(math.Ceil(float64(total) / float64(pageSize)))
	if pages < 1 {
		return 1
	}
	return pages
}

func valueOr[T any](ptr *T, fallback T) T {
	if ptr == nil {
		return fallback
	}
	return *ptr
}
