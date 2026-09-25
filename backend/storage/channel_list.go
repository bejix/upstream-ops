package storage

import (
	"bytes"
	"cmp"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"gorm.io/gorm"
)

// 渠道列表的状态筛选值。除 paused 外与前端 channel-cards.tsx 的 statusOf() 一一对应，
// 判断顺序也一致：先看 last_error，再看是否采集过余额，最后比较阈值。
const (
	ChannelStatusHealthy = "healthy" // 无错误、已采集余额，且未低于阈值
	ChannelStatusLow     = "low"     // 无错误、已采集余额，设置了阈值且余额低于阈值
	ChannelStatusFailed  = "failed"  // last_error 非空
	ChannelStatusIdle    = "idle"    // 无错误、尚未采集到余额
	ChannelStatusPaused  = "paused"  // 已暂停监控，与上面四种健康状态正交
)

// 渠道列表的排序字段。
//
// name / username 需要按拼音排中文，SQLite / MySQL 的默认排序规则都做不到（只按码点），
// 所以这两种排序在 Go 里做（见 sortChannelsByText）；其余排序仍由 SQL 完成。
const (
	ChannelSortDefault  = "default"  // sort_order DESC, id ASC（原有顺序），忽略 Order
	ChannelSortName     = "name"     // 名称，数字符号 → 中文（按拼音）→ 字母（忽略大小写），同前端 localeCompare('zh-CN')（见 channelTextCollator）
	ChannelSortUsername = "username" // 账号，规则同名称；空账号无论升降序都排在最后
	ChannelSortHealth   = "health"   // 升序：登录失败 → 低余额 → 尚未采集 → 健康
	ChannelSortBalance  = "balance"  // 余额；未采集余额的渠道无论升降序都排在最后
)

// 渠道列表的排序方向。
const (
	ChannelOrderAsc  = "asc"
	ChannelOrderDesc = "desc"
)

// MaxChannelListQueryRunes 搜索关键词的长度上限。
// 同时保证转义后的 LIKE 模式远小于 SQLite 50000 字节的上限，避免超长输入变成 500。
const MaxChannelListQueryRunes = 200

// ChannelListFilter 渠道分页列表的搜索、筛选与排序条件；零值表示不筛选、按默认顺序。
//
// 各字段都来自用户输入：Query / Tag 只作为 LIKE 参数绑定；Status / Sort / Order
// 只用于从白名单里挑选预先写好的 SQL 片段，绝不拼接进 SQL。
//
// Query / Tag 的大小写只折叠 ASCII 字母（见 lowerASCII）。MySQL 默认的 *_ci 排序规则
// 在 LIKE 比较时还会忽略其它字母的大小写、重音和全半角（如 tag=Ärger 也会命中 ärger），
// 这个差异可以接受，不做特殊处理。
type ChannelListFilter struct {
	Query  string // 名称 / 账号 / 站点地址 / 备注 / 标签的子串搜索，最多 MaxChannelListQueryRunes 个字符
	Status string // ChannelStatus*，空表示不限
	Tag    string // 单个标签，精确匹配，最多 MaxChannelTagRunes 个字符
	Sort   string // ChannelSort*，空表示默认顺序
	Order  string // ChannelOrder*，空表示升序
}

// likeEscaper 转义 LIKE 模式里的通配符，配合 ESCAPE '!' 使用。
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

const (
	channelHasErrorSQL = "COALESCE(last_error, '') <> ''"
	channelNoErrorSQL  = "COALESCE(last_error, '') = ''"
	// 低余额：设置了阈值（> 0）且余额低于阈值。调用方需先保证 last_balance IS NOT NULL，
	// 这样整个表达式不会得到 NULL，NOT (...) 才能正确取反。
	channelBelowThresholdSQL = "COALESCE(balance_threshold, 0) > 0 AND last_balance < balance_threshold"
	// 健康状态排序权重：登录失败 0、低余额 1、尚未采集 2、健康 3。
	channelHealthRankSQL = "CASE WHEN " + channelHasErrorSQL + " THEN 0" +
		" WHEN last_balance IS NULL THEN 2" +
		" WHEN " + channelBelowThresholdSQL + " THEN 1" +
		" ELSE 3 END"
)

