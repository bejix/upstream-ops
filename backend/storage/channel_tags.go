package storage

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// 渠道标签 / 备注的长度上限，由 API 层校验。
// tags 列为 size:1024，按上限最多占用 1 + 20*(32+1) = 661 个字符。
const (
	MaxChannelTags       = 20
	MaxChannelTagRunes   = 32
	MaxChannelNotesRunes = 2000
)

// ChannelTags 渠道自定义标签。
//
// 数据库中保存为首尾都带分隔符的字符串：无标签为 ""，否则形如 ",vip,备用,"。
// 这样按单个标签精确筛选时可以用 SQLite / MySQL 通用的
// tags LIKE '%,<转义后的标签>,%' ESCAPE '!'，不依赖各自的 JSON 函数。
// 写库前会先 NormalizeChannelTags，保证单个标签内不含分隔符。
//
// JSON 中始终是字符串数组，无标签时输出 []（不输出 null）。
type ChannelTags []string

// NormalizeChannelTags 规范化标签列表：
//   - 含半角 "," 或全角 "，" 的元素拆成多个标签
//   - 去掉首尾空白，丢弃空标签
//   - 只忽略 ASCII 字母大小写去重（"VIP" 与 "vip" 算同一个，"Ärger" 与 "ärger" 不算），
//     保留首次出现的写法与原始顺序；与按标签筛选时 SQLite LOWER() 的折叠范围一致
//
// 结果为空时返回 nil。
func NormalizeChannelTags(tags []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		for _, part := range strings.FieldsFunc(raw, isChannelTagSeparator) {
			tag := strings.TrimSpace(part)
			if tag == "" {
				continue
			}
			key := lowerASCII(tag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}

func isChannelTagSeparator(r rune) bool { return r == ',' || r == '，' }

// Value 实现 driver.Valuer：规范化后写成 ",a,b," 形式，无标签写 ""。
func (t ChannelTags) Value() (driver.Value, error) {
	tags := NormalizeChannelTags(t)
	if len(tags) == 0 {
		return "", nil
	}
	return "," + strings.Join(tags, ",") + ",", nil
}

// Scan 实现 sql.Scanner：兼容 NULL、""、[]byte / string，以及未带首尾分隔符的旧格式（"a,b"）。
func (t *ChannelTags) Scan(src any) error {
	var raw string
	switch v := src.(type) {
	case nil:
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("scan ChannelTags: unsupported type %T", src)
	}
	*t = NormalizeChannelTags([]string{raw})
	return nil
}

// MarshalJSON 无标签时输出 []，前端无需再判断 null。
func (t ChannelTags) MarshalJSON() ([]byte, error) {
	if len(t) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(t))
}

// UnmarshalJSON 接受字符串数组，null 视为无标签。
func (t *ChannelTags) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*t = list
	return nil
}
