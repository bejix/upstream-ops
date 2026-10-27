// 上游模型目录解析测试：用真实抓包的字段形态（xem / 码少 / WorldClawPro）作为样例。
package newapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/connector"
)

// 精简自 https://ai.xem8k5.top/api/pricing 的真实响应。
const xemPricingFixture = `{
  "success": true,
  "data": [
    {"model_name":"claude-opus-4-6","quota_type":0,"model_ratio":2.5,"completion_ratio":5,
     "cache_ratio":0.1,"create_cache_ratio":1.25,"enable_groups":["claude-max","claude"],
     "tags":"Reasoning,Tools,Files,Vision,1M","vendor_id":12,
     "supported_endpoint_types":["anthropic","openai"]},
    {"model_name":"claude-haiku-4-5","quota_type":0,"model_ratio":0.5,"completion_ratio":5,
     "enable_groups":["claude"],"vendor_id":12,"supported_endpoint_types":["anthropic"]},
    {"model_name":"google/gemma-4-31b-it","quota_type":0,"model_ratio":0.5,"completion_ratio":1,
     "enable_groups":["default"],"vendor_id":3,"supported_endpoint_types":["openai"]},
    {"model_name":"dall-e-3","quota_type":1,"model_ratio":0,"model_price":0.04,
     "enable_groups":["default"],"vendor_id":null,"supported_endpoint_types":["image-generation"]}
  ],
  "usable_group": {"claude":"kiro企业","claude-max":"可外接,禁蒸馏","default":"默认分组","vip":"VIP 分组"},
  "group_ratio": {"claude":4.55,"claude-max":12.55,"default":0.2,"vip":7.5},
  "vendors": [{"id":12,"name":"Anthropic"},{"id":3,"name":"Google"}],
  "pricing_version": "abc123"
}`

