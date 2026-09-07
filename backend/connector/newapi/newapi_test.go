package newapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
)

func TestSetHTTPConfigAppliesUserAgentAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "custom-agent" {
			t.Fatalf("user agent = %q", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{}}`))
	}))
	defer srv.Close()

	c := New()
	c.SetHTTPConfig(connector.HTTPConfig{
		Timeout:   45 * time.Second,
		UserAgent: "custom-agent",
	})
	if c.http.GetClient().Timeout != 45*time.Second {
		t.Fatalf("timeout = %s", c.http.GetClient().Timeout)
	}
	if _, err := c.getJSON(context.Background(), srv.URL, nil); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
}

func TestLoginAddsExtraParams(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["username"] != "u" || body["password"] != "p" || body["device_id"] != "d1" {
			t.Fatalf("body = %#v", body)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":7}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	session, err := c.Login(context.Background(), &connector.Channel{
		SiteURL:          srv.URL,
		Username:         "u",
		Password:         "p",
		LoginExtraParams: map[string]any{"device_id": "d1"},
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.UserID != "7" || session.Cookie == "" {
		t.Fatalf("session = %#v", session)
	}
}

// TestLoginParsesNewAPIAuthBundle 覆盖新版 NewAPI 登录响应：
// 响应体不再带顶层 id，而是嵌在 data.user.id，鉴权凭据改为 data.access_token。
// 站点只 Set refresh cookie（不参与 dashboard 鉴权），connector 必须忽略它改走 Bearer。
func TestLoginParsesNewAPIAuthBundle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["username"] != "u" || body["password"] != "p" {
			t.Fatalf("body = %#v", body)
		}
		// 新版新-api 只写 refresh cookie，鉴权完全走 Authorization Bearer。
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "refresh-abc"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"jwt-dashboard-token","token_type":"Bearer","access_expires_at":1800000000,"session":{"sid":"sess-1"},"user":{"id":9,"username":"u"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	session, err := c.Login(context.Background(), &connector.Channel{
		SiteURL:  srv.URL,
		Username: "u",
		Password: "p",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.UserID != "9" {
		t.Fatalf("user id = %q, want 9", session.UserID)
	}
	if session.AccessToken != "jwt-dashboard-token" {
		t.Fatalf("access token = %q, want jwt-dashboard-token", session.AccessToken)
	}
	// refresh cookie 不得进入 Cookie 字段，避免被误当成 dashboard 鉴权。
	if session.Cookie != "" {
		t.Fatalf("cookie = %q, want empty", session.Cookie)
	}
	// refresh cookie 的值必须被解析到 RefreshToken，供后续 RefreshSession 使用。
	if session.RefreshToken != "refresh-abc" {
		t.Fatalf("refresh token = %q, want refresh-abc", session.RefreshToken)
	}
	wantExpires := time.Unix(1800000000, 0)
	if !session.ExpiresAt.Equal(wantExpires) {
		t.Fatalf("expires at = %s, want %s", session.ExpiresAt, wantExpires)
	}
}

func TestLoginNewAPIMissingUserFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		// 即使带了 access_token，缺 user.id 也无法构造后续请求头，应失败。
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"jwt-token","access_expires_at":1800000000,"user":{}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	_, err := c.Login(context.Background(), &connector.Channel{
		SiteURL:  srv.URL,
		Username: "u",
		Password: "p",
	})
	if err == nil || !strings.Contains(err.Error(), "missing user id") {
		t.Fatalf("err = %v, want missing user id", err)
	}
}

// TestRefreshSessionPostsRefreshCookie 验证 RefreshSession 走 cookie 路径：
// 请求必须携带 Cookie: new_api_refresh=<refresh>，不带 body，不携带 Authorization。
// 成功响应返回新的 access_token，并通过 Set-Cookie 轮换 refresh token。
func TestRefreshSessionPostsRefreshCookie(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/newapi/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("refresh must not send Authorization, got %q", got)
		}
		if got := r.Header.Get("Origin"); got != "http://"+r.Host {
			t.Fatalf("origin = %q, want %q", got, "http://"+r.Host)
		}
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "new_api_refresh=old-refresh") {
			t.Fatalf("cookie = %q, want new_api_refresh=old-refresh", cookie)
		}
		// 服务器轮换 refresh cookie；新值仅通过 Set-Cookie 下发，不在 JSON body。
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "rotated-refresh"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"new-jwt","token_type":"Bearer","access_expires_at":1900000000,"user":{"id":9}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	old := &connector.AuthSession{
		UserID:       "9",
		AccessToken:  "stale-jwt",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(1 * time.Minute),
	}
	refreshed, err := c.RefreshSession(context.Background(), &connector.Channel{SiteURL: srv.URL + "/newapi/"}, old)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if refreshed.AccessToken != "new-jwt" {
		t.Fatalf("access token = %q, want new-jwt", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "rotated-refresh" {
		t.Fatalf("refresh token = %q, want rotated-refresh", refreshed.RefreshToken)
	}
	if refreshed.UserID != "9" {
		t.Fatalf("user id = %q, want 9", refreshed.UserID)
	}
	if want := time.Unix(1900000000, 0); !refreshed.ExpiresAt.Equal(want) {
		t.Fatalf("expires at = %s, want %s", refreshed.ExpiresAt, want)
	}
	// refresh 后必须清空 Cookie，避免老版 cookie 被当成有效鉴权继续使用。
	if refreshed.Cookie != "" {
		t.Fatalf("cookie = %q, want empty after refresh", refreshed.Cookie)
	}
	// 入参 session 不应被原地修改（防止共享 session 被破坏）。
	if old.AccessToken != "stale-jwt" || old.RefreshToken != "old-refresh" {
		t.Fatalf("input session mutated: %#v", old)
	}
}

func TestRefreshSessionFallbackPolicy(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		allowFallback bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, allowFallback: true},
		{name: "origin forbidden", status: http.StatusForbidden, allowFallback: false},
		{name: "refresh race", status: http.StatusConflict, allowFallback: false},
		{name: "rate limited", status: http.StatusTooManyRequests, allowFallback: false},
		{name: "server error", status: http.StatusInternalServerError, allowFallback: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"success":false,"code":"refresh_failed","message":"refresh failed"}`))
			}))
			defer srv.Close()

			_, err := New().RefreshSession(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
				UserID:       "9",
				RefreshToken: "refresh",
			})
			if err == nil {
				t.Fatal("RefreshSession succeeded, want error")
			}
			if got := connector.ShouldLoginAfterRefreshError(err); got != tt.allowFallback {
				t.Fatalf("ShouldLoginAfterRefreshError() = %v, want %v; err = %v", got, tt.allowFallback, err)
			}
		})
	}
}

