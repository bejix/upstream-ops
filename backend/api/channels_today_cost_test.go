package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type todayCostChannelOut struct {
	ID        uint     `json:"id"`
	Name      string   `json:"name"`
	TodayCost *float64 `json:"today_cost"`
}

// seedTodayCostChannels 固定"当前时间"为北京时间 6/20 00:30，并用 UTC（仍是 6/19 16:30）表示，
// 模拟未设置 TZ 的服务器进程：跨天仍须按北京时间判断。写入三类渠道：
//   - stale:  采样于前一天 23:30，应按 0 输出
//   - fresh:  采样于当天 00:10，原样输出
//   - legacy: 升级前的旧数据，无 TodayCostAt；再次 AutoMigrate 时按 LastBalanceAt（两天前）回填，应按 0 输出
func seedTodayCostChannels(t *testing.T) (*gorm.DB, *storage.Channels) {
	t.Helper()
	loc := time.FixedZone("UTC+8", 8*60*60)
	oldNow := costNow
	costNow = func() time.Time { return time.Date(2026, 6, 20, 0, 30, 0, 0, loc).UTC() }
	t.Cleanup(func() { costNow = oldNow })

	db := openTestDB(t)
	channels := storage.NewChannels(db)
	ts := func(day, hour, minute int) *time.Time {
		v := time.Date(2026, 6, day, hour, minute, 0, 0, loc)
		return &v
	}
	f := func(v float64) *float64 { return &v }
	items := []storage.Channel{
		{Name: "stale", TodayCost: f(2.5), TotalCost: f(10), TodayCostAt: ts(19, 23, 30), LastBalanceAt: ts(19, 23, 30)},
		{Name: "fresh", TodayCost: f(1.25), TotalCost: f(20), TodayCostAt: ts(20, 0, 10), LastBalanceAt: ts(20, 0, 10)},
		{Name: "legacy", TodayCost: f(4), TotalCost: f(30), LastBalanceAt: ts(18, 12, 0)},
	}
	for i := range items {
		items[i].Type = storage.ChannelTypeNewAPI
		items[i].SiteURL = "https://" + items[i].Name + ".example.com"
		items[i].Username = "u"
		items[i].PasswordCipher = "x"
		if err := channels.Create(&items[i]); err != nil {
			t.Fatalf("create channel %s: %v", items[i].Name, err)
		}
	}
	// 模拟升级后重启：迁移时给 legacy 回填 today_cost_at。
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db, channels
}

func assertTodayCosts(t *testing.T, label string, list []todayCostChannelOut) {
	t.Helper()
	want := map[string]float64{"stale": 0, "fresh": 1.25, "legacy": 0}
	if len(list) != len(want) {
		t.Fatalf("%s: channels len = %d, want %d", label, len(list), len(want))
	}
	for _, item := range list {
		w, ok := want[item.Name]
		if !ok {
			t.Fatalf("%s: unexpected channel %q", label, item.Name)
		}
		if item.TodayCost == nil {
			t.Fatalf("%s: channel %q today_cost missing, want %v", label, item.Name, w)
		}
		if *item.TodayCost != w {
			t.Fatalf("%s: channel %q today_cost = %v, want %v", label, item.Name, *item.TodayCost, w)
		}
	}
}

func getTodayCostJSON(t *testing.T, r *gin.Engine, path string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
}

func TestChannelsResetStaleTodayCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, channels := seedTodayCostChannels(t)

	r := gin.New()
	registerChannels(r.Group("/api"), &Deps{Channels: channels})

	var list struct {
		Data []todayCostChannelOut `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels", &list)
	assertTodayCosts(t, "list", list.Data)

	var page struct {
		Data struct {
			Items []todayCostChannelOut `json:"items"`
		} `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels?page=1&page_size=20", &page)
	assertTodayCosts(t, "page", page.Data.Items)

	var staleID, legacyID uint
	for _, item := range list.Data {
		switch item.Name {
		case "stale":
			staleID = item.ID
		case "legacy":
			legacyID = item.ID
		}
	}
	var single struct {
		Data todayCostChannelOut `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels/"+strconv.FormatUint(uint64(staleID), 10), &single)
	if single.Data.TodayCost == nil || *single.Data.TodayCost != 0 {
		t.Fatalf("get stale today_cost = %v, want 0", single.Data.TodayCost)
	}

	// 归零只发生在输出层，数据库里的原始值保持不变。
	stored, err := channels.FindByID(staleID)
	if err != nil {
		t.Fatalf("find stale channel: %v", err)
	}
	if stored.TodayCost == nil || *stored.TodayCost != 2.5 {
		t.Fatalf("stored today cost = %v, want 2.5", stored.TodayCost)
	}

	// 只刷新余额（兑换码充值 / 消费采集失败）让 last_balance_at 变成今天，
	// legacy 两天前的消费也不能重新算作今日消费。
	now := costNow()
	if err := channels.UpdateBalance(legacyID, 1, &now, ""); err != nil {
		t.Fatalf("update legacy balance: %v", err)
	}
	getTodayCostJSON(t, r, "/api/channels", &list)
	assertTodayCosts(t, "list after balance-only update", list.Data)
}

func TestDashboardSummaryResetsStaleTodayCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, channels := seedTodayCostChannels(t)

	r := gin.New()
	registerDashboard(r.Group("/api"), &Deps{
		Channels: channels,
		Rates:    storage.NewRates(db),
	})

	var resp struct {
		Data struct {
			TodayTotalCost float64               `json:"today_total_cost"`
			TotalCost      float64               `json:"total_cost"`
			Channels       []todayCostChannelOut `json:"channels"`
		} `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/dashboard/summary", &resp)
	if resp.Data.TodayTotalCost != 1.25 {
		t.Fatalf("today total cost = %v, want 1.25", resp.Data.TodayTotalCost)
	}
	if resp.Data.TotalCost != 60 {
		t.Fatalf("total cost = %v, want 60", resp.Data.TotalCost)
	}
	assertTodayCosts(t, "summary", resp.Data.Channels)
}
