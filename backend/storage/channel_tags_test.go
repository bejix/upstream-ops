package storage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeChannelTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "all empty", in: []string{"", "  ", "\t", "，", ",,"}, want: nil},
		{name: "trim", in: []string{"  vip  ", "\t备用\n"}, want: []string{"vip", "备用"}},
		{name: "full width space trimmed", in: []string{"　高速　"}, want: []string{"高速"}},
		{name: "split half and full width comma", in: []string{"a,b", "c，d", " e , ，f "}, want: []string{"a", "b", "c", "d", "e", "f"}},
		{name: "wrapped storage form", in: []string{",vip,备用,"}, want: []string{"vip", "备用"}},
		{name: "dedupe case insensitive keeps first", in: []string{"VIP", "b", "vip", "Vip", "B"}, want: []string{"VIP", "b"}},
		// 只折叠 ASCII 大小写，与 SQLite LOWER() 一致；否则去重后只剩一个标签，按它筛选却只能命中一半渠道。
		{name: "non-ascii case kept apart", in: []string{"Ärger", "ärger"}, want: []string{"Ärger", "ärger"}},
		{name: "ascii letters still folded", in: []string{"Ärger", "ÄRGER", "ärger", "ärGER"}, want: []string{"Ärger", "ärger"}},
		{name: "dedupe across split parts", in: []string{"a,A", "a"}, want: []string{"a"}},
		{name: "keeps order", in: []string{"z", "a", "m"}, want: []string{"z", "a", "m"}},
		{name: "inner spaces kept", in: []string{" 主 力 "}, want: []string{"主 力"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeChannelTags(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NormalizeChannelTags(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeChannelTagsDoesNotMutateInput(t *testing.T) {
	in := []string{" a ", "b,c"}
	_ = NormalizeChannelTags(in)
	if !reflect.DeepEqual(in, []string{" a ", "b,c"}) {
		t.Fatalf("input mutated: %#v", in)
	}
}

func TestChannelTagsValue(t *testing.T) {
	cases := []struct {
		name string
		in   ChannelTags
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "empty", in: ChannelTags{}, want: ""},
		{name: "blank only", in: ChannelTags{" ", ""}, want: ""},
		{name: "single", in: ChannelTags{"vip"}, want: ",vip,"},
		{name: "multiple", in: ChannelTags{"vip", "备用"}, want: ",vip,备用,"},
		{name: "normalized before write", in: ChannelTags{" a ", "A", "b,c", "d，e"}, want: ",a,b,c,d,e,"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.in.Value()
			if err != nil {
				t.Fatalf("Value() error: %v", err)
			}
			s, ok := v.(string)
			if !ok {
				t.Fatalf("Value() type = %T, want string", v)
			}
			if s != tc.want {
				t.Fatalf("Value() = %q, want %q", s, tc.want)
			}
		})
	}
}

func TestChannelTagsScan(t *testing.T) {
	cases := []struct {
		name string
		src  any
		want ChannelTags
	}{
		{name: "NULL", src: nil, want: nil},
		{name: "empty string", src: "", want: nil},
		{name: "only delimiters", src: ",,", want: nil},
		{name: "wrapped string", src: ",vip,备用,", want: ChannelTags{"vip", "备用"}},
		{name: "wrapped bytes", src: []byte(",a,b,"), want: ChannelTags{"a", "b"}},
		{name: "legacy unwrapped", src: "a, b", want: ChannelTags{"a", "b"}},
		{name: "legacy single", src: "vip", want: ChannelTags{"vip"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 预置旧值，确认 Scan 会整体覆盖而不是追加
			got := ChannelTags{"stale"}
			if err := got.Scan(tc.src); err != nil {
				t.Fatalf("Scan(%#v) error: %v", tc.src, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Scan(%#v) = %#v, want %#v", tc.src, got, tc.want)
			}
		})
	}

	var tags ChannelTags
	if err := tags.Scan(int64(1)); err == nil {
		t.Fatal("Scan(int64) error = nil, want unsupported type error")
	}
}

func TestChannelTagsValueScanRoundTrip(t *testing.T) {
	for _, in := range []ChannelTags{nil, {"vip"}, {"vip", "备用", "a b"}} {
		v, err := in.Value()
		if err != nil {
			t.Fatalf("Value(%#v) error: %v", in, err)
		}
		var out ChannelTags
		if err := out.Scan(v); err != nil {
			t.Fatalf("Scan(%#v) error: %v", v, err)
		}
		if len(in) == 0 {
			if len(out) != 0 {
				t.Fatalf("round trip of %#v = %#v, want empty", in, out)
			}
			continue
		}
		if !reflect.DeepEqual(out, in) {
			t.Fatalf("round trip of %#v = %#v", in, out)
		}
	}
}

func TestChannelTagsJSON(t *testing.T) {
	for _, tc := range []struct {
		in   ChannelTags
		want string
	}{
		{in: nil, want: "[]"},
		{in: ChannelTags{}, want: "[]"},
		{in: ChannelTags{"vip", "备用"}, want: `["vip","备用"]`},
	} {
		b, err := json.Marshal(tc.in)
		if err != nil {
			t.Fatalf("marshal %#v: %v", tc.in, err)
		}
		if string(b) != tc.want {
			t.Fatalf("marshal %#v = %s, want %s", tc.in, b, tc.want)
		}
	}

	// 作为结构体字段（nil 切片）时同样输出 []
	b, err := json.Marshal(Channel{Name: "x"})
	if err != nil {
		t.Fatalf("marshal channel: %v", err)
	}
	if !strings.Contains(string(b), `"tags":[]`) {
		t.Fatalf("channel json = %s, want \"tags\":[]", b)
	}
	if !strings.Contains(string(b), `"notes":""`) {
		t.Fatalf("channel json = %s, want \"notes\":\"\"", b)
	}

	for _, tc := range []struct {
		in   string
		want ChannelTags
	}{
		{in: `null`, want: nil},
		{in: `[]`, want: ChannelTags{}},
		{in: `["a","备用"]`, want: ChannelTags{"a", "备用"}},
	} {
		got := ChannelTags{"stale"}
		if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.in, err)
		}
		if len(tc.want) == 0 {
			if len(got) != 0 {
				t.Fatalf("unmarshal %s = %#v, want empty", tc.in, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("unmarshal %s = %#v, want %#v", tc.in, got, tc.want)
		}
	}

	var bad ChannelTags
	if err := json.Unmarshal([]byte(`"vip"`), &bad); err == nil {
		t.Fatal("unmarshal string error = nil, want error")
	}

	var ch Channel
	if err := json.Unmarshal([]byte(`{"name":"x","tags":null,"notes":"n"}`), &ch); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	if len(ch.Tags) != 0 || ch.Notes != "n" {
		t.Fatalf("unmarshal channel = tags %#v notes %q", ch.Tags, ch.Notes)
	}
}

func rawChannelTags(t *testing.T, channels *Channels, id uint) string {
	t.Helper()
	var raw string
	if err := channels.db.Raw("SELECT tags FROM channels WHERE id = ?", id).Scan(&raw).Error; err != nil {
		t.Fatalf("select raw tags: %v", err)
	}
	return raw
}

func TestChannelTagsAndNotesPersist(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)

	tagged := &Channel{
		Name:           "tagged",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		Tags:           ChannelTags{"vip", "备用"},
		Notes:          "主力渠道\n晚高峰慢",
	}
	if err := channels.Create(tagged); err != nil {
		t.Fatalf("create tagged: %v", err)
	}
	if raw := rawChannelTags(t, channels, tagged.ID); raw != ",vip,备用," {
		t.Fatalf("raw tags = %q, want %q", raw, ",vip,备用,")
	}
	got, err := channels.FindByID(tagged.ID)
	if err != nil {
		t.Fatalf("find tagged: %v", err)
	}
	if !reflect.DeepEqual(got.Tags, ChannelTags{"vip", "备用"}) {
		t.Fatalf("tags = %#v", got.Tags)
	}
	if got.Notes != "主力渠道\n晚高峰慢" {
		t.Fatalf("notes = %q", got.Notes)
	}

	plain := &Channel{
		Name:           "plain",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
	}
	if err := channels.Create(plain); err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if raw := rawChannelTags(t, channels, plain.ID); raw != "" {
		t.Fatalf("plain raw tags = %q, want empty", raw)
	}
	got, err = channels.FindByID(plain.ID)
	if err != nil {
		t.Fatalf("find plain: %v", err)
	}
	if len(got.Tags) != 0 || got.Notes != "" {
		t.Fatalf("plain tags = %#v notes = %q, want empty", got.Tags, got.Notes)
	}

	// 整行 Save 清空标签与备注
	got, err = channels.FindByID(tagged.ID)
	if err != nil {
		t.Fatalf("find tagged: %v", err)
	}
	got.Tags = nil
	got.Notes = ""
	if err := channels.Update(got); err != nil {
		t.Fatalf("update tagged: %v", err)
	}
	if raw := rawChannelTags(t, channels, tagged.ID); raw != "" {
		t.Fatalf("cleared raw tags = %q, want empty", raw)
	}
	got, err = channels.FindByID(tagged.ID)
	if err != nil {
		t.Fatalf("find cleared: %v", err)
	}
	if len(got.Tags) != 0 || got.Notes != "" {
		t.Fatalf("cleared tags = %#v notes = %q, want empty", got.Tags, got.Notes)
	}

	// 其它字段的局部更新不影响标签
	got.Tags = ChannelTags{"a"}
	if err := channels.Update(got); err != nil {
		t.Fatalf("update tags: %v", err)
	}
	if err := channels.UpdateBalance(tagged.ID, 1, nil, ""); err != nil {
		t.Fatalf("update balance: %v", err)
	}
	if raw := rawChannelTags(t, channels, tagged.ID); raw != ",a," {
		t.Fatalf("raw tags after balance update = %q, want %q", raw, ",a,")
	}
}

// TestAutoMigrateAddsChannelTagsAndNotes 模拟旧库（无 tags / notes 列）升级：
// 已有行的 tags 取默认 ""，notes 为 NULL，读取时都应视为空。
func TestAutoMigrateAddsChannelTagsAndNotes(t *testing.T) {
	db := openTestDB(t)
	for _, ddl := range []string{
		"ALTER TABLE channels DROP COLUMN tags",
		"ALTER TABLE channels DROP COLUMN notes",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("exec %q: %v", ddl, err)
		}
	}
	legacy := &Channel{
		Name:           "legacy",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
	}
	if err := db.Omit("Tags", "Notes").Create(legacy).Error; err != nil {
		t.Fatalf("create legacy channel: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	for _, column := range []string{"tags", "notes"} {
		hasColumn, err := tableHasColumn(db, "channels", column)
		if err != nil {
			t.Fatalf("inspect channels.%s: %v", column, err)
		}
		if !hasColumn {
			t.Fatalf("channels.%s missing after auto migrate", column)
		}
	}

	channels := NewChannels(db)
	if raw := rawChannelTags(t, channels, legacy.ID); raw != "" {
		t.Fatalf("legacy raw tags = %q, want empty", raw)
	}
	got, err := channels.FindByID(legacy.ID)
	if err != nil {
		t.Fatalf("find legacy: %v", err)
	}
	if len(got.Tags) != 0 || got.Notes != "" {
		t.Fatalf("legacy tags = %#v notes = %q, want empty", got.Tags, got.Notes)
	}
	list, err := channels.List()
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(list) != 1 || len(list[0].Tags) != 0 {
		t.Fatalf("list = %#v", list)
	}

	// 再次迁移保持幂等
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate again: %v", err)
	}
}