func TestRefreshSessionRejectsInvalidSiteURL(t *testing.T) {
	_, err := New().RefreshSession(context.Background(), &connector.Channel{SiteURL: "ftp://example.com/path"}, &connector.AuthSession{
		UserID:       "9",
		RefreshToken: "refresh",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid NewAPI site URL") {
		t.Fatalf("err = %v, want invalid NewAPI site URL", err)
	}
	if connector.ShouldLoginAfterRefreshError(err) {
		t.Fatal("invalid site URL must not permit password login fallback")
	}
}

// TestRefreshSessionMissingRefreshTokenFails 没有 RefreshToken 必须返回错误，
// 让 channel/service.go 退回到重新登录路径（与 sub2api 一致）。
func TestRefreshSessionMissingRefreshTokenFails(t *testing.T) {
	c := New()
	cases := []struct {
		name    string
		session *connector.AuthSession
	}{
		{"nil", nil},
		{"empty refresh", &connector.AuthSession{UserID: "9", AccessToken: "jwt"}},
		{"whitespace refresh", &connector.AuthSession{UserID: "9", RefreshToken: "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.RefreshSession(context.Background(), &connector.Channel{SiteURL: "http://example.invalid"}, tc.session)
			if err == nil || !strings.Contains(err.Error(), "missing refresh token") {
				t.Fatalf("err = %v, want missing refresh token", err)
			}
		})
	}
}

// TestRefreshSessionServerFailure 把 !success 响应翻译成 `newapi refresh: <message>` 错误。
func TestRefreshSessionServerFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"message":"refresh token is invalid","data":null}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	session := &connector.AuthSession{UserID: "9", RefreshToken: "stale"}
	_, err := c.RefreshSession(context.Background(), &connector.Channel{SiteURL: srv.URL}, session)
	if err == nil || !strings.Contains(err.Error(), "newapi refresh:") || !strings.Contains(err.Error(), "refresh token is invalid") {
		t.Fatalf("err = %v, want newapi refresh: ...refresh token is invalid", err)
	}
}

