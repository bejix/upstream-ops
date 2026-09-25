package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

type channelListPageOut struct {
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

func (p channelListPageOut) names() []string {
	names := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		names = append(names, item.Name)
	}
	return names
}

// newChannelListFilterRouter 写入 5 个渠道（默认顺序即下面的顺序）：
//
//	Alpha   healthy  标签 vip   排序 5
//	bravo   low      暂停       排序 3
//	Charlie failed   标签 vipx  排序 1
//	delta   idle                排序 1
//	echo    healthy  标签 VIP   排序 1
func newChannelListFilterRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	f := func(v float64) *float64 { return &v }
	items := []storage.Channel{
		{Name: "Alpha", Username: "alice", SortOrder: 5, LastBalance: f(100), BalanceThreshold: 10, Tags: storage.ChannelTags{"vip"}},
		{Name: "bravo", Username: "bob", SortOrder: 3, LastBalance: f(5), BalanceThreshold: 10},
		{Name: "Charlie", Username: "carol", SortOrder: 1, LastBalance: f(80), LastError: "登录失败", Tags: storage.ChannelTags{"vipx"}},
		{Name: "delta", Username: "dave", SortOrder: 1},
		{Name: "echo", Username: "eve", SortOrder: 1, LastBalance: f(50), Tags: storage.ChannelTags{"VIP"}},
	}
	for i := range items {
		c := &items[i]
		c.Type = storage.ChannelTypeNewAPI
		c.SiteURL = "https://" + strings.ToLower(c.Name) + ".example.com"
		c.PasswordCipher = "x"
		c.MonitorEnabled = true
		if err := channels.Create(c); err != nil {
			t.Fatalf("create channel %s: %v", c.Name, err)
		}
	}
	// monitor_enabled 带 default:true，Create 时写不进 false，单独 Save 一次。
	items[1].MonitorEnabled = false
	if err := channels.Update(&items[1]); err != nil {
		t.Fatalf("pause bravo: %v", err)
	}

	r := gin.New()
	registerChannels(r.Group("/api"), &Deps{Channels: channels})
	return r
}

func getChannelListPage(t *testing.T, r *gin.Engine, params url.Values) channelListPageOut {
	t.Helper()
	var resp struct {
		Data channelListPageOut `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels?"+params.Encode(), &resp)
	return resp.Data
}

func TestChannelsPageSearchFilterSort(t *testing.T) {
	r := newChannelListFilterRouter(t)

	cases := []struct {
		name      string
		params    url.Values
		want      []string
		wantTotal int64
		wantPages int
	}{
		{
			name:      "no filter keeps default order",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}},
			want:      []string{"Alpha", "bravo", "Charlie", "delta", "echo"},
			wantTotal: 5, wantPages: 1,
		},
		{
			name:      "status paginated total after filter",
			params:    url.Values{"page": {"1"}, "page_size": {"1"}, "status": {"healthy"}},
			want:      []string{"Alpha"},
			wantTotal: 2, wantPages: 2,
		},
		{
			name:      "status page 2",
			params:    url.Values{"page": {"2"}, "page_size": {"1"}, "status": {"healthy"}},
			want:      []string{"echo"},
			wantTotal: 2, wantPages: 2,
		},
		{
			name:      "paused",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "status": {"paused"}},
			want:      []string{"bravo"},
			wantTotal: 1, wantPages: 1,
		},
		{
			name:      "query trims and ignores case",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "q": {"  ECHO "}},
			want:      []string{"echo"},
			wantTotal: 1, wantPages: 1,
		},
		{
			// 名称 bravo 与站点地址都不含 bob，只有账号能命中。
			name:      "query matches username only",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "q": {"BOB"}},
			want:      []string{"bravo"},
			wantTotal: 1, wantPages: 1,
		},
		{
			name:      "query percent is literal",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "q": {"%"}},
			want:      []string{},
			wantTotal: 0, wantPages: 1,
		},
		{
			name:      "tag exact match ignores vipx",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "tag": {"vip"}},
			want:      []string{"Alpha", "echo"},
			wantTotal: 2, wantPages: 1,
		},
		{
			name:      "tag with sort desc and all page size",
			params:    url.Values{"page": {"1"}, "page_size": {"-1"}, "tag": {"vip"}, "sort": {"name"}, "order": {"desc"}},
			want:      []string{"echo", "Alpha"},
			wantTotal: 2, wantPages: 1,
		},
		{
			name:      "health sort anomalies first",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "sort": {"health"}},
			want:      []string{"Charlie", "bravo", "delta", "Alpha", "echo"},
			wantTotal: 5, wantPages: 1,
		},
		{
			name:      "balance desc nulls last",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "sort": {"balance"}, "order": {"desc"}},
			want:      []string{"Alpha", "Charlie", "echo", "bravo", "delta"},
			wantTotal: 5, wantPages: 1,
		},
		{
			name:      "empty filter values are ignored",
			params:    url.Values{"page": {"1"}, "page_size": {"20"}, "q": {""}, "status": {""}, "tag": {""}, "sort": {""}, "order": {""}},
			want:      []string{"Alpha", "bravo", "Charlie", "delta", "echo"},
			wantTotal: 5, wantPages: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getChannelListPage(t, r, tc.params)
			if !reflect.DeepEqual(got.names(), tc.want) {
				t.Fatalf("items = %q, want %q", got.names(), tc.want)
			}
			if got.Total != tc.wantTotal || got.Pages != tc.wantPages {
				t.Fatalf("total/pages = %d/%d, want %d/%d", got.Total, got.Pages, tc.wantTotal, tc.wantPages)
			}
		})
	}
}

func TestChannelsUnpagedListIgnoresFilters(t *testing.T) {
	r := newChannelListFilterRouter(t)

	// 不带 page / page_size 的全量列表（useChannels）保持原样，不受筛选参数影响。
	var resp struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels?q=nomatch&status=failed&sort=bogus", &resp)
	if len(resp.Data) != 5 || resp.Data[0].Name != "Alpha" {
		t.Fatalf("unpaged list = %#v, want all 5 channels in default order", resp.Data)
	}
}

func TestChannelsPageRejectsInvalidFilter(t *testing.T) {
	r := newChannelListFilterRouter(t)

	cases := []struct {
		param, value, wantMsg string
	}{
		{"status", "broken", "status 仅支持"},
		{"status", "all", "status 仅支持"},
		{"sort", "sort_order", "sort 仅支持"},
		{"sort", "name desc", "sort 仅支持"},
		{"order", "up", "order 仅支持"},
		{"tag", "a,b", "tag 只能是单个标签"},
		// 超长关键词在 SQLite 上会让 LIKE 报错（pattern too complex），必须在校验阶段返回 400。
		{"q", strings.Repeat("a", 50001), "搜索关键词最多 200 个字符"},
		{"q", strings.Repeat("中", 201), "搜索关键词最多 200 个字符"},
		{"tag", strings.Repeat("a", 33), "tag 最多 32 个字符"},
	}
	for _, tc := range cases {
		params := url.Values{"page": {"1"}, "page_size": {"9"}, tc.param: {tc.value}}
		req := httptest.NewRequest(http.MethodGet, "/api/channels?"+params.Encode(), nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		label := tc.value
		if len(label) > 40 {
			label = label[:40] + "..."
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s=%q status = %d, want 400, body = %.200s", tc.param, label, rec.Code, rec.Body.String())
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s=%q decode: %v", tc.param, label, err)
		}
		if !strings.Contains(body.Error, tc.wantMsg) {
			t.Fatalf("%s=%q error = %q, want containing %q", tc.param, label, body.Error, tc.wantMsg)
		}
	}
}
