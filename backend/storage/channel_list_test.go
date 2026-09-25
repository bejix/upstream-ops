package storage

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// seedChannelListFixtures 写入覆盖各状态 / 排序边界的渠道，按 id 递增依次为：
//
//	名称     账号   排序  余额  阈值  错误  标签       备注        监控   → 状态
//	Alpha    alice  5     100   10    -     vip,备用   主力渠道     开     healthy
//	bravo    Bob    1     5     10    -     ab         -           暂停   low
//	Charlie  carol  3     50    0     有    a          -           开     failed
//	delta    dave   1     nil   0     -     -          进度 100%    开     idle
//	echo     Eve    1     0     0     -     -          -           开     healthy（站点含 "_"）
//	Foxtrot  frank  1     20    20    -     VIP        hi!         暂停   healthy（余额等于阈值不算低）
//	golf     ""     1     nil   5     有    -          -           开     failed（错误优先于未采集）
//
// 默认顺序（sort_order DESC, id ASC）：Alpha, Charlie, bravo, delta, echo, Foxtrot, golf。
func seedChannelListFixtures(t *testing.T) *Channels {
	t.Helper()
	db := openTestDB(t)
	channels := NewChannels(db)
	f := func(v float64) *float64 { return &v }
	type fixture struct {
		ch     Channel
		paused bool
	}
	fixtures := []fixture{
		{ch: Channel{Name: "Alpha", Username: "alice", SiteURL: "https://alpha.example.com", SortOrder: 5, LastBalance: f(100), BalanceThreshold: 10, Tags: ChannelTags{"vip", "备用"}, Notes: "主力渠道"}},
		{ch: Channel{Name: "bravo", Username: "Bob", SiteURL: "https://bravo.example.com", SortOrder: 1, LastBalance: f(5), BalanceThreshold: 10, Tags: ChannelTags{"ab"}}, paused: true},
		{ch: Channel{Name: "Charlie", Username: "carol", SiteURL: "https://charlie.example.com", SortOrder: 3, LastBalance: f(50), LastError: "登录失败: 401", Tags: ChannelTags{"a"}}},
		{ch: Channel{Name: "delta", Username: "dave", SiteURL: "https://delta.example.com", SortOrder: 1, Notes: "进度 100%"}},
		{ch: Channel{Name: "echo", Username: "Eve", SiteURL: "https://echo.example.com/v1_api", SortOrder: 1, LastBalance: f(0)}},
		{ch: Channel{Name: "Foxtrot", Username: "frank", SiteURL: "https://foxtrot.example.com", SortOrder: 1, LastBalance: f(20), BalanceThreshold: 20, Tags: ChannelTags{"VIP"}, Notes: "hi!"}, paused: true},
		{ch: Channel{Name: "golf", Username: "", SiteURL: "https://golf.example.com", SortOrder: 1, BalanceThreshold: 5, LastError: "session expired"}},
	}
	for i := range fixtures {
		c := fixtures[i].ch
		c.Type = ChannelTypeNewAPI
		c.PasswordCipher = "x"
		c.MonitorEnabled = true
		if err := channels.Create(&c); err != nil {
			t.Fatalf("create channel %s: %v", c.Name, err)
		}
		// monitor_enabled 带 default:true，Create 时 false 会被当成零值忽略，这里用 Save 落库。
		if fixtures[i].paused {
			c.MonitorEnabled = false
			if err := channels.Update(&c); err != nil {
				t.Fatalf("pause channel %s: %v", c.Name, err)
			}
		}
	}
	return channels
}

func channelNames(list []Channel) []string {
	names := make([]string, 0, len(list))
	for _, c := range list {
		names = append(names, c.Name)
	}
	return names
}

func listChannelNames(t *testing.T, channels *Channels, page, pageSize int, filter ChannelListFilter) ([]string, int64) {
	t.Helper()
	list, total, err := channels.ListPage(page, pageSize, filter)
	if err != nil {
		t.Fatalf("ListPage(%d, %d, %+v): %v", page, pageSize, filter, err)
	}
	return channelNames(list), total
}