// channelStatusScopes 各状态对应的 WHERE 条件。
var channelStatusScopes = map[string]func(*gorm.DB) *gorm.DB{
	ChannelStatusFailed: func(q *gorm.DB) *gorm.DB { return q.Where(channelHasErrorSQL) },
	ChannelStatusIdle: func(q *gorm.DB) *gorm.DB {
		return q.Where(channelNoErrorSQL + " AND last_balance IS NULL")
	},
	ChannelStatusLow: func(q *gorm.DB) *gorm.DB {
		return q.Where(channelNoErrorSQL + " AND last_balance IS NOT NULL AND " + channelBelowThresholdSQL)
	},
	ChannelStatusHealthy: func(q *gorm.DB) *gorm.DB {
		return q.Where(channelNoErrorSQL + " AND last_balance IS NOT NULL AND NOT (" + channelBelowThresholdSQL + ")")
	},
	// 前端按 !monitor_enabled 判断，NULL 也算暂停。
	ChannelStatusPaused: func(q *gorm.DB) *gorm.DB {
		return q.Where("(monitor_enabled = ? OR monitor_enabled IS NULL)", false)
	},
}

// Normalize 去掉首尾空白、统一取值大小写并补全默认值。
// 含未知取值或 Query / Tag 超长时返回可以直接展示给用户的错误。
func (f ChannelListFilter) Normalize() (ChannelListFilter, error) {
	f.Query = strings.TrimSpace(f.Query)
	f.Tag = strings.TrimSpace(f.Tag)
	f.Status = strings.ToLower(strings.TrimSpace(f.Status))
	f.Sort = strings.ToLower(strings.TrimSpace(f.Sort))
	f.Order = strings.ToLower(strings.TrimSpace(f.Order))

	if f.Status != "" {
		if _, ok := channelStatusScopes[f.Status]; !ok {
			return f, fmt.Errorf("status 仅支持 healthy、low、failed、idle、paused")
		}
	}
	if utf8.RuneCountInString(f.Query) > MaxChannelListQueryRunes {
		return f, fmt.Errorf("搜索关键词最多 %d 个字符", MaxChannelListQueryRunes)
	}
	if strings.ContainsFunc(f.Tag, isChannelTagSeparator) {
		return f, fmt.Errorf("tag 只能是单个标签，不能包含逗号")
	}
	// 超过上限的标签根本存不进来，直接报错而不是静默返回空列表。
	if utf8.RuneCountInString(f.Tag) > MaxChannelTagRunes {
		return f, fmt.Errorf("tag 最多 %d 个字符", MaxChannelTagRunes)
	}
	switch f.Sort {
	case "":
		f.Sort = ChannelSortDefault
	case ChannelSortDefault, ChannelSortName, ChannelSortUsername, ChannelSortHealth, ChannelSortBalance:
	default:
		return f, fmt.Errorf("sort 仅支持 default、name、username、health、balance")
	}
	switch f.Order {
	case "":
		f.Order = ChannelOrderAsc
	case ChannelOrderAsc, ChannelOrderDesc:
	default:
		return f, fmt.Errorf("order 仅支持 asc 或 desc")
	}
	return f, nil
}

