// 上游模型目录服务层测试：缓存、降级与「上游不支持」三条路径。
package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

const testPricingBody = `{
  "success": true,
  "data": [
    {"model_name":"m1","model_ratio":2,"completion_ratio":5,"enable_groups":["g1"]},
    {"model_name":"m2","model_ratio":1,"completion_ratio":2,"enable_groups":["g1","g2"]}
  ],
  "usable_group": {"g1":"组一","g3":"组三"},
  "group_ratio": {"g1":1.5,"g2":0.8,"g3":2}
}`

// newAPIAuthStub 为 token 模式的渠道提供 CheckAuth 所需的 /api/user/self。
const newAPIAuthStub = `{"success":true,"data":{"id":1,"username":"u"}}`

type pricingServerOpts struct {
	// pricingStatus 非 200 时模拟上游失败
	pricingStatus int
	pricingBody   string
	// failAuth=true 时 /api/user/self 返回 401，模拟 token 失效
	failAuth bool
}

func newPricingServer(t *testing.T, opts pricingServerOpts) (*httptest.Server, *int32) {
	t.Helper()
	var pricingHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			if opts.failAuth {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"AUTH_TOKEN_EXPIRED"}`))
				return
			}
			_, _ = w.Write([]byte(newAPIAuthStub))
		case "/api/pricing":
			atomic.AddInt32(&pricingHits, 1)
			if opts.pricingStatus != 0 && opts.pricingStatus != http.StatusOK {
				w.WriteHeader(opts.pricingStatus)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			body := opts.pricingBody
			if body == "" {
				body = testPricingBody
			}
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &pricingHits
}

// newCatalogTestChannel 建一个 token 模式的新 API 渠道，指向测试服务器。
func newCatalogTestChannel(t *testing.T, svc *Service, siteURL string) *storage.Channel {
	t.Helper()
	enc, err := svc.Cipher.Encrypt(`{"access_token":"tok","user_id":"1"}`)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	ch := &storage.Channel{
		Name:           "catalog-test",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        siteURL,
		Username:       "u",
		PasswordCipher: enc,
		CredentialMode: storage.CredentialModeToken,
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

func TestGetUpstreamCatalogReturnsGroupModelMapping(t *testing.T) {
	svc, _ := testService(t)
	srv, _ := newPricingServer(t, pricingServerOpts{})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	cat, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, true)
	if err != nil {
		t.Fatalf("GetUpstreamCatalog: %v", err)
	}
	if cat.Degrade != connector.CatalogDegradeNone {
		t.Fatalf("degrade = %q (%s)", cat.Degrade, cat.DegradeMessage)
	}
	if len(cat.Models) != 2 {
		t.Fatalf("models = %d", len(cat.Models))
	}

	byName := map[string]connector.CatalogGroup{}
	for _, g := range cat.Groups {
		byName[g.Name] = g
	}
	// g1/g2 被模型引用，g3 仅授权无模型 → 三个分组都应在。
	if len(byName) != 3 {
		t.Fatalf("groups = %v", byName)
	}
	// g2 被引用且上游给了倍率，但不在 usable_group 里 → 未授权。
	if g := byName["g2"]; g.Authorized {
		t.Errorf("g2 should be unauthorized: %+v", g)
	}
	if g := byName["g1"]; !g.Authorized || g.ModelCount != 2 || g.Ratio != 1.5 {
		t.Errorf("g1 = %+v", g)
	}
	if g := byName["g3"]; g.ModelCount != 0 || !g.Authorized {
		t.Errorf("g3 = %+v", g)
	}
	if len(cat.Notes) == 0 {
		t.Error("unauthorized group should produce a note")
	}
}

func TestGetUpstreamCatalogCachesUntilFresh(t *testing.T) {
	svc, _ := testService(t)
	srv, hits := newPricingServer(t, pricingServerOpts{})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	for i := 0; i < 3; i++ {
		if _, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, false); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	first := atomic.LoadInt32(hits)
	if first != 1 {
		t.Fatalf("expected exactly 1 upstream pricing hit for 3 cached calls, got %d", first)
	}

	// fresh=true 必须绕过缓存。
	if _, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, true); err != nil {
		t.Fatalf("fresh call: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != first+1 {
		t.Fatalf("fresh should hit upstream once more: before=%d after=%d", first, got)
	}
}

func TestGetUpstreamCatalogDegradesOnUpstreamError(t *testing.T) {
	svc, _ := testService(t)
	srv, _ := newPricingServer(t, pricingServerOpts{pricingStatus: http.StatusInternalServerError})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	cat, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, true)
	if err == nil {
		t.Fatal("expected error from upstream failure")
	}
	if cat == nil {
		t.Fatal("catalog should be non-nil even on failure so UI can explain")
	}
	if cat.Degrade != connector.CatalogDegradeFetchFailed {
		t.Fatalf("degrade = %q", cat.Degrade)
	}
	if cat.DegradeMessage == "" {
		t.Fatal("degrade message should be filled")
	}
}

func TestGetUpstreamCatalogDegradesOnExpiredToken(t *testing.T) {
	svc, _ := testService(t)
	srv, _ := newPricingServer(t, pricingServerOpts{failAuth: true})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	cat, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, true)
	if err == nil {
		t.Fatal("expected auth error")
	}
	if cat == nil || cat.Degrade != connector.CatalogDegradeAuthExpired {
		t.Fatalf("catalog = %+v", cat)
	}
	// 提示要能引导用户去重新登录，而不是只抛「status 401」。
	if !strings.Contains(cat.DegradeMessage, "token") && !strings.Contains(cat.DegradeMessage, "凭据") {
		t.Fatalf("degrade message should explain re-login: %q", cat.DegradeMessage)
	}
}

func TestGetUpstreamCatalogUnknownChannel(t *testing.T) {
	svc, _ := testService(t)
	cat, err := svc.GetUpstreamCatalog(context.Background(), 99999, true)
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
	if cat == nil || cat.Degrade == connector.CatalogDegradeNone {
		t.Fatalf("catalog = %+v", cat)
	}
}

func TestInvalidateUpstreamCatalogClearsCache(t *testing.T) {
	svc, _ := testService(t)
	srv, hits := newPricingServer(t, pricingServerOpts{})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	if _, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	before := atomic.LoadInt32(hits)

	svc.InvalidateUpstreamCatalog(ch.ID)

	if _, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, false); err != nil {
		t.Fatalf("after invalidate: %v", err)
	}
	if got := atomic.LoadInt32(hits); got <= before {
		t.Fatalf("cache should be cleared: before=%d after=%d", before, got)
	}
}

func TestUpstreamCatalogJSONIsUIReady(t *testing.T) {
	svc, _ := testService(t)
	srv, _ := newPricingServer(t, pricingServerOpts{})
	ch := newCatalogTestChannel(t, svc, srv.URL)

	cat, err := svc.GetUpstreamCatalog(context.Background(), ch.ID, true)
	if err != nil {
		t.Fatalf("GetUpstreamCatalog: %v", err)
	}
	raw, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 前端依赖这几个键名，改字段时这里会先炸。
	for _, key := range []string{`"groups"`, `"models"`, `"authorized"`, `"has_ratio"`, `"model_ratio"`, `"degrade"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("marshalled catalog missing %s", key)
		}
	}
}

func TestGetUpstreamCatalogSub2APIDegradesExplicitly(t *testing.T) {
	svc, _ := testService(t)
	// Sub2API 需要鉴权接口，这里只要它返回「不支持」而不是 panic 即可。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	enc, err := svc.Cipher.Encrypt(`{"access_token":"tok","refresh_token":"r"}`)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ch := &storage.Channel{
		Name:           "sub2-test",
		Type:           storage.ChannelTypeSub2API,
		SiteURL:        srv.URL,
		Username:       "u",
		PasswordCipher: enc,
		CredentialMode: storage.CredentialModeToken,
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create: %v", err)
	}

	cat, _ := svc.GetUpstreamCatalog(context.Background(), ch.ID, true)
	if cat == nil {
		t.Fatal("catalog should be non-nil")
	}
	// 无法登录时应是 auth_expired；能登录则应是 no_pricing_api。两者都是明确降级。
	if cat.Degrade == connector.CatalogDegradeNone {
		t.Fatalf("Sub2API should never report a healthy catalog: %+v", cat)
	}
	if cat.Capability != connector.CatalogCapabilityPlatformPool &&
		cat.Capability != connector.CatalogCapabilityGroupModels {
		t.Fatalf("unexpected capability %q", cat.Capability)
	}
}