func assertChannelNames(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %q, want %q", label, got, want)
	}
}

// statusOfForTest 逐行照抄前端 channel-cards.tsx 的 statusOf()，用来交叉校验 SQL 条件。
func statusOfForTest(c Channel) string {
	if c.LastError != "" {
		return ChannelStatusFailed
	}
	if c.LastBalance == nil {
		return ChannelStatusIdle
	}
	if c.BalanceThreshold > 0 && *c.LastBalance < c.BalanceThreshold {
		return ChannelStatusLow
	}
	return ChannelStatusHealthy
}

var defaultChannelListOrder = []string{"Alpha", "Charlie", "bravo", "delta", "echo", "Foxtrot", "golf"}

func TestChannelListPageDefaultOrderUnchanged(t *testing.T) {
	channels := seedChannelListFixtures(t)

	got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{})
	assertChannelNames(t, "zero filter", got, defaultChannelListOrder...)
	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}

	// 默认顺序忽略 order，与 List() 保持一致。
	got, _ = listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: ChannelSortDefault, Order: ChannelOrderDesc})
	assertChannelNames(t, "default desc", got, defaultChannelListOrder...)

	all, err := channels.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	assertChannelNames(t, "List()", channelNames(all), defaultChannelListOrder...)

	got, total = listChannelNames(t, channels, 2, 3, ChannelListFilter{})
	assertChannelNames(t, "page 2", got, "delta", "echo", "Foxtrot")
	if total != 7 {
		t.Fatalf("page 2 total = %d, want 7", total)
	}
}

func TestChannelListPageQuery(t *testing.T) {
	channels := seedChannelListFixtures(t)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "name case insensitive", query: "ALPHA", want: []string{"Alpha"}},
		{name: "username case insensitive", query: "bob", want: []string{"bravo"}},
		{name: "site url", query: "charlie.example", want: []string{"Charlie"}},
		{name: "notes", query: "主力", want: []string{"Alpha"}},
		{name: "tags", query: "备用", want: []string{"Alpha"}},
		{name: "tags case insensitive", query: "Vip", want: []string{"Alpha", "Foxtrot"}},
		{name: "trimmed", query: "  golf  ", want: []string{"golf"}},
		{name: "blank means no filter", query: "   ", want: defaultChannelListOrder},
		{name: "matches every row", query: "EXAMPLE", want: defaultChannelListOrder},
		{name: "no match", query: "nomatch", want: nil},
		// 通配符与转义符本身都按字面量匹配，否则 "%" / "_" 会命中所有渠道。
		{name: "literal percent", query: "%", want: []string{"delta"}},
		{name: "literal underscore", query: "_", want: []string{"echo"}},
		{name: "literal escape char", query: "!", want: []string{"Foxtrot"}},
		{name: "underscore not wildcard", query: "v1_a", want: []string{"echo"}},
		{name: "percent not wildcard", query: "0%", want: []string{"delta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{Query: tc.query})
			assertChannelNames(t, tc.query, got, tc.want...)
			if total != int64(len(tc.want)) {
				t.Fatalf("total = %d, want %d", total, len(tc.want))
			}
		})
	}
}

func TestChannelListPageQueryNonASCII(t *testing.T) {
	channels := seedChannelListFixtures(t)
	c := Channel{Name: "Émile", Type: ChannelTypeNewAPI, SiteURL: "https://emile.example.com", Username: "u", PasswordCipher: "x", MonitorEnabled: true}
	if err := channels.Create(&c); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	// SQLite 的 LOWER() 不转换 É，模式串也必须原样保留 É 才能匹配。
	got, _ := listChannelNames(t, channels, 1, -1, ChannelListFilter{Query: "ÉMILE"})
	assertChannelNames(t, "non-ascii", got, "Émile")
}