// lowerASCII 只把 ASCII 字母转成小写。
// SQLite 的 LOWER() 只处理 ASCII，模式串若用 strings.ToLower 把 É 转成 é，
// 反而和 LOWER(列) 里原样保留的 É 对不上。标签去重（NormalizeChannelTags）也用它，
// 保证"去重后算一个标签"和"按标签筛选能命中"的口径一致。
func lowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// applyChannelListFilter 追加搜索 / 标签 / 状态条件，f 需已 Normalize。
// 统计总数和查询分页都要走这里，保证 total 与列表口径一致。
func applyChannelListFilter(q *gorm.DB, f ChannelListFilter) *gorm.DB {
	if f.Query != "" {
		p := "%" + likeEscaper.Replace(lowerASCII(f.Query)) + "%"
		q = q.Where("(LOWER(name) LIKE ? ESCAPE '!'"+
			" OR LOWER(username) LIKE ? ESCAPE '!'"+
			" OR LOWER(site_url) LIKE ? ESCAPE '!'"+
			" OR LOWER(COALESCE(notes, '')) LIKE ? ESCAPE '!'"+
			" OR LOWER(COALESCE(tags, '')) LIKE ? ESCAPE '!')", p, p, p, p, p)
	}
	if f.Tag != "" {
		// tags 以 ",a,b," 形式存储（见 ChannelTags），首尾带分隔符才能精确匹配单个标签，
		// 避免 "a" 误中 "ab"。在 SQLite 上只折叠 ASCII 大小写，与标签去重规则一致；
		// MySQL 的 *_ci 排序规则会折叠得更宽（见 ChannelListFilter）。
		q = q.Where("LOWER(COALESCE(tags, '')) LIKE ? ESCAPE '!'", "%,"+likeEscaper.Replace(lowerASCII(f.Tag))+",%")
	}
	if scope, ok := channelStatusScopes[f.Status]; ok {
		q = scope(q)
	}
	return q
}

// applyChannelListOrder 追加排序，f 需已 Normalize。name / username 不走这里（见 sortChannelsByText）。
// 方向只取自常量；最后总是补上默认顺序作为稳定的次级排序，保证翻页不重不漏。
func applyChannelListOrder(q *gorm.DB, f ChannelListFilter) *gorm.DB {
	dir := "ASC"
	if f.Order == ChannelOrderDesc {
		dir = "DESC"
	}
	switch f.Sort {
	case ChannelSortHealth:
		q = q.Order(channelHealthRankSQL + " " + dir)
	case ChannelSortBalance:
		q = q.Order("CASE WHEN last_balance IS NULL THEN 1 ELSE 0 END ASC").Order("last_balance " + dir)
	}
	return q.Order("sort_order DESC").Order("id ASC")
}

// sortsChannelsInMemory 该排序是否需要把筛选结果全部读出后在 Go 里排序。
func (f ChannelListFilter) sortsChannelsInMemory() bool {
	return f.Sort == ChannelSortName || f.Sort == ChannelSortUsername
}

// sortChannelsByText 按名称或账号原地排序，f 需已 Normalize 且 Sort 为 name / username。
//
// 顺序与前端 localeCompare('zh-CN')（分组弹窗的"渠道 A-Z"、标签选项）一致：数字符号在前，
// 然后是按拼音排的中文，最后是忽略大小写的字母；实现与已知差异见 channelTextCollator。
// Order 只翻转主比较；空账号（去掉空白后为空）无论升降序都排在最后；主比较相同时按默认顺序
// （sort_order DESC, id ASC）。
func sortChannelsByText(list []Channel, f ChannelListFilter) {
	col := newChannelTextCollator()
	type entry struct {
		ch    Channel
		key   channelTextKey
		blank bool
	}
	entries := make([]entry, len(list))
	for i := range list {
		text := list[i].Name
		if f.Sort == ChannelSortUsername {
			text = list[i].Username
		}
		text = strings.TrimSpace(text)
		entries[i] = entry{ch: list[i], key: col.key(text), blank: text == ""}
	}
	desc := f.Order == ChannelOrderDesc
	blankLast := f.Sort == ChannelSortUsername
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := &entries[i], &entries[j]
		if blankLast && a.blank != b.blank {
			return b.blank
		}
		if c := compareChannelTextKeys(a.key, b.key); c != 0 {
			if desc {
				return c > 0
			}
			return c < 0
		}
		if a.ch.SortOrder != b.ch.SortOrder {
			return a.ch.SortOrder > b.ch.SortOrder
		}
		return a.ch.ID < b.ch.ID
	})
	for i := range entries {
		list[i] = entries[i].ch
	}
}