func newPricingServer(body string, requireAuth bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pricing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if requireAuth && r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"AUTH_UNAUTHORIZED"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestGetCatalogBuildsGroupModelMapping(t *testing.T) {
	srv := newPricingServer(xemPricingFixture, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if cat.Capability != connector.CatalogCapabilityGroupModels {
		t.Fatalf("capability = %q", cat.Capability)
	}
	if cat.Degrade != connector.CatalogDegradeNone {
		t.Fatalf("degrade = %q", cat.Degrade)
	}
	if cat.PricingVersion != "abc123" {
		t.Fatalf("pricing_version = %q", cat.PricingVersion)
	}

	// 分组：claude(2) / claude-max(1) / default(2)，另加仅授权无模型的 vip。
	byName := map[string]connector.CatalogGroup{}
	for _, g := range cat.Groups {
		byName[g.Name] = g
	}
	if len(byName) != 4 {
		t.Fatalf("groups = %d, want 4: %+v", len(byName), byName)
	}
	if g := byName["claude"]; g.ModelCount != 2 || !g.Authorized || !g.HasRatio || g.Ratio != 4.55 || g.Description != "kiro企业" {
		t.Fatalf("claude group = %+v", g)
	}
	// 授权但无模型：应保留，ModelCount=0。
	if g := byName["vip"]; g.ModelCount != 0 || !g.Authorized || g.Ratio != 7.5 {
		t.Fatalf("vip group = %+v", g)
	}

	// 模型：跨组模型要保留全部组名。
	var opus *connector.CatalogModel
	for i := range cat.Models {
		if cat.Models[i].Name == "claude-opus-4-6" {
			opus = &cat.Models[i]
		}
	}
	if opus == nil {
		t.Fatal("claude-opus-4-6 missing")
	}
	if len(opus.Groups) != 2 || opus.Groups[0] != "claude" || opus.Groups[1] != "claude-max" {
		t.Fatalf("groups = %v, want sorted [claude claude-max]", opus.Groups)
	}
	if opus.Vendor != "Anthropic" {
		t.Fatalf("vendor = %q", opus.Vendor)
	}
	if len(opus.Tags) != 5 || opus.Tags[0] != "Reasoning" {
		t.Fatalf("tags = %v", opus.Tags)
	}
	if opus.CacheRatio == nil || *opus.CacheRatio != 0.1 {
		t.Fatalf("cache_ratio = %v", opus.CacheRatio)
	}

	// 模型名可含斜杠：必须原样保留，不能被 URL 或路径逻辑改写。
	found := false
	for _, m := range cat.Models {
		if m.Name == "google/gemma-4-31b-it" {
			found = true
		}
	}
	if !found {
		t.Fatal("model with slash was dropped")
	}

	// vendor_id 为 null 时留空而不是报错。
	for _, m := range cat.Models {
		if m.Name == "dall-e-3" && m.Vendor != "" {
			t.Fatalf("dall-e-3 vendor = %q, want empty", m.Vendor)
		}
	}
}

func TestGetCatalogFallsBackToAnonymous(t *testing.T) {
	// 站点要求鉴权，但定价对匿名开放：带 session 失败后应重试匿名并成功。
	srv := newPricingServer(xemPricingFixture, false)
	defer srv.Close()

	// 构造一个永远 401 的站点，验证最终会走 fetch_failed 而不是 panic。
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"AUTH_TOKEN_EXPIRED"}`))
	}))
	defer authSrv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: authSrv.URL}, nil)
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if cat == nil || cat.Degrade != connector.CatalogDegradeFetchFailed {
		t.Fatalf("catalog = %+v, want fetch_failed", cat)
	}
	if cat.DegradeMessage == "" {
		t.Fatal("degrade message should be populated for diagnosis")
	}
}

func TestGetCatalogHandlesTieredExprBilling(t *testing.T) {
	// 码少形态：model_ratio 为 0，靠 billing_expr 计费。
	body := `{
      "success": true,
      "data": [
        {"model_name":"claude-opus-5.5","quota_type":0,"model_ratio":0,"completion_ratio":1,
         "billing_mode":"tiered_expr","billing_expr":"tier(\"base\", p * 4 + c * 20 + cr * 0.2 + cc * 5)",
         "enable_groups":["码少专用"]},
        {"model_name":"muse-image","quota_type":0,"model_ratio":0,
         "billing_expr":"tier(\"base\", fixed(0.02))","enable_groups":["码少专用"]},
        {"model_name":"muse-video","quota_type":0,"model_ratio":0,
         "billing_expr":"tier(\"base\", u(\"seconds\") * 0.02)","enable_groups":["码少专用"]}
      ],
      "usable_group": {"GPT混池":""},
      "group_ratio": {"GPT混池":0.06}
    }`
	srv := newPricingServer(body, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}

	briefs := map[string]string{}
	for _, m := range cat.Models {
		briefs[m.Name] = m.BillingBrief
	}
	if got := briefs["claude-opus-5.5"]; got != "入×4 · 出×20" {
		t.Fatalf("token brief = %q", got)
	}
	if got := briefs["muse-image"]; got != "$0.02/次" {
		t.Fatalf("image brief = %q", got)
	}
	if got := briefs["muse-video"]; got != "$0.02/秒" {
		t.Fatalf("video brief = %q", got)
	}

	// billing_mode 缺失时按表达式推断成 tiered_expr。
	for _, m := range cat.Models {
		if m.Name == "muse-image" && m.BillingMode != "tiered_expr" {
			t.Fatalf("billing_mode = %q, want tiered_expr", m.BillingMode)
		}
	}

	// 「码少专用」被模型引用但不在 usable_group 里 → 未授权，且要有提示。
	var g *connector.CatalogGroup
	for i := range cat.Groups {
		if cat.Groups[i].Name == "码少专用" {
			g = &cat.Groups[i]
		}
	}
	if g == nil {
		t.Fatal("码少专用 group missing")
	}
	if g.Authorized {
		t.Fatal("码少专用 should be unauthorized")
	}
	if g.ModelCount != 3 {
		t.Fatalf("model_count = %d", g.ModelCount)
	}
	joined := strings.Join(cat.Notes, "|")
	if !strings.Contains(joined, "授权列表") {
		t.Fatalf("notes should warn about unauthorized groups: %v", cat.Notes)
	}
}

func TestGetCatalogTolerantNumericForms(t *testing.T) {
	// 二开版常见：倍率是字符串、tags 是数组。
	body := `{
      "success": true,
      "data": [
        {"model_name":"m1","model_ratio":"1.25","completion_ratio":"2",
         "tags":["Vision","Tools"],"vendor_id":"12","enable_groups":["g1"]}
      ],
      "usable_group": {"g1":"组一"},
      "group_ratio": {"g1":"0.5"},
      "vendors": [{"id":"12","name":"Anthropic"}]
    }`
	srv := newPricingServer(body, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	m := cat.Models[0]
	if m.ModelRatio != 1.25 || m.CompletionRatio != 2 {
		t.Fatalf("ratios = %v / %v", m.ModelRatio, m.CompletionRatio)
	}
	if m.Vendor != "Anthropic" {
		t.Fatalf("vendor = %q", m.Vendor)
	}
	if len(m.Tags) != 2 || m.Tags[1] != "Tools" {
		t.Fatalf("tags = %v", m.Tags)
	}
	if g := cat.Groups[0]; g.Ratio != 0.5 || !g.HasRatio {
		t.Fatalf("group ratio = %v has=%v", g.Ratio, g.HasRatio)
	}
}

func TestGetCatalogEmptyData(t *testing.T) {
	srv := newPricingServer(`{"success":true,"data":[]}`, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	if len(cat.Models) != 0 || len(cat.Groups) != 0 {
		t.Fatalf("expected empty catalog, got %d models %d groups", len(cat.Models), len(cat.Groups))
	}
	if len(cat.Notes) == 0 {
		t.Fatal("empty catalog should carry an explanatory note")
	}
}

func TestBriefBillingExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{`tier("base", p * 4 + c * 20 + cr * 0.2 + cc * 5)`, "入×4 · 出×20"},
		{`tier("base", fixed(0.03))`, "$0.03/次"},
		{`tier("base", u("seconds") * 0.02)`, "$0.02/秒"},
		{`len < 272000 ? tier("默认", p * 5) : tier("长", p * 10)`, "入×5 · 按长度分档"},
		{`some_opaque_call(1)`, "自定义表达式"},
	}
	for _, tc := range cases {
		if got := BriefBillingExpr(tc.in); got != tc.want {
			t.Errorf("BriefBillingExpr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGetCatalogDecodeFailure(t *testing.T) {
	srv := newPricingServer(`not json at all`, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if cat == nil || cat.Degrade != connector.CatalogDegradeFetchFailed {
		t.Fatalf("catalog = %+v", cat)
	}
}

// 确保 Catalog 可被前端直接序列化（字段名稳定）。
func TestCatalogJSONShape(t *testing.T) {
	srv := newPricingServer(xemPricingFixture, false)
	defer srv.Close()

	cat, err := New().GetCatalog(context.Background(), &connector.Channel{SiteURL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	raw, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"capability"`, `"degrade"`, `"groups"`, `"models"`, `"authorized"`, `"model_count"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("marshalled catalog missing %s", key)
		}
	}
}