func TestChannelListPageTag(t *testing.T) {
	channels := seedChannelListFixtures(t)

	cases := []struct {
		tag  string
		want []string
	}{
		// "a" 不能误中 "ab"，"b" 也不能误中 "ab"。
		{tag: "a", want: []string{"Charlie"}},
		{tag: "b", want: nil},
		{tag: "ab", want: []string{"bravo"}},
		{tag: "vip", want: []string{"Alpha", "Foxtrot"}},
		{tag: " VIP ", want: []string{"Alpha", "Foxtrot"}},
		{tag: "备用", want: []string{"Alpha"}},
		{tag: "%", want: nil},
		{tag: "nope", want: nil},
	}
	for _, tc := range cases {
		got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{Tag: tc.tag})
		assertChannelNames(t, "tag "+tc.tag, got, tc.want...)
		if total != int64(len(tc.want)) {
			t.Fatalf("tag %q total = %d, want %d", tc.tag, total, len(tc.want))
		}
	}
}

// TestChannelListPageTagNonASCIICase 标签只按 ASCII 忽略大小写：Ärger 与 ärger 是两个标签，
// 在 SQLite 上按其中一个筛选只命中对应的渠道。
//
// 只适用于 SQLite：MySQL 的 *_ci 排序规则在 LIKE 时还会折叠非 ASCII 大小写和重音，
// 两个都会命中（已接受的差异，见 ChannelListFilter），换成其它数据库时跳过。
func TestChannelListPageTagNonASCIICase(t *testing.T) {
	db := openTestDB(t)
	if !isSQLite(db) {
		t.Skipf("仅适用于 SQLite，当前为 %s", db.Dialector.Name())
	}
	channels := NewChannels(db)
	for _, c := range []Channel{
		{Name: "upper", Tags: ChannelTags{"Ärger"}},
		{Name: "lower", Tags: ChannelTags{"ärger"}},
		{Name: "both", Tags: ChannelTags{"ärger", "Ärger", "ÄRGER"}}, // ÄRGER 与 Ärger 只差 ASCII 大小写，去重
	} {
		c.Type = ChannelTypeNewAPI
		c.SiteURL = "https://" + c.Name + ".example.com"
		c.PasswordCipher = "x"
		if err := channels.Create(&c); err != nil {
			t.Fatalf("create channel %s: %v", c.Name, err)
		}
	}
	all, err := channels.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := all[2].Tags; !reflect.DeepEqual(got, ChannelTags{"ärger", "Ärger"}) {
		t.Fatalf("both tags = %#v, want [ärger Ärger]", got)
	}

	for tag, want := range map[string][]string{
		"Ärger": {"upper", "both"},
		"ärger": {"lower", "both"},
		"ÄRGER": {"upper", "both"}, // ASCII 部分仍忽略大小写
		"äRGER": {"lower", "both"},
		"arger": nil,
	} {
		got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{Tag: tag})
		assertChannelNames(t, "tag "+tag, got, want...)
		if total != int64(len(want)) {
			t.Fatalf("tag %q total = %d, want %d", tag, total, len(want))
		}
	}
}

func TestChannelListPageStatus(t *testing.T) {
	channels := seedChannelListFixtures(t)

	want := map[string][]string{
		ChannelStatusHealthy: {"Alpha", "echo", "Foxtrot"},
		ChannelStatusLow:     {"bravo"},
		ChannelStatusFailed:  {"Charlie", "golf"},
		ChannelStatusIdle:    {"delta"},
		ChannelStatusPaused:  {"bravo", "Foxtrot"},
	}
	all, err := channels.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for status, names := range want {
		got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{Status: status})
		assertChannelNames(t, "status "+status, got, names...)
		if total != int64(len(names)) {
			t.Fatalf("status %s total = %d, want %d", status, total, len(names))
		}

		// 与前端 statusOf() 的判断结果逐一比对。
		var mirrored []string
		for _, c := range all {
			if status == ChannelStatusPaused {
				if !c.MonitorEnabled {
					mirrored = append(mirrored, c.Name)
				}
			} else if statusOfForTest(c) == status {
				mirrored = append(mirrored, c.Name)
			}
		}
		assertChannelNames(t, "statusOf mirror "+status, got, mirrored...)
	}
}

