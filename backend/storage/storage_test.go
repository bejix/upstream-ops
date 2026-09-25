package storage

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := Open(DBConfig{
		Driver:       DBDriverSQLite,
		Path:         filepath.Join(t.TempDir(), "test.db"),
		MaxOpenConns: 20,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

func TestAggregateBalanceTrend(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, Balance: 20, SampledAt: day2.Add(12 * time.Hour)},
		{ChannelID: 2, Balance: 5, SampledAt: day2.Add(10 * time.Hour)},
		{ChannelID: 1, Balance: 7, SampledAt: day1.Add(11 * time.Hour)},
		{ChannelID: 2, Balance: 3, SampledAt: day1.Add(18 * time.Hour)},
		{ChannelID: 2, Balance: 9, SampledAt: day0.Add(8 * time.Hour)},
		{ChannelID: 2, Balance: 11, SampledAt: day0.Add(22 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	got, err := rates.AggregateBalanceTrend(3)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyAggregate{
		{Day: day2, Balance: 25},
		{Day: day1, Balance: 10},
		{Day: day0, Balance: 11},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Balance != want[i].Balance {
			t.Fatalf("balance %d mismatch: got %v want %v", i, got[i].Balance, want[i].Balance)
		}
	}
}

func TestChannelProxyEnabledPersists(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)
	ch := &Channel{
		Name:           "proxy-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		ProxyEnabled:   true,
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	got, err := channels.FindByID(ch.ID)
	if err != nil {
		t.Fatalf("find channel: %v", err)
	}
	if !got.ProxyEnabled {
		t.Fatal("proxy_enabled = false, want true")
	}
}

func TestProxyEnabledPersistsForCaptchaAndNotification(t *testing.T) {
	db := openTestDB(t)

	captchas := NewCaptchas(db)
	cfg := &CaptchaConfig{
		Name:         "solver-proxy",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
		ProxyEnabled: true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("create captcha: %v", err)
	}
	gotCaptcha, err := captchas.FindByID(cfg.ID)
	if err != nil {
		t.Fatalf("find captcha: %v", err)
	}
	if !gotCaptcha.ProxyEnabled {
		t.Fatal("captcha proxy_enabled = false, want true")
	}

	notifies := NewNotifications(db)
	notify := &NotificationChannel{
		Name:         "notify-proxy",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
		ProxyEnabled: true,
	}
	if err := notifies.CreateChannel(notify); err != nil {
		t.Fatalf("create notification: %v", err)
	}
	gotNotify, err := notifies.FindChannel(notify.ID)
	if err != nil {
		t.Fatalf("find notification: %v", err)
	}
	if !gotNotify.ProxyEnabled {
		t.Fatal("notification proxy_enabled = false, want true")
	}
}

func TestAggregateBalanceTrendFillsMissingDays(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, Balance: 20, SampledAt: day0.Add(12 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	got, err := rates.AggregateBalanceTrend(3)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyAggregate{
		{Day: day2, Balance: 10},
		{Day: day1, Balance: 0},
		{Day: day0, Balance: 20},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Balance != want[i].Balance {
			t.Fatalf("balance %d mismatch: got %v want %v", i, got[i].Balance, want[i].Balance)
		}
	}
}

func TestAggregateCostTrend(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.1, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, TodayCost: 2.2, SampledAt: day2.Add(18 * time.Hour)},
		{ChannelID: 2, TodayCost: 0.8, SampledAt: day2.Add(10 * time.Hour)},
		{ChannelID: 1, TodayCost: 3.5, SampledAt: day1.Add(11 * time.Hour)},
		{ChannelID: 2, TodayCost: 1.2, SampledAt: day1.Add(13 * time.Hour)},
		{ChannelID: 2, TodayCost: 1.8, SampledAt: day1.Add(22 * time.Hour)},
		{ChannelID: 1, TodayCost: 4.0, SampledAt: day0.Add(8 * time.Hour)},
		{ChannelID: 2, TodayCost: 2.5, SampledAt: day0.Add(21 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	got, err := rates.AggregateCostTrend(3)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyCostAggregate{
		{Day: day2, Cost: 3.0},
		{Day: day1, Cost: 5.3},
		{Day: day0, Cost: 6.5},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Cost != want[i].Cost {
			t.Fatalf("cost %d mismatch: got %v want %v", i, got[i].Cost, want[i].Cost)
		}
	}
}

func TestAggregateCostTrendFillsMissingDays(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.5, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, TodayCost: 2.5, SampledAt: day0.Add(12 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	got, err := rates.AggregateCostTrend(3)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyCostAggregate{
		{Day: day2, Cost: 1.5},
		{Day: day1, Cost: 0},
		{Day: day0, Cost: 2.5},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Cost != want[i].Cost {
			t.Fatalf("cost %d mismatch: got %v want %v", i, got[i].Cost, want[i].Cost)
		}
	}
}

func TestAggregateTrendUsesShanghaiDayBoundary(t *testing.T) {
	oldNow := trendNow
	trendNow = func() time.Time {
		return time.Date(2026, 6, 19, 16, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { trendNow = oldNow })

	db := openTestDB(t)
	rates := NewRates(db)

	day0 := time.Date(2026, 6, 20, 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)

	balanceSnapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: time.Date(2026, 6, 19, 15, 59, 0, 0, time.UTC)},
		{ChannelID: 1, Balance: 20, SampledAt: time.Date(2026, 6, 19, 16, 1, 0, 0, time.UTC)},
	}
	for _, snapshot := range balanceSnapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	costSnapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.5, SampledAt: time.Date(2026, 6, 19, 15, 59, 0, 0, time.UTC)},
		{ChannelID: 1, TodayCost: 2.5, SampledAt: time.Date(2026, 6, 19, 16, 1, 0, 0, time.UTC)},
	}
	for _, snapshot := range costSnapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	balances, err := rates.AggregateBalanceTrend(2)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(balances) != 2 {
		t.Fatalf("balance days = %d, want 2", len(balances))
	}
	if !balances[0].Day.Equal(day1) || balances[0].Balance != 10 {
		t.Fatalf("previous shanghai day = %#v, want day %s balance 10", balances[0], day1)
	}
	if !balances[1].Day.Equal(day0) || balances[1].Balance != 20 {
		t.Fatalf("current shanghai day = %#v, want day %s balance 20", balances[1], day0)
	}

	costs, err := rates.AggregateCostTrend(2)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(costs) != 2 {
		t.Fatalf("cost days = %d, want 2", len(costs))
	}
	if !costs[0].Day.Equal(day1) || costs[0].Cost != 1.5 {
		t.Fatalf("previous shanghai day cost = %#v, want day %s cost 1.5", costs[0], day1)
	}
	if !costs[1].Day.Equal(day0) || costs[1].Cost != 2.5 {
		t.Fatalf("current shanghai day cost = %#v, want day %s cost 2.5", costs[1], day0)
	}
}

func TestDeleteCostSnapshotsBefore(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now()
	oldSnapshot := CostSnapshot{ChannelID: 1, TodayCost: 1.2, SampledAt: now.AddDate(0, 0, -10)}
	newSnapshot := CostSnapshot{ChannelID: 1, TodayCost: 2.3, SampledAt: now.AddDate(0, 0, -2)}
	if err := rates.AppendCost(&oldSnapshot); err != nil {
		t.Fatalf("append old cost: %v", err)
	}
	if err := rates.AppendCost(&newSnapshot); err != nil {
		t.Fatalf("append new cost: %v", err)
	}

	deleted, err := rates.DeleteCostSnapshotsBefore(now.AddDate(0, 0, -5))
	if err != nil {
		t.Fatalf("delete cost snapshots: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	var count int64
	if err := db.Model(&CostSnapshot{}).Count(&count).Error; err != nil {
		t.Fatalf("count cost snapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining count = %d, want 1", count)
	}
}

func TestTryClaimCooldown(t *testing.T) {
	db := openTestDB(t)
	notifications := NewNotifications(db)

	ok, err := notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !ok {
		t.Fatal("first claim should succeed")
	}

	ok, err = notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if ok {
		t.Fatal("second claim should be blocked by cooldown")
	}

	oldTime := time.Now().Add(-2 * time.Minute)
	if err := db.Model(&NotificationCooldown{}).
		Where("channel_id = ? AND event = ?", 1, EventBalanceLow).
		Updates(map[string]any{
			"last_sent_at": oldTime,
			"updated_at":   oldTime,
		}).Error; err != nil {
		t.Fatalf("age cooldown: %v", err)
	}

	ok, err = notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("third claim: %v", err)
	}
	if !ok {
		t.Fatal("third claim should succeed after cooldown expires")
	}
}

func TestTryClaimCooldownConcurrent(t *testing.T) {
	db := openTestDB(t)
	notifications := NewNotifications(db)

	var claimed int32
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ok, err := notifications.TryClaimCooldown(2, EventBalanceLow, time.Minute)
			if err != nil {
				t.Errorf("concurrent claim: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&claimed, 1)
			}
		}()
	}
	wg.Wait()

	if claimed != 1 {
		t.Fatalf("expected exactly one successful claim, got %d", claimed)
	}
}

func TestUpstreamAnnouncementsSyncDedupes(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	items := []UpstreamAnnouncement{
		{SourceKey: "a", Title: "A", Content: "one", FirstSeenAt: now},
		{SourceKey: "a", Title: "A2", Content: "dup", FirstSeenAt: now.Add(time.Second)},
	}
	newItems, err := announcements.Sync(1, items)
	if err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if len(newItems) != 1 {
		t.Fatalf("new items = %d, want 1", len(newItems))
	}

	exists, err := announcements.Exists(1, "a")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected announcement to exist")
	}
}

func TestUpstreamAnnouncementsListLatest(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	publishedOld := now.Add(-3 * time.Hour)
	publishedNew := now.Add(-1 * time.Hour)
	items := []UpstreamAnnouncement{
		{ChannelID: 1, SourceKey: "a", Content: "body", PublishedAt: &publishedOld, FirstSeenAt: now.Add(3 * time.Minute)},
		{ChannelID: 1, SourceKey: "b", Content: "body", PublishedAt: &publishedNew, FirstSeenAt: now.Add(1 * time.Minute)},
		{ChannelID: 1, SourceKey: "c", Content: "body", FirstSeenAt: now.Add(4 * time.Minute)},
	}
	for _, item := range items {
		item := item
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create announcement: %v", err)
		}
	}

	list, err := announcements.ListLatest(2)
	if err != nil {
		t.Fatalf("list latest: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].SourceKey != "b" || list[1].SourceKey != "a" {
		t.Fatalf("unexpected order: %#v", list)
	}
}

func TestUpstreamAnnouncementsDeleteByChannel(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "a",
		Content:     "one",
		FirstSeenAt: now,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if _, err := announcements.Sync(2, []UpstreamAnnouncement{{
		SourceKey:   "b",
		Content:     "two",
		FirstSeenAt: now,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}

	rows, err := announcements.DeleteByChannel(1)
	if err != nil {
		t.Fatalf("delete by channel: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	list, total, err := announcements.ListPage(1, 10)
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ChannelID != 2 {
		t.Fatalf("unexpected remaining announcements: total=%d list=%#v", total, list)
	}
}

func TestUpstreamAnnouncementsDeleteBefore(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	oldTime := time.Now().AddDate(0, 0, -10)
	newTime := time.Now()
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "old",
		Content:     "old",
		FirstSeenAt: oldTime,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "new",
		Content:     "new",
		FirstSeenAt: newTime,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}

	rows, err := announcements.DeleteBefore(time.Now().AddDate(0, 0, -5))
	if err != nil {
		t.Fatalf("delete before: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	list, total, err := announcements.ListPage(1, 10)
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].SourceKey != "new" {
		t.Fatalf("unexpected remaining announcements: total=%d list=%#v", total, list)
	}
}

func TestUpdateCosts(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)

	c := &Channel{
		Name:           "test",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(c); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	at := time.Date(2026, 6, 20, 10, 30, 0, 0, time.UTC)
	if err := channels.UpdateCosts(c.ID, 1.23, 9.87, at); err != nil {
		t.Fatalf("update costs: %v", err)
	}

	got, err := channels.FindByID(c.ID)
	if err != nil {
		t.Fatalf("find channel: %v", err)
	}
	if got.TodayCost == nil || *got.TodayCost != 1.23 {
		t.Fatalf("today cost mismatch: %#v", got.TodayCost)
	}
	if got.TotalCost == nil || *got.TotalCost != 9.87 {
		t.Fatalf("total cost mismatch: %#v", got.TotalCost)
	}
	if got.TodayCostAt == nil || !got.TodayCostAt.Equal(at) {
		t.Fatalf("today cost at mismatch: %#v, want %v", got.TodayCostAt, at)
	}
}

func TestChannelEffectiveTodayCost(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	at := func(day, hour, minute int) *time.Time {
		v := time.Date(2026, 6, day, hour, minute, 0, 0, loc)
		return &v
	}
	cost := 3.5
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, loc)

	cases := []struct {
		name string
		ch   Channel
		now  time.Time
		want *float64
	}{
		{
			name: "same day keeps cost",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(20, 8, 0)},
			now:  now,
			want: &cost,
		},
		{
			name: "previous day resets to zero",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(19, 23, 59)},
			now:  now,
			want: ptrFloat(0),
		},
		{
			name: "just after midnight resets to zero",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(19, 23, 50)},
			now:  time.Date(2026, 6, 20, 0, 0, 1, 0, loc),
			want: ptrFloat(0),
		},
		{
			name: "sample just after midnight is today",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(20, 0, 1)},
			now:  time.Date(2026, 6, 20, 0, 5, 0, 0, loc),
			want: &cost,
		},
		{
			// 采样为 UTC 6/19 15:30（北京时间 6/19 23:30），now 为北京时间 6/20 00:30（UTC 仍是 6/19）：
			// 按 UTC 比较会误判为同一天，须按北京时间换算日期。
			name: "sample in utc compares dates in beijing time",
			ch: Channel{TodayCost: &cost, TodayCostAt: func() *time.Time {
				v := time.Date(2026, 6, 19, 15, 30, 0, 0, time.UTC) // UTC+8: 6/19 23:30
				return &v
			}()},
			now:  time.Date(2026, 6, 20, 0, 30, 0, 0, loc), // UTC: 6/19 16:30
			want: ptrFloat(0),
		},
		{
			// 服务器进程在 UTC（如 docker run 未设 TZ）：now 是 UTC 6/19 17:00，即北京时间 6/20 01:00，
			// 已经跨天，要与按北京时间换日的趋势图一致，不能按 now 自带的 UTC 判断成同一天。
			name: "now in utc resets at beijing midnight",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(19, 23, 0)},
			now:  time.Date(2026, 6, 19, 17, 0, 0, 0, time.UTC),
			want: ptrFloat(0),
		},
		{
			// 反过来：UTC 已经到 6/20，但北京时间 6/20 07:00 的采样与 08:30 的 now 仍是同一天。
			name: "now in utc keeps cost within beijing day",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(20, 7, 0)},
			now:  time.Date(2026, 6, 20, 0, 30, 0, 0, time.UTC),
			want: &cost,
		},
		{
			// 旧数据的采集时间由 AutoMigrate 回填，读取时不再拿 LastBalanceAt 推断。
			name: "missing today cost at ignores stale last balance at",
			ch:   Channel{TodayCost: &cost, LastBalanceAt: at(18, 12, 0)},
			now:  now,
			want: &cost,
		},
		{
			name: "missing today cost at ignores fresh last balance at",
			ch:   Channel{TodayCost: &cost, LastBalanceAt: at(20, 9, 0)},
			now:  now,
			want: &cost,
		},
		{
			// 余额单独刷新到今天，不能让昨天的消费重新变成"今日消费"。
			name: "fresh last balance at does not revive stale cost",
			ch:   Channel{TodayCost: &cost, TodayCostAt: at(19, 22, 0), LastBalanceAt: at(20, 9, 0)},
			now:  now,
			want: ptrFloat(0),
		},
		{
			name: "nil cost stays nil",
			ch:   Channel{TodayCostAt: at(18, 12, 0)},
			now:  now,
			want: nil,
		},
		{
			name: "no timestamps keeps cost",
			ch:   Channel{TodayCost: &cost},
			now:  now,
			want: &cost,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.ch.EffectiveTodayCost(tc.now)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("got %v, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("got nil, want %v", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("got %v, want %v", *got, *tc.want)
			}
		})
	}

	// 归零只作用于返回值，不能改写渠道上的原始数据。
	stale := Channel{TodayCost: &cost, TodayCostAt: at(19, 12, 0)}
	_ = stale.EffectiveTodayCost(now)
	if stale.TodayCost == nil || *stale.TodayCost != 3.5 {
		t.Fatalf("original today cost mutated: %#v", stale.TodayCost)
	}
}