// TestRefreshSessionMissingAccessTokenFails 即使 success=true 但 body 缺 access_token/user.id
// 也必须失败，避免写入无凭据的 session。
func TestRefreshSessionMissingAccessTokenFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "rotated"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"","user":{}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	session := &connector.AuthSession{UserID: "9", RefreshToken: "old"}
	_, err := c.RefreshSession(context.Background(), &connector.Channel{SiteURL: srv.URL}, session)
	if err == nil || !strings.Contains(err.Error(), "missing access_token or user id") {
		t.Fatalf("err = %v, want missing access_token or user id", err)
	}
}

func TestCreateRechargeUsesBearerAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/pay", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer jwt-token" {
			t.Fatalf("authorization = %q, want Bearer jwt-token", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Fatalf("cookie = %q, want empty", got)
		}
		_, _ = w.Write([]byte(`{"message":"success","data":{"pid":"123","type":"alipay"},"url":"https://pay.example.com/submit"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	launch, err := c.CreateRecharge(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		AccessToken: "jwt-token",
		UserID:      "9",
	}, connector.RechargeRequest{
		Amount:        20,
		PaymentMethod: "alipay",
	})
	if err != nil {
		t.Fatalf("CreateRecharge: %v", err)
	}
	if launch.FormFields["pid"] != "123" {
		t.Fatalf("pid = %q", launch.FormFields["pid"])
	}
}

func TestGetCosts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota_per_unit":500000}}`))
	})
	mux.HandleFunc("/api/log/self/stat", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("type"); got != "0" {
			t.Fatalf("type = %q, want 0", got)
		}
		if r.URL.Query().Get("start_timestamp") == "" || r.URL.Query().Get("end_timestamp") == "" {
			t.Fatalf("expected start/end timestamp")
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":1000000,"rpm":0,"tpm":0}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":3416846,"used_quota":39091119}}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	res, err := c.GetCosts(context.Background(), &connector.Channel{
		SiteURL: srv.URL,
	}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	})
	if err != nil {
		t.Fatalf("GetCosts: %v", err)
	}
	if res.TodayCost != 2 {
		t.Fatalf("today cost = %v, want 2", res.TodayCost)
	}
	if res.TotalCost != 78.1822 {
		t.Fatalf("total cost = %v, want 78.1822", res.TotalCost)
	}
}

func TestGetCostsAppliesManualRechargeMultiplier(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota_per_unit":500000}}`))
	})
	mux.HandleFunc("/api/log/self/stat", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":1000000}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"used_quota":5000000}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	multiplier := 2.0
	res, err := c.GetCosts(context.Background(), &connector.Channel{
		SiteURL:                srv.URL,
		RechargeMultiplier:     &multiplier,
		RechargeMultiplierMode: connector.RechargeMultiplierModeDivide,
	}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	})
	if err != nil {
		t.Fatalf("GetCosts: %v", err)
	}
	if res.TodayCost != 1 {
		t.Fatalf("today cost = %v, want 1", res.TodayCost)
	}
	if res.TotalCost != 5 {
		t.Fatalf("total cost = %v, want 5", res.TotalCost)
	}
}