func TestChannelListPageSort(t *testing.T) {
	channels := seedChannelListFixtures(t)

	cases := []struct {
		sort, order string
		want        []string
	}{
		// 名称 / 账号忽略大小写排序；SQLite 默认的 BINARY 排序会把大写排在前面。
		{ChannelSortName, ChannelOrderAsc, []string{"Alpha", "bravo", "Charlie", "delta", "echo", "Foxtrot", "golf"}},
		{ChannelSortName, "", []string{"Alpha", "bravo", "Charlie", "delta", "echo", "Foxtrot", "golf"}},
		{ChannelSortName, ChannelOrderDesc, []string{"golf", "Foxtrot", "echo", "delta", "Charlie", "bravo", "Alpha"}},
		// golf 的账号为空，无论升降序都排最后。
		{ChannelSortUsername, ChannelOrderAsc, []string{"Alpha", "bravo", "Charlie", "delta", "echo", "Foxtrot", "golf"}},
		{ChannelSortUsername, ChannelOrderDesc, []string{"Foxtrot", "echo", "delta", "Charlie", "bravo", "Alpha", "golf"}},
		// 同一健康档内按默认顺序（sort_order DESC, id ASC）排，方向不受 order 影响。
		{ChannelSortHealth, ChannelOrderAsc, []string{"Charlie", "golf", "bravo", "delta", "Alpha", "echo", "Foxtrot"}},
		{ChannelSortHealth, ChannelOrderDesc, []string{"Alpha", "echo", "Foxtrot", "delta", "bravo", "Charlie", "golf"}},
		// 未采集余额（NULL）无论升降序都排最后。
		{ChannelSortBalance, ChannelOrderAsc, []string{"echo", "bravo", "Foxtrot", "Charlie", "Alpha", "delta", "golf"}},
		{ChannelSortBalance, ChannelOrderDesc, []string{"Alpha", "Charlie", "Foxtrot", "bravo", "echo", "delta", "golf"}},
		{"HEALTH", "ASC", []string{"Charlie", "golf", "bravo", "delta", "Alpha", "echo", "Foxtrot"}},
	}
	for _, tc := range cases {
		got, _ := listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: tc.sort, Order: tc.order})
		assertChannelNames(t, tc.sort+" "+tc.order, got, tc.want...)
	}
}

// seedPinyinChannels 写入中文 / 英文混合名称的渠道，按 id 递增依次为：
//
//	名称       账号       排序
//	小米       ""         1
//	百度       bd@x       1
//	Zeta       Zed        1
//	腾讯       "  "       1   （账号只有空白，视为空）
//	阿里云     a@x        1
//	beta       YY@x       1
//	Alpha      yy@x       9   （账号与 beta 忽略大小写后相同，按 sort_order DESC 排在前面）
//	阿里巴巴   阿里@x     1
//
// 站点地址不含字母 a，方便用 q=a 只命中名称 / 账号。
func seedPinyinChannels(t *testing.T) *Channels {
	t.Helper()
	channels := NewChannels(openTestDB(t))
	for i, c := range []Channel{
		{Name: "小米", Username: "", SortOrder: 1},
		{Name: "百度", Username: "bd@x", SortOrder: 1},
		{Name: "Zeta", Username: "Zed", SortOrder: 1},
		{Name: "腾讯", Username: "  ", SortOrder: 1},
		{Name: "阿里云", Username: "a@x", SortOrder: 1},
		{Name: "beta", Username: "YY@x", SortOrder: 1},
		{Name: "Alpha", Username: "yy@x", SortOrder: 9},
		{Name: "阿里巴巴", Username: "阿里@x", SortOrder: 1},
	} {
		c.Type = ChannelTypeNewAPI
		c.SiteURL = fmt.Sprintf("https://ch%d.test", i)
		c.PasswordCipher = "x"
		if err := channels.Create(&c); err != nil {
			t.Fatalf("create channel %s: %v", c.Name, err)
		}
	}
	return channels
}