func ptrFloat(v float64) *float64 { return &v }

func TestHardDeleteAllowsReusingNames(t *testing.T) {
	db := openTestDB(t)

	channels := NewChannels(db)
	ch := &Channel{
		Name:           "demo",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := channels.Delete(ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	ch = &Channel{
		Name:           "demo",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("recreate channel: %v", err)
	}

	captchas := NewCaptchas(db)
	cfg := &CaptchaConfig{
		Name:         "solver",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("create captcha: %v", err)
	}
	if err := captchas.Delete(cfg.ID); err != nil {
		t.Fatalf("delete captcha: %v", err)
	}
	cfg = &CaptchaConfig{
		Name:         "solver",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("recreate captcha: %v", err)
	}

	notifications := NewNotifications(db)
	notify := &NotificationChannel{
		Name:         "telegram",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
	}
	if err := notifications.CreateChannel(notify); err != nil {
		t.Fatalf("create notification channel: %v", err)
	}
	if err := notifications.DeleteChannel(notify.ID); err != nil {
		t.Fatalf("delete notification channel: %v", err)
	}
	notify = &NotificationChannel{
		Name:         "telegram",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
	}
	if err := notifications.CreateChannel(notify); err != nil {
		t.Fatalf("recreate notification channel: %v", err)
	}
}

func TestDeleteChannelCleansScopedState(t *testing.T) {
	db := openTestDB(t)

	channels := NewChannels(db)
	ch := &Channel{
		Name:           "demo",
		Type:           ChannelTypeSub2API,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	now := time.Now()
	if err := db.Create(&AuthSession{ChannelID: ch.ID}).Error; err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	if err := db.Create(&RateSnapshot{ChannelID: ch.ID, ModelName: "old", Ratio: 1, LastSeenAt: now}).Error; err != nil {
		t.Fatalf("create rate snapshot: %v", err)
	}
	if err := db.Create(&RateChangeLog{ChannelID: ch.ID, ModelName: "old", NewRatio: 1, ChangedAt: now}).Error; err != nil {
		t.Fatalf("create rate change: %v", err)
	}
	if err := db.Create(&BalanceSnapshot{ChannelID: ch.ID, Balance: 1, SampledAt: now}).Error; err != nil {
		t.Fatalf("create balance snapshot: %v", err)
	}
	if err := db.Create(&CostSnapshot{ChannelID: ch.ID, TodayCost: 1, SampledAt: now}).Error; err != nil {
		t.Fatalf("create cost snapshot: %v", err)
	}
	if err := db.Create(&MonitorLog{ChannelID: ch.ID, Job: MonitorJobBalance, Success: true, StartedAt: now, FinishedAt: now}).Error; err != nil {
		t.Fatalf("create monitor log: %v", err)
	}
	if err := db.Create(&NotificationCooldown{ChannelID: ch.ID, Event: EventBalanceLow, LastSentAt: now}).Error; err != nil {
		t.Fatalf("create cooldown: %v", err)
	}
	if err := db.Create(&NotificationLog{ChannelID: 99, UpstreamChannelID: ch.ID, Event: EventBalanceLow, Subject: "alert", Success: true, SentAt: now}).Error; err != nil {
		t.Fatalf("create notification log: %v", err)
	}
	if err := db.Create(&NotificationLog{ChannelID: 99, Event: EventBalanceLow, Subject: "demo 余额低于阈值", Success: true, SentAt: now}).Error; err != nil {
		t.Fatalf("create legacy notification log: %v", err)
	}
	if err := db.Create(&UpstreamAnnouncement{ChannelID: ch.ID, SourceKey: "a", Content: "deleted", FirstSeenAt: now}).Error; err != nil {
		t.Fatalf("create announcement: %v", err)
	}

	if err := channels.Delete(ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}

	for _, tt := range []struct {
		name  string
		model any
	}{
		{"auth_sessions", &AuthSession{}},
		{"rate_snapshots", &RateSnapshot{}},
		{"rate_change_logs", &RateChangeLog{}},
		{"balance_snapshots", &BalanceSnapshot{}},
		{"cost_snapshots", &CostSnapshot{}},
		{"monitor_logs", &MonitorLog{}},
		{"notification_cooldowns", &NotificationCooldown{}},
		{"upstream_announcements", &UpstreamAnnouncement{}},
		{"notification_logs", &NotificationLog{}},
	} {
		var count int64
		q := db.Model(tt.model).Where("channel_id = ?", ch.ID)
		if tt.name == "notification_logs" {
			q = db.Model(tt.model).Where("upstream_channel_id = ? OR subject LIKE ?", ch.ID, "%"+ch.Name+"%")
		}
		if err := q.Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", tt.name, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", tt.name, count)
		}
	}
}

func TestAutoMigrateDropsDeletedAtColumns(t *testing.T) {
	db := openTestDB(t)

	for _, ddl := range []string{
		"ALTER TABLE channels ADD COLUMN deleted_at datetime",
		"ALTER TABLE captcha_configs ADD COLUMN deleted_at datetime",
		"ALTER TABLE notification_channels ADD COLUMN deleted_at datetime",
		"CREATE INDEX idx_channels_deleted_at ON channels(deleted_at)",
		"CREATE INDEX idx_captcha_configs_deleted_at ON captcha_configs(deleted_at)",
		"CREATE INDEX idx_notification_channels_deleted_at ON notification_channels(deleted_at)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("exec %q: %v", ddl, err)
		}
	}

	now := time.Now()
	activeChannel := &Channel{
		Name:           "active-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	deletedChannel := &Channel{
		Name:           "deleted-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := db.Create(activeChannel).Error; err != nil {
		t.Fatalf("create active channel: %v", err)
	}
	if err := db.Create(deletedChannel).Error; err != nil {
		t.Fatalf("create deleted channel: %v", err)
	}
	if err := db.Table("channels").Where("id = ?", deletedChannel.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted channel: %v", err)
	}

	activeCaptcha := &CaptchaConfig{Name: "active-captcha", Type: CaptchaCapSolver, APIKeyCipher: "x", Enabled: true}
	deletedCaptcha := &CaptchaConfig{Name: "deleted-captcha", Type: CaptchaCapSolver, APIKeyCipher: "x", Enabled: true}
	if err := db.Create(activeCaptcha).Error; err != nil {
		t.Fatalf("create active captcha: %v", err)
	}
	if err := db.Create(deletedCaptcha).Error; err != nil {
		t.Fatalf("create deleted captcha: %v", err)
	}
	if err := db.Table("captcha_configs").Where("id = ?", deletedCaptcha.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted captcha: %v", err)
	}

	activeNotify := &NotificationChannel{Name: "active-notify", Type: NotifyTelegram, ConfigCipher: "x", Enabled: true}
	deletedNotify := &NotificationChannel{Name: "deleted-notify", Type: NotifyTelegram, ConfigCipher: "x", Enabled: true}
	if err := db.Create(activeNotify).Error; err != nil {
		t.Fatalf("create active notification channel: %v", err)
	}
	if err := db.Create(deletedNotify).Error; err != nil {
		t.Fatalf("create deleted notification channel: %v", err)
	}
	if err := db.Table("notification_channels").Where("id = ?", deletedNotify.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted notification channel: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	for _, table := range []string{"channels", "captcha_configs", "notification_channels"} {
		hasColumn, err := tableHasColumn(db, table, "deleted_at")
		if err != nil {
			t.Fatalf("inspect %s.deleted_at: %v", table, err)
		}
		if hasColumn {
			t.Fatalf("%s.deleted_at still exists", table)
		}
	}

	var count int64
	if err := db.Model(&Channel{}).Count(&count).Error; err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("channel count = %d, want 1", count)
	}
	if err := db.Model(&CaptchaConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count captchas: %v", err)
	}
	if count != 1 {
		t.Fatalf("captcha count = %d, want 1", count)
	}
	if err := db.Model(&NotificationChannel{}).Count(&count).Error; err != nil {
		t.Fatalf("count notification channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("notification channel count = %d, want 1", count)
	}
}

func TestAutoMigrateDropsObsoleteRateSiteColumns(t *testing.T) {
	db := openTestDB(t)
	if err := db.Exec("ALTER TABLE rate_snapshots ADD COLUMN site_id integer NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatalf("add rate_snapshots.site_id: %v", err)
	}
	if err := db.Exec("ALTER TABLE rate_change_logs ADD COLUMN site_id integer NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatalf("add rate_change_logs.site_id: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	for _, table := range []string{"rate_snapshots", "rate_change_logs"} {
		hasColumn, err := tableHasColumn(db, table, "site_id")
		if err != nil {
			t.Fatalf("inspect %s.site_id: %v", table, err)
		}
		if hasColumn {
			t.Fatalf("%s.site_id still exists", table)
		}
	}
}

func TestGatewayGroupsReorderAndListOrder(t *testing.T) {
	db := openTestDB(t)
	repo := NewGatewayGroups(db)

	a := &GatewayGroup{Name: "a", Status: GatewayGroupStatusActive}
	b := &GatewayGroup{Name: "b", Status: GatewayGroupStatusActive}
	c := &GatewayGroup{Name: "c", Status: GatewayGroupStatusActive}
	for _, g := range []*GatewayGroup{a, b, c} {
		pos, err := repo.NextPosition()
		if err != nil {
			t.Fatalf("next pos: %v", err)
		}
		g.Position = pos
		if err := repo.Create(g); err != nil {
			t.Fatalf("create %s: %v", g.Name, err)
		}
	}

	// 创建顺序 a,b,c → position 0,1,2；列表应为 a,b,c
	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 || list[0].Name != "a" || list[1].Name != "b" || list[2].Name != "c" {
		t.Fatalf("initial order = %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}

	// 重排为 c, a, b
	if err := repo.Reorder([]uint{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	list, err = repo.List()
	if err != nil {
		t.Fatalf("list after reorder: %v", err)
	}
	if list[0].Name != "c" || list[1].Name != "a" || list[2].Name != "b" {
		t.Fatalf("reordered = %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
	if list[0].Position != 0 || list[1].Position != 1 || list[2].Position != 2 {
		t.Fatalf("positions = %d %d %d", list[0].Position, list[1].Position, list[2].Position)
	}
}

func TestGatewayRoutesSaveForGroupPreservesID(t *testing.T) {
	db, err := Open(DBConfig{
		Driver:       DBDriverSQLite,
		Path:         filepath.Join(t.TempDir(), "gw-route-preserve.db"),
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	routes := NewGatewayRoutes(db)
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{SourceChannelID: 1, Weight: 1, Enabled: true},
		{SourceChannelID: 2, Weight: 2, Enabled: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := routes.ListByGroupID(7)
	if err != nil || len(first) != 2 {
		t.Fatalf("list first: %v len=%d", err, len(first))
	}
	id0, id1 := first[0].ID, first[1].ID
	if id0 == 0 || id1 == 0 {
		t.Fatalf("ids zero: %+v", first)
	}
	// 模拟 ensure-keys 写入上游密钥
	if err := routes.UpdateSourceKey(id0, 41, "upstream-ops-gw-g7-r1", "cipher-a"); err != nil {
		t.Fatalf("key0: %v", err)
	}
	if err := routes.UpdateSourceKey(id1, 42, "k2", "cipher-b"); err != nil {
		t.Fatalf("key1: %v", err)
	}

	// 换序 + 改权重，id 应保持；密钥字段由服务端从旧行回填
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{ID: id1, SourceChannelID: 2, Weight: 9, Enabled: true},
		{ID: id0, SourceChannelID: 1, Weight: 3, Enabled: true},
	}); err != nil {
		t.Fatalf("update reorder: %v", err)
	}
	second, err := routes.ListByGroupID(7)
	if err != nil || len(second) != 2 {
		t.Fatalf("list second: %v len=%d", err, len(second))
	}
	if second[0].ID != id1 || second[1].ID != id0 {
		t.Fatalf("ids not preserved: got %d,%d want %d,%d", second[0].ID, second[1].ID, id1, id0)
	}
	if second[0].Weight != 9 || second[1].Weight != 3 {
		t.Fatalf("weights: %+v", second)
	}
	if second[0].SourceAPIKeyName != "k2" || second[1].SourceAPIKeyName != "upstream-ops-gw-g7-r1" {
		t.Fatalf("keys not preserved: %+v / %+v", second[0], second[1])
	}

	// 删除一条
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{ID: id0, SourceChannelID: 1, Weight: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	third, err := routes.ListByGroupID(7)
	if err != nil || len(third) != 1 || third[0].ID != id0 {
		t.Fatalf("after delete: %+v err=%v", third, err)
	}
}

func TestNoteSuccessForPauseErrorClearsAfterStreak(t *testing.T) {
	db := openTestDB(t)
	routes := NewGatewayRoutes(db)
	if err := routes.SaveForGroup(9, []GatewayRoute{
		{SourceChannelID: 11, Weight: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := routes.ListByGroupID(9)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	id := list[0].ID

	until := time.Now().Add(5 * time.Minute)
	if err := routes.SetTempUnschedulable(id, until, "upstream HTTP error\nstatus: 503", time.Now(), "req_pause_1"); err != nil {
		t.Fatalf("set pause: %v", err)
	}

	// 无残留路由：调用应 noop
	if err := routes.NoteSuccessForPauseError(0); err != nil {
		t.Fatalf("id0: %v", err)
	}

	// 第 1、2 次成功：解除冷却，但保留错误信息
	for i := 1; i <= 2; i++ {
		if err := routes.NoteSuccessForPauseError(id); err != nil {
			t.Fatalf("success %d: %v", i, err)
		}
		got, err := routes.FindByID(id)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
		if got.TempUnschedulableUntil != nil {
			t.Fatalf("success %d: until should be cleared", i)
		}
		if got.TempUnschedulableReason == "" {
			t.Fatalf("success %d: reason should remain for UI", i)
		}
		if got.RecoverSuccessStreak != i {
			t.Fatalf("success %d: streak=%d", i, got.RecoverSuccessStreak)
		}
	}

	// 第 3 次成功：清空「已恢复/错误/清除」相关字段
	if err := routes.NoteSuccessForPauseError(id); err != nil {
		t.Fatalf("success 3: %v", err)
	}
	got, err := routes.FindByID(id)
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if got.TempUnschedulableUntil != nil ||
		got.TempUnschedulableReason != "" ||
		got.TempUnschedulableAt != nil ||
		got.TempUnschedulableRequestID != "" ||
		got.RecoverSuccessStreak != 0 {
		t.Fatalf("expected full clear, got %+v", got)
	}

	// 无残留后再成功：不累计 streak
	if err := routes.NoteSuccessForPauseError(id); err != nil {
		t.Fatalf("success after clear: %v", err)
	}
	got, err = routes.FindByID(id)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.RecoverSuccessStreak != 0 {
		t.Fatalf("streak should stay 0, got %d", got.RecoverSuccessStreak)
	}
}

func TestGatewayUsageListModels(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	now := time.Now()
	rows := []GatewayUsageLog{
		{RequestID: "r1", RequestedModel: "grok-4", UpstreamModel: "grok-4", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
		{RequestID: "r2", RequestedModel: "grok-4", UpstreamModel: "grok-4", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
		{RequestID: "r3", RequestedModel: "claude-sonnet", UpstreamModel: "claude-sonnet", GatewayGroupID: 1, GatewayKeyID: 2, Success: true, CreatedAt: now},
		{RequestID: "r4", RequestedModel: "gpt-4o", UpstreamModel: "gpt-4o", GatewayGroupID: 2, GatewayKeyID: 3, Success: true, CreatedAt: now},
		{RequestID: "r5", RequestedModel: "", UpstreamModel: "ignored", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	all, err := usage.ListModels(GatewayUsageQuery{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 models, got %d %+v", len(all), all)
	}
	if all[0].Model != "grok-4" || all[0].Count != 2 {
		t.Fatalf("first should be grok-4 x2, got %+v", all[0])
	}

	g1, err := usage.ListModels(GatewayUsageQuery{GatewayGroupID: 1})
	if err != nil {
		t.Fatalf("list g1: %v", err)
	}
	if len(g1) != 2 {
		t.Fatalf("group1 want 2 models, got %d %+v", len(g1), g1)
	}

	// model 筛选不应影响下拉聚合
	withModel, err := usage.ListModels(GatewayUsageQuery{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("list with model filter ignored: %v", err)
	}
	if len(withModel) != 3 {
		t.Fatalf("model filter should be ignored, got %d", len(withModel))
	}
}