func TestGetCostsAppliesUpstreamRechargeMultiplier(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota_per_unit":500000,"price":7.2}}`))
	})
	mux.HandleFunc("/api/log/self/stat", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":1000000}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"used_quota":5000000}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	res, err := c.GetCosts(context.Background(), &connector.Channel{
		SiteURL:                srv.URL,
		RechargeMultiplierMode: connector.RechargeMultiplierModeDivide,
	}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	})
	if err != nil {
		t.Fatalf("GetCosts: %v", err)
	}
	if res.TodayCost != 14.4 {
		t.Fatalf("today cost = %v, want 14.4", res.TodayCost)
	}
	if res.TotalCost != 72 {
		t.Fatalf("total cost = %v, want 72", res.TotalCost)
	}
}

func TestGetRechargeInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/topup/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"pay_methods":"[{\"name\":\"支付宝\",\"type\":\"alipay\",\"min_topup\":\"10\"},{\"name\":\"微信\",\"type\":\"wxpay\",\"min_topup\":\"12\"},{\"name\":\"Stripe\",\"type\":\"stripe\",\"min_topup\":\"30\"}]","min_topup":8,"amount_options":"[10,20,50]"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	info, err := c.GetRechargeInfo(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	})
	if err != nil {
		t.Fatalf("GetRechargeInfo: %v", err)
	}
	if len(info.Methods) != 2 {
		t.Fatalf("methods len = %d, want 2", len(info.Methods))
	}
	if info.Methods[0].Type != "alipay" || info.Methods[0].MinAmount != 10 {
		t.Fatalf("alipay method = %#v", info.Methods[0])
	}
	if info.AmountStep != 1 {
		t.Fatalf("amount step = %v, want 1", info.AmountStep)
	}
	if len(info.PresetAmounts) != 3 || info.PresetAmounts[2] != 50 {
		t.Fatalf("preset amounts = %#v", info.PresetAmounts)
	}
}