func TestChannelListPageSortPinyin(t *testing.T) {
	channels := seedPinyinChannels(t)

	// 与浏览器 localeCompare('zh-CN') 一致：中文在字母之前，按拼音排
	// （a li ba ba < a li yun < bai du < teng xun < xiao mi）；字母忽略大小写。
	// SQLite / MySQL 按码点排会得到 小米 < 百度 < 腾讯 < 阿里……
	got, total := listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: ChannelSortName})
	assertChannelNames(t, "name asc", got, "阿里巴巴", "阿里云", "百度", "腾讯", "小米", "Alpha", "beta", "Zeta")
	if total != 8 {
		t.Fatalf("name asc total = %d, want 8", total)
	}
	got, _ = listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: ChannelSortName, Order: ChannelOrderDesc})
	assertChannelNames(t, "name desc", got, "Zeta", "beta", "Alpha", "小米", "腾讯", "百度", "阿里云", "阿里巴巴")

	// 账号为空（含只有空白）的无论升降序都排最后，彼此之间按默认顺序；
	// 降序只翻转主比较，账号相同的 Alpha / beta 仍按 sort_order DESC 排。
	got, _ = listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: ChannelSortUsername})
	assertChannelNames(t, "username asc", got, "阿里巴巴", "阿里云", "百度", "Alpha", "beta", "Zeta", "小米", "腾讯")
	got, _ = listChannelNames(t, channels, 1, -1, ChannelListFilter{Sort: ChannelSortUsername, Order: ChannelOrderDesc})
	assertChannelNames(t, "username desc", got, "Zeta", "Alpha", "beta", "百度", "阿里云", "阿里巴巴", "小米", "腾讯")
}

// TestChannelTextCollatorMatchesBrowser 逐一对照浏览器的排序结果。
// 期望顺序取自 Node 24（ICU 78）与 Chrome 的 [...].sort((a, b) => a.localeCompare(b, 'zh-CN'))：
// 数字符号 → 中文（拼音）→ 拉丁字母 → 其它文字；"A阿" 排在 "Ab" 之前说明分组是逐字符比较的。
func TestChannelTextCollatorMatchesBrowser(t *testing.T) {
	want := []string{
		"_x", "123渠道", "9号",
		"阿里巴巴", "阿里云", "百度", "腾讯", "小米",
		"A阿", "Ab", "ac", "Alpha", "arger", "Ärger", "b", "beta", "éa", "e\u0301b", "Zeta",
		"Ω希腊", "가나", "ア", "ㄅㄆ",
	}
	got := append([]string(nil), want...)
	for i := range got { // 打乱成逆序，确保不是原样保留
		j := len(got) - 1 - i
		if i >= j {
			break
		}
		got[i], got[j] = got[j], got[i]
	}
	col := newChannelTextCollator()
	keys := make(map[string]channelTextKey, len(got))
	for _, s := range got {
		keys[s] = col.key(s)
	}
	sort.SliceStable(got, func(i, j int) bool { return compareChannelTextKeys(keys[got[i]], keys[got[j]]) < 0 })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order mismatch\n got: %q\nwant: %q", got, want)
	}

	// 大小写不同视为相同，交给调用方按默认顺序决定先后。
	if c := compareChannelTextKeys(col.key("Alpha"), col.key("alpha")); c != 0 {
		t.Fatalf("Alpha vs alpha = %d, want 0", c)
	}
	// 重音只在逐字符都相同时才起作用。
	if c := compareChannelTextKeys(col.key("arger"), col.key("Ärger")); c >= 0 {
		t.Fatalf("arger vs Ärger = %d, want < 0", c)
	}
}

