package storage

import (
	"testing"
	"time"
)

// createTodayCostChannel 写入一个渠道，cost / costAt / balanceAt 为 nil 时对应列为 NULL。
func createTodayCostChannel(t *testing.T, channels *Channels, name string, cost *float64, costAt, balanceAt *time.Time) uint {
	t.Helper()
	c := &Channel{
		Name:           name,
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://" + name + ".example.com",
		Username:       "u",
		PasswordCipher: "x",
		TodayCost:      cost,
		TodayCostAt:    costAt,
		LastBalanceAt:  balanceAt,
	}
	if err := channels.Create(c); err != nil {
		t.Fatalf("create channel %s: %v", name, err)
	}
	return c.ID
}

func appendCostSnapshots(t *testing.T, rates *Rates, channelID uint, at ...time.Time) {
	t.Helper()
	for _, sampledAt := range at {
		if err := rates.AppendCost(&CostSnapshot{ChannelID: channelID, TodayCost: 1, SampledAt: sampledAt}); err != nil {
			t.Fatalf("append cost snapshot: %v", err)
		}
	}
}

func assertTodayCostAt(t *testing.T, channels *Channels, label string, id uint, want *time.Time) {
	t.Helper()
	got, err := channels.FindByID(id)
	if err != nil {
		t.Fatalf("%s: find channel %d: %v", label, id, err)
	}
	switch {
	case want == nil && got.TodayCostAt != nil:
		t.Fatalf("%s: today_cost_at = %v, want NULL", label, *got.TodayCostAt)
	case want != nil && got.TodayCostAt == nil:
		t.Fatalf("%s: today_cost_at = NULL, want %v", label, *want)
	case want != nil && !got.TodayCostAt.Equal(*want):
		t.Fatalf("%s: today_cost_at = %v, want %v", label, *got.TodayCostAt, *want)
	}
}

func TestAutoMigrateBackfillsTodayCostAt(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)
	rates := NewRates(db)
	loc := time.FixedZone("UTC+8", 8*60*60)
	ts := func(day, hour int) *time.Time {
		v := time.Date(2026, 6, day, hour, 0, 0, 0, loc)
		return &v
	}

	// 余额单独刷新到了今天，消费最后一次成功采集在 6/18：应取快照里最大的 sampled_at，
	// 而不是最后插入的那条，也不是 last_balance_at。
	fromSnapshot := createTodayCostChannel(t, channels, "from-snapshot", ptrFloat(5), nil, ts(20, 9))
	appendCostSnapshots(t, rates, fromSnapshot, *ts(17, 10), *ts(18, 11), *ts(16, 8))
	// 快照已被清理：退回 last_balance_at。
	fromBalance := createTodayCostChannel(t, channels, "from-balance", ptrFloat(2), nil, ts(19, 12))
	// 已有采集时间：不被更新的快照覆盖。
	keep := createTodayCostChannel(t, channels, "keep", ptrFloat(3), ts(20, 8), ts(20, 8))
	appendCostSnapshots(t, rates, keep, *ts(20, 9))
	// 从未采集到消费：保持 NULL。
	noCost := createTodayCostChannel(t, channels, "no-cost", nil, nil, ts(19, 12))
	appendCostSnapshots(t, rates, noCost, *ts(19, 12))
	// 没有快照也没有余额时间：无从回填，保持 NULL（其它渠道的快照不能串过来）。
	noTime := createTodayCostChannel(t, channels, "no-time", ptrFloat(1), nil, nil)

	want := map[string]struct {
		id uint
		at *time.Time
	}{
		"from-snapshot": {fromSnapshot, ts(18, 11)},
		"from-balance":  {fromBalance, ts(19, 12)},
		"keep":          {keep, ts(20, 8)},
		"no-cost":       {noCost, nil},
		"no-time":       {noTime, nil},
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	for name, w := range want {
		assertTodayCostAt(t, channels, "backfill "+name, w.id, w.at)
	}

	// 幂等：已回填的行不再变化，即使之后又有了更新的快照。
	appendCostSnapshots(t, rates, fromSnapshot, *ts(20, 10))
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate again: %v", err)
	}
	for name, w := range want {
		assertTodayCostAt(t, channels, "rerun "+name, w.id, w.at)
	}

	// 回填后的输出：6/18、6/19 的消费在 6/20 归零；无从判断的原样返回。
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, loc)
	for name, wantCost := range map[string]*float64{
		"from-snapshot": ptrFloat(0),
		"from-balance":  ptrFloat(0),
		"keep":          ptrFloat(3),
		"no-cost":       nil,
		"no-time":       ptrFloat(1),
	} {
		ch, err := channels.FindByID(want[name].id)
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		got := ch.EffectiveTodayCost(now)
		switch {
		case wantCost == nil && got != nil:
			t.Fatalf("%s: effective today cost = %v, want nil", name, *got)
		case wantCost != nil && got == nil:
			t.Fatalf("%s: effective today cost = nil, want %v", name, *wantCost)
		case wantCost != nil && *got != *wantCost:
			t.Fatalf("%s: effective today cost = %v, want %v", name, *got, *wantCost)
		}
	}
}

// TestLegacyTodayCostNotRevivedByBalanceUpdate 复现评审场景：升级前的库没有 today_cost_at 列，
// 渠道三天前采到 today_cost=5。升级后只刷新余额（兑换码充值 / 消费采集失败），
// last_balance_at 变成今天，今日消费仍应为 0。
func TestLegacyTodayCostNotRevivedByBalanceUpdate(t *testing.T) {
	db := openTestDB(t)
	if err := db.Exec("ALTER TABLE channels DROP COLUMN today_cost_at").Error; err != nil {
		t.Fatalf("drop today_cost_at: %v", err)
	}
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, loc)
	threeDaysAgo := now.AddDate(0, 0, -3)
	legacy := &Channel{
		Name:           "legacy",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://legacy.example.com",
		Username:       "u",
		PasswordCipher: "x",
		TodayCost:      ptrFloat(5),
		LastBalanceAt:  &threeDaysAgo,
	}
	if err := db.Omit("TodayCostAt").Create(legacy).Error; err != nil {
		t.Fatalf("create legacy channel: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	channels := NewChannels(db)
	assertTodayCostAt(t, channels, "after upgrade", legacy.ID, &threeDaysAgo)

	if err := channels.UpdateBalance(legacy.ID, 42, &now, ""); err != nil {
		t.Fatalf("update balance: %v", err)
	}
	got, err := channels.FindByID(legacy.ID)
	if err != nil {
		t.Fatalf("find legacy: %v", err)
	}
	if got.LastBalanceAt == nil || !got.LastBalanceAt.Equal(now) {
		t.Fatalf("last_balance_at = %v, want %v", got.LastBalanceAt, now)
	}
	if v := got.EffectiveTodayCost(now); v == nil {
		t.Fatal("effective today cost = nil, want 0")
	} else if *v != 0 {
		t.Fatalf("effective today cost = %v, want 0", *v)
	}
}