// channelTextCollator 生成名称 / 账号的排序键，模拟浏览器 ICU 的 zh-CN 排序规则。
//
// golang.org/x/text/collate 的中文规则（CLDR 23）能按拼音排中文，但不会像浏览器 ICU 那样
// 调整脚本顺序：它把汉字排在所有字母文字之后，而浏览器把汉字排在字母之前、数字符号之后；
// x/text 的 collate.Reorder 也尚未实现。所以这里逐字符比较，先比脚本分组
// （见 channelTextGroup），分组相同再比 x/text 的主权重（不区分大小写、重音、全半角），
// 这样 "A阿" 也和浏览器一样排在 "Ab" 之前。逐字符全部相同时再比整串的排序键，区分重音。
//
// 已知差异：少数多音词的读音与新版 ICU 不同（如 ICU 把"重庆"按 chong 排，这里按 zhong），
// 字母的大小写不区分（ICU 会把小写排在前面）。
//
// Collator 与 Buffer 不能并发使用，每次排序新建一个。
type channelTextCollator struct {
	primary *collate.Collator // collate.Loose：排序键只剩主权重
	full    *collate.Collator
	buf     collate.Buffer
}

// channelTextKey 一段文本的排序键，用 compareChannelTextKeys 比较。
type channelTextKey struct {
	units []channelTextUnit // 逐字符的分组与主权重，跳过没有主权重的字符（如组合附加符号）
	full  []byte            // 整串的排序键（忽略大小写）
}

type channelTextUnit struct {
	group   uint8
	primary []byte
}

func newChannelTextCollator() *channelTextCollator {
	return &channelTextCollator{
		primary: collate.New(language.Chinese, collate.Loose),
		full:    collate.New(language.Chinese, collate.IgnoreCase),
	}
}

// key 生成 s 的排序键。返回的切片引用 c.buf，c 用完之前一直有效。
func (c *channelTextCollator) key(s string) channelTextKey {
	k := channelTextKey{full: c.full.KeyFromString(&c.buf, s)}
	for _, r := range s {
		p := c.primary.KeyFromString(&c.buf, string(r))
		if len(p) == 0 {
			continue
		}
		k.units = append(k.units, channelTextUnit{group: channelTextGroup(r), primary: p})
	}
	return k
}

// channelTextGroup 字符的脚本分组：数字、标点、符号等为 0，汉字为 1，其它文字为 2，
// 注音符号为 3（浏览器 ICU 把注音排在所有文字之后）。分组之内的相对顺序与 x/text 的默认顺序一致。
func channelTextGroup(r rune) uint8 {
	switch {
	case unicode.Is(unicode.Han, r):
		return 1
	case unicode.Is(unicode.Bopomofo, r):
		return 3
	case unicode.IsLetter(r):
		return 2
	default:
		return 0
	}
}

// compareChannelTextKeys 比较两个排序键，返回 -1 / 0 / 1。
func compareChannelTextKeys(a, b channelTextKey) int {
	for i := 0; i < len(a.units) && i < len(b.units); i++ {
		ua, ub := a.units[i], b.units[i]
		if ua.group != ub.group {
			return cmp.Compare(ua.group, ub.group)
		}
		if c := bytes.Compare(ua.primary, ub.primary); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(len(a.units), len(b.units)); c != 0 {
		return c
	}
	return bytes.Compare(a.full, b.full)
}

// pageChannels 取出第 page 页（从 1 开始），pageSize 为 -1 时返回全部；
// 页码超出范围时返回空列表，与 SQL 分页的结果一致。
func pageChannels(list []Channel, page, pageSize int) []Channel {
	if pageSize == -1 {
		return list
	}
	if page-1 >= (len(list)+pageSize-1)/pageSize {
		return []Channel{}
	}
	start := (page - 1) * pageSize
	return list[start:min(start+pageSize, len(list))]
}