func TestChannelListPageSortPinyinPagination(t *testing.T) {
	channels := seedPinyinChannels(t)
	byName := ChannelListFilter{Sort: ChannelSortName}

	for i, want := range [][]string{
		{"阿里巴巴", "阿里云", "百度"},
		{"腾讯", "小米", "Alpha"},
		{"beta", "Zeta"},
		nil, // 页码超出范围：空列表，total 不变
		nil,
	} {
		got, total := listChannelNames(t, channels, i+1, 3, byName)
		assertChannelNames(t, fmt.Sprintf("name page %d", i+1), got, want...)
		if total != 8 {
			t.Fatalf("name page %d total = %d, want 8", i+1, total)
		}
	}

	// 内存排序同样先按条件筛选，total 是筛选后的数量。
	filtered := ChannelListFilter{Query: "A", Sort: ChannelSortUsername, Order: ChannelOrderDesc}
	for i, want := range [][]string{
		{"Zeta", "Alpha"},
		{"beta", "阿里云"},
		nil,
	} {
		got, total := listChannelNames(t, channels, i+1, 2, filtered)
		assertChannelNames(t, fmt.Sprintf("filtered page %d", i+1), got, want...)
		if total != 4 {
			t.Fatalf("filtered page %d total = %d, want 4", i+1, total)
		}
	}

	got, total := listChannelNames(t, channels, 1, 5, ChannelListFilter{Query: "nomatch", Sort: ChannelSortName})
	assertChannelNames(t, "no match", got)
	if total != 0 {
		t.Fatalf("no match total = %d, want 0", total)
	}
}

func TestChannelListPageFiltersWithPagination(t *testing.T) {
	channels := seedChannelListFixtures(t)

	healthy := ChannelListFilter{Status: ChannelStatusHealthy}
	got, total := listChannelNames(t, channels, 1, 2, healthy)
	assertChannelNames(t, "healthy p1", got, "Alpha", "echo")
	if total != 3 {
		t.Fatalf("healthy p1 total = %d, want 3", total)
	}
	got, total = listChannelNames(t, channels, 2, 2, healthy)
	assertChannelNames(t, "healthy p2", got, "Foxtrot")
	if total != 3 {
		t.Fatalf("healthy p2 total = %d, want 3", total)
	}
	got, total = listChannelNames(t, channels, 3, 2, healthy)
	assertChannelNames(t, "healthy p3", got)
	if total != 3 {
		t.Fatalf("healthy p3 total = %d, want 3", total)
	}

	combo := ChannelListFilter{Query: "example", Tag: "vip", Sort: ChannelSortName, Order: ChannelOrderDesc}
	got, total = listChannelNames(t, channels, 1, 1, combo)
	assertChannelNames(t, "combo p1", got, "Foxtrot")
	if total != 2 {
		t.Fatalf("combo total = %d, want 2", total)
	}
	got, _ = listChannelNames(t, channels, 2, 1, combo)
	assertChannelNames(t, "combo p2", got, "Alpha")

	got, total = listChannelNames(t, channels, 1, 20, ChannelListFilter{Status: ChannelStatusPaused, Tag: "VIP"})
	assertChannelNames(t, "paused+tag", got, "Foxtrot")
	if total != 1 {
		t.Fatalf("paused+tag total = %d, want 1", total)
	}

	got, total = listChannelNames(t, channels, 1, -1, ChannelListFilter{Status: ChannelStatusFailed, Sort: ChannelSortBalance, Order: ChannelOrderDesc})
	assertChannelNames(t, "failed all by balance", got, "Charlie", "golf")
	if total != 2 {
		t.Fatalf("failed all total = %d, want 2", total)
	}

	got, total = listChannelNames(t, channels, 1, 9, ChannelListFilter{Status: ChannelStatusLow, Query: "alpha"})
	assertChannelNames(t, "empty result", got)
	if total != 0 {
		t.Fatalf("empty result total = %d, want 0", total)
	}
}