func TestCreateRecharge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/pay", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "session=1" {
			t.Fatalf("cookie = %q", got)
		}
		if got := r.Header.Get("New-Api-User"); got != "7" {
			t.Fatalf("user header = %q", got)
		}
		_, _ = w.Write([]byte(`{"message":"success","data":{"pid":"123","type":"alipay"},"url":"https://pay.example.com/submit"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	launch, err := c.CreateRecharge(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	}, connector.RechargeRequest{
		Amount:        20,
		PaymentMethod: "alipay",
	})
	if err != nil {
		t.Fatalf("CreateRecharge: %v", err)
	}
	if launch.Mode != "form" {
		t.Fatalf("mode = %q, want form", launch.Mode)
	}
	if launch.FormAction != "https://pay.example.com/submit" {
		t.Fatalf("action = %q", launch.FormAction)
	}
	if launch.FormFields["pid"] != "123" {
		t.Fatalf("pid = %q", launch.FormFields["pid"])
	}
}

func TestCreateRechargeReturnsDataError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/pay", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"error","data":"支付方式不存在"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	_, err := c.CreateRecharge(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	}, connector.RechargeRequest{
		Amount:        20,
		PaymentMethod: "alipay",
	})
	if err == nil || !strings.Contains(err.Error(), "支付方式不存在") {
		t.Fatalf("err = %v, want data error", err)
	}
}

func TestCreateRechargeRejectsFloatAmount(t *testing.T) {
	c := New()
	_, err := c.CreateRecharge(context.Background(), &connector.Channel{}, &connector.AuthSession{}, connector.RechargeRequest{
		Amount:        1.5,
		PaymentMethod: "alipay",
	})
	if err == nil || !strings.Contains(err.Error(), "正整数") {
		t.Fatalf("err = %v, want integer error", err)
	}
}

func TestSubscriptionUnsupported(t *testing.T) {
	c := New()
	_, err := c.GetSubscriptionInfo(context.Background(), &connector.Channel{}, &connector.AuthSession{})
	if err == nil || !strings.Contains(err.Error(), "不支持订阅") {
		t.Fatalf("GetSubscriptionInfo err = %v, want unsupported error", err)
	}
	_, err = c.CreateSubscription(context.Background(), &connector.Channel{}, &connector.AuthSession{}, connector.SubscriptionRequest{})
	if err == nil || !strings.Contains(err.Error(), "不支持订阅") {
		t.Fatalf("CreateSubscription err = %v, want unsupported error", err)
	}
	_, err = c.GetSubscriptionUsage(context.Background(), &connector.Channel{}, &connector.AuthSession{})
	if err == nil || !strings.Contains(err.Error(), "不支持订阅") {
		t.Fatalf("GetSubscriptionUsage err = %v, want unsupported error", err)
	}
}

func TestListAPIKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/self/groups", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"default":{"ratio":1.25,"desc":"默认分组"}}}`))
	})
	mux.HandleFunc("/api/token/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("p"); got != "2" {
			t.Fatalf("p = %q, want 2", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "10" {
			t.Fatalf("page_size = %q, want 10", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[{"id":9,"key":"sk**********abcd","status":1,"name":"main","created_time":1700000000,"accessed_time":1700000100,"expired_time":-1,"remain_quota":100,"used_quota":5,"unlimited_quota":false,"model_limits_enabled":true,"model_limits":"gpt-4","allow_ips":"1.1.1.1","group":"default","cross_group_retry":true}],"total":1,"page":2,"page_size":10,"pages":1}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	page, err := c.ListAPIKeys(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	}, connector.APIKeyQuery{Page: 2, PageSize: 10})
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != "active" || page.Items[0].AllowIPs != "1.1.1.1" || page.Items[0].GroupName != "default" || page.Items[0].GroupRatio != 1.25 {
		t.Fatalf("page = %#v", page)
	}
}

func TestListAPIKeyGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/self/groups", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"default":{"ratio":1.25,"desc":"默认分组"},"auto":{"ratio":"自动","desc":"跳过"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	groups, err := c.ListAPIKeyGroups(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	})
	if err != nil {
		t.Fatalf("ListAPIKeyGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "default" || groups[0].Description != "默认分组" || groups[0].Ratio != 1.25 {
		t.Fatalf("groups = %#v", groups)
	}
}

func TestGetAnnouncements(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"announcements":[{"id":12,"title":"平台公告","content":"维护通知","type":"warning","publishDate":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T04:04:05Z"}]}}`))
	})
	mux.HandleFunc("/api/notice", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":"站点公告"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	items, err := c.GetAnnouncements(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{})
	if err != nil {
		t.Fatalf("GetAnnouncements: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if items[0].SourceKey != "id:12" || items[0].Title != "平台公告" || items[0].Type != "warning" {
		t.Fatalf("first item = %#v", items[0])
	}
	if !strings.HasPrefix(items[1].SourceKey, "hash:") || items[1].Content != "站点公告" {
		t.Fatalf("second item = %#v", items[1])
	}
}

func TestGetAnnouncementsFromNoticeOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"announcements":[]}}`))
	})
	mux.HandleFunc("/api/notice", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":"文本公告"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	items, err := c.GetAnnouncements(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{})
	if err != nil {
		t.Fatalf("GetAnnouncements: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if !strings.HasPrefix(items[0].SourceKey, "hash:") || items[0].Content != "文本公告" {
		t.Fatalf("item = %#v", items[0])
	}
}

func TestGetAnnouncementsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"announcements":[]}}`))
	})
	mux.HandleFunc("/api/notice", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":""}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	items, err := c.GetAnnouncements(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{})
	if err != nil {
		t.Fatalf("GetAnnouncements: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items len = %d, want 0", len(items))
	}
}

func TestSearchAPIKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("keyword"); got != "main" {
			t.Fatalf("keyword = %q, want main", got)
		}
		if got := r.URL.Query().Get("token"); got != "main" {
			t.Fatalf("token = %q, want main", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[],"total":0,"page":1,"page_size":20,"pages":1}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	_, err := c.ListAPIKeys(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	}, connector.APIKeyQuery{Search: "main"})
	if err != nil {
		t.Fatalf("ListAPIKeys search: %v", err)
	}
}

func TestCreateUpdateDeleteRevealAPIKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[],"total":0,"page":1,"page_size":100,"pages":0}}`))
		case http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			if body["name"] != "main" || body["custom_key"] != "sk-custom" {
				t.Fatalf("create body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update: %v", err)
			}
			if body["id"] != float64(9) || body["status"] != float64(2) {
				t.Fatalf("update body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":9,"status":2,"name":"main disabled"}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})
	mux.HandleFunc("/api/token/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Query().Get("keyword"); got != "main" {
			t.Fatalf("keyword = %q, want main", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[{"id":9,"status":1,"name":"main","expired_time":-1,"group":"default"}],"total":1,"page":1,"page_size":20,"pages":1}}`))
	})
	mux.HandleFunc("/api/token/9", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", r.Method)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":""}`))
	})
	mux.HandleFunc("/api/token/9/key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"key":"sk-full"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	session := &connector.AuthSession{Cookie: "session=1", UserID: "7"}
	created, err := c.CreateAPIKey(context.Background(), &connector.Channel{SiteURL: srv.URL}, session, connector.APIKeyCreateRequest{
		Name:      "main",
		CustomKey: "sk-custom",
	})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if created.ID != 9 {
		t.Fatalf("created id = %d, want 9", created.ID)
	}
	updated, err := c.UpdateAPIKey(context.Background(), &connector.Channel{SiteURL: srv.URL}, session, 9, connector.APIKeyUpdateRequest{
		Status: strPtr("disabled"),
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey: %v", err)
	}
	if updated.Status != "disabled" {
		t.Fatalf("updated status = %q", updated.Status)
	}
	if err := c.DeleteAPIKey(context.Background(), &connector.Channel{SiteURL: srv.URL}, session, 9); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
	key, err := c.RevealAPIKey(context.Background(), &connector.Channel{SiteURL: srv.URL}, session, 9)
	if err != nil {
		t.Fatalf("RevealAPIKey: %v", err)
	}
	if key != "sk-full" {
		t.Fatalf("key = %q", key)
	}
}

func TestBuildNewAPIUpdateTokenGroupOnly(t *testing.T) {
	group := "pro"
	body := buildNewAPIUpdateToken(9, connector.APIKeyUpdateRequest{Group: &group})
	if body["id"] != 9 || body["group"] != group {
		t.Fatalf("group update body = %#v", body)
	}
	if len(body) != 2 {
		t.Fatalf("group update contains unrelated fields: %#v", body)
	}
}

func TestCreateAPIKeyFallsBackToListWhenSearchMisses(t *testing.T) {
	mux := http.NewServeMux()
	listCalls := 0
	mux.HandleFunc("/api/user/self/groups", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"default":{"ratio":1,"desc":"default"}}}`))
	})
	mux.HandleFunc("/api/token/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		case http.MethodGet:
			listCalls++
			if listCalls == 1 {
				_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[{"id":18,"status":1,"name":"old","expired_time":-1,"group":"default"}],"total":1,"page":1,"page_size":100,"pages":1}}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[{"id":19,"status":1,"name":"server-renamed","expired_time":-1,"group":"default"},{"id":18,"status":1,"name":"old","expired_time":-1,"group":"default"}],"total":2,"page":1,"page_size":100,"pages":1}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})
	mux.HandleFunc("/api/token/search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"items":[],"total":0,"page":1,"page_size":20,"pages":0}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	created, err := New().CreateAPIKey(context.Background(), &connector.Channel{SiteURL: srv.URL}, &connector.AuthSession{
		Cookie: "session=1",
		UserID: "7",
	}, connector.APIKeyCreateRequest{Name: "main"})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if created.ID != 19 {
		t.Fatalf("created id = %d, want 19", created.ID)
	}
}

func strPtr(v string) *string {
	return &v
}