func TestChannelListFilterNormalize(t *testing.T) {
	got, err := ChannelListFilter{Query: "  a b ", Status: " Healthy ", Tag: " vip ", Sort: " NAME ", Order: "DESC"}.Normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	want := ChannelListFilter{Query: "a b", Status: ChannelStatusHealthy, Tag: "vip", Sort: ChannelSortName, Order: ChannelOrderDesc}
	if got != want {
		t.Fatalf("normalize = %+v, want %+v", got, want)
	}

	got, err = ChannelListFilter{}.Normalize()
	if err != nil {
		t.Fatalf("normalize zero: %v", err)
	}
	if got != (ChannelListFilter{Sort: ChannelSortDefault, Order: ChannelOrderAsc}) {
		t.Fatalf("normalize zero = %+v", got)
	}

	channels := seedChannelListFixtures(t)
	for _, bad := range []ChannelListFilter{
		{Status: "broken"},
		{Status: "all"},
		{Sort: "name; DROP TABLE channels"},
		{Sort: "sort_order"},
		{Order: "up"},
		{Tag: "a,b"},
		{Tag: "a，b"},
		{Query: strings.Repeat("a", MaxChannelListQueryRunes+1)},
		{Query: strings.Repeat("a", 50001)},
		{Tag: strings.Repeat("a", MaxChannelTagRunes+1)},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Fatalf("Normalize(%+v) error = nil, want error", bad)
		}
		if _, _, err := channels.ListPage(1, 10, bad); err == nil {
			t.Fatalf("ListPage(%+v) error = nil, want error", bad)
		}
	}
}

func TestChannelListFilterNormalizeLength(t *testing.T) {
	cases := []struct {
		name    string
		filter  ChannelListFilter
		wantErr string // 空表示应通过
	}{
		{name: "query at limit", filter: ChannelListFilter{Query: strings.Repeat("a", MaxChannelListQueryRunes)}},
		{name: "query limit counts runes", filter: ChannelListFilter{Query: strings.Repeat("中", MaxChannelListQueryRunes)}},
		{name: "query trimmed before counting", filter: ChannelListFilter{Query: "  " + strings.Repeat("a", MaxChannelListQueryRunes) + "\t"}},
		{name: "query over limit", filter: ChannelListFilter{Query: strings.Repeat("中", MaxChannelListQueryRunes+1)}, wantErr: "搜索关键词最多 200 个字符"},
		// 超过 SQLite LIKE 模式 50000 字节上限的输入必须在这里挡住，而不是变成 500。
		{name: "query beyond sqlite like limit", filter: ChannelListFilter{Query: strings.Repeat("a", 50001)}, wantErr: "搜索关键词最多 200 个字符"},
		{name: "tag at limit", filter: ChannelListFilter{Tag: strings.Repeat("标", MaxChannelTagRunes)}},
		{name: "tag over limit", filter: ChannelListFilter{Tag: strings.Repeat("标", MaxChannelTagRunes+1)}, wantErr: "tag 最多 32 个字符"},
		{name: "tag trimmed before counting", filter: ChannelListFilter{Tag: " " + strings.Repeat("a", MaxChannelTagRunes) + " "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.filter.Normalize()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Normalize error = %v, want nil", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("Normalize error = nil, want %q", tc.wantErr)
			case tc.wantErr != "" && err.Error() != tc.wantErr:
				t.Fatalf("Normalize error = %q, want %q", err, tc.wantErr)
			}
		})
	}

	// 上限以内的超长关键词在 SQLite 上也能正常查询（只是没有匹配）。
	channels := seedChannelListFixtures(t)
	got, total := listChannelNames(t, channels, 1, 9, ChannelListFilter{Query: strings.Repeat("%", MaxChannelListQueryRunes)})
	assertChannelNames(t, "long query", got)
	if total != 0 {
		t.Fatalf("long query total = %d, want 0", total)
	}
}
