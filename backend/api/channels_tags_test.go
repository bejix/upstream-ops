package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/channel"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
)

type channelMetaOut struct {
	ID    uint      `json:"id"`
	Name  string    `json:"name"`
	Tags  *[]string `json:"tags"` // 指针用来区分 [] 与缺失 / null
	Notes *string   `json:"notes"`
}

func newChannelMetaRouter(t *testing.T) (*gin.Engine, *storage.Channels) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := openTestDB(t)
	channels := storage.NewChannels(db)
	cipher, err := crypto.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	channelSvc := channel.NewService(
		channels,
		storage.NewAuthSessions(db),
		storage.NewCaptchas(db),
		storage.NewRates(db),
		storage.NewMonitorLogs(db),
		cipher,
	)
	r := gin.New()
	registerChannels(r.Group("/api"), &Deps{Channels: channels, ChannelSvc: channelSvc})
	return r, channels
}

func doChannelJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	switch b := body.(type) {
	case nil:
		reader = strings.NewReader("")
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeChannelMeta(t *testing.T, rec *httptest.ResponseRecorder) channelMetaOut {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data channelMetaOut `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return resp.Data
}

func assertChannelMeta(t *testing.T, label string, got channelMetaOut, wantTags []string, wantNotes string) {
	t.Helper()
	if got.Tags == nil {
		t.Fatalf("%s: tags missing or null, want %#v", label, wantTags)
	}
	if wantTags == nil {
		wantTags = []string{}
	}
	if !reflect.DeepEqual(*got.Tags, wantTags) {
		t.Fatalf("%s: tags = %#v, want %#v", label, *got.Tags, wantTags)
	}
	if got.Notes == nil {
		t.Fatalf("%s: notes missing, want %q", label, wantNotes)
	}
	if *got.Notes != wantNotes {
		t.Fatalf("%s: notes = %q, want %q", label, *got.Notes, wantNotes)
	}
}

func assertStoredChannelMeta(t *testing.T, channels *storage.Channels, id uint, wantTags []string, wantNotes string) {
	t.Helper()
	ch, err := channels.FindByID(id)
	if err != nil {
		t.Fatalf("find channel: %v", err)
	}
	if len(wantTags) == 0 {
		if len(ch.Tags) != 0 {
			t.Fatalf("stored tags = %#v, want empty", ch.Tags)
		}
	} else if !reflect.DeepEqual([]string(ch.Tags), wantTags) {
		t.Fatalf("stored tags = %#v, want %#v", ch.Tags, wantTags)
	}
	if ch.Notes != wantNotes {
		t.Fatalf("stored notes = %q, want %q", ch.Notes, wantNotes)
	}
}

func baseChannelCreateBody(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"type":     "sub2api",
		"site_url": "https://" + name + ".example.com",
		"username": "u",
		"password": "p",
	}
}

func TestChannelTagsAndNotesCreateUpdateGet(t *testing.T) {
	r, channels := newChannelMetaRouter(t)

	body := baseChannelCreateBody("demo")
	body["tags"] = []string{" vip ", "备用", "VIP", "a，b", "", "c,d"}
	body["notes"] = "  主力渠道\n晚高峰慢  "
	created := decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPost, "/api/channels", body))
	wantTags := []string{"vip", "备用", "a", "b", "c", "d"}
	wantNotes := "主力渠道\n晚高峰慢"
	assertChannelMeta(t, "create", created, wantTags, wantNotes)
	assertStoredChannelMeta(t, channels, created.ID, wantTags, wantNotes)

	path := fmt.Sprintf("/api/channels/%d", created.ID)
	assertChannelMeta(t, "get", decodeChannelMeta(t, doChannelJSON(t, r, http.MethodGet, path, nil)), wantTags, wantNotes)

	// 省略 tags / notes 的局部更新不应改动它们
	updated := decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"name":"demo-renamed"}`))
	if updated.Name != "demo-renamed" {
		t.Fatalf("name = %q, want demo-renamed", updated.Name)
	}
	assertChannelMeta(t, "partial update", updated, wantTags, wantNotes)
	assertStoredChannelMeta(t, channels, created.ID, wantTags, wantNotes)

	// null 与省略等价
	updated = decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"tags":null,"notes":null}`))
	assertChannelMeta(t, "null update", updated, wantTags, wantNotes)

	// 暂停 / 恢复监控走的也是 Update，不应影响标签
	if rec := doChannelJSON(t, r, http.MethodPost, path+"/disable", nil); rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body = %s", rec.Code, rec.Body.String())
	}
	assertStoredChannelMeta(t, channels, created.ID, wantTags, wantNotes)

	// 只改标签，备注保持不变
	updated = decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"tags":["x"," y ","X"]}`))
	assertChannelMeta(t, "tags only", updated, []string{"x", "y"}, wantNotes)
	assertStoredChannelMeta(t, channels, created.ID, []string{"x", "y"}, wantNotes)

	// 只改备注，标签保持不变
	updated = decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"notes":"  新备注 "}`))
	assertChannelMeta(t, "notes only", updated, []string{"x", "y"}, "新备注")

	// [] 与 "" 清空
	updated = decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"tags":[],"notes":""}`))
	assertChannelMeta(t, "clear", updated, nil, "")
	assertStoredChannelMeta(t, channels, created.ID, nil, "")
	if !strings.Contains(doChannelJSON(t, r, http.MethodGet, path, nil).Body.String(), `"tags":[]`) {
		t.Fatal("cleared channel json should contain \"tags\":[]")
	}

	// 只含空白 / 分隔符的输入等同清空
	decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"tags":["a"],"notes":"n"}`))
	updated = decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPut, path, `{"tags":[" ",",，"],"notes":"  \n "}`))
	assertChannelMeta(t, "blank clear", updated, nil, "")

	// 未提供 tags / notes 的新建渠道：列表与分页里都输出 [] 与 ""
	plain := decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPost, "/api/channels", baseChannelCreateBody("plain")))
	assertChannelMeta(t, "create plain", plain, nil, "")

	var list struct {
		Data []channelMetaOut `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels", &list)
	var page struct {
		Data struct {
			Items []channelMetaOut `json:"items"`
		} `json:"data"`
	}
	getTodayCostJSON(t, r, "/api/channels?page=1&page_size=20", &page)
	for label, items := range map[string][]channelMetaOut{"list": list.Data, "page": page.Data.Items} {
		if len(items) != 2 {
			t.Fatalf("%s: len = %d, want 2", label, len(items))
		}
		for _, item := range items {
			assertChannelMeta(t, label+" "+item.Name, item, nil, "")
		}
	}
}

func TestChannelTagsAndNotesValidation(t *testing.T) {
	r, channels := newChannelMetaRouter(t)

	manyTags := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("t%d", i))
		}
		return out
	}
	longTag := strings.Repeat("标", storage.MaxChannelTagRunes+1)
	okTag := strings.Repeat("标", storage.MaxChannelTagRunes)
	longNotes := strings.Repeat("备", storage.MaxChannelNotesRunes+1)
	okNotes := strings.Repeat("备", storage.MaxChannelNotesRunes)

	assertBadRequest := func(label string, rec *httptest.ResponseRecorder, wantMsg string) {
		t.Helper()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400, body = %s", label, rec.Code, rec.Body.String())
		}
		var resp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: decode: %v", label, err)
		}
		if !strings.Contains(resp.Error, wantMsg) {
			t.Fatalf("%s: error = %q, want contains %q", label, resp.Error, wantMsg)
		}
	}

	createCases := []struct {
		label string
		tags  []string
		notes string
		want  string
	}{
		{label: "too many tags", tags: manyTags(storage.MaxChannelTags + 1), want: "标签最多 20 个"},
		{label: "too many tags via comma split", tags: []string{strings.Join(manyTags(storage.MaxChannelTags+1), ",")}, want: "标签最多 20 个"},
		{label: "tag too long", tags: []string{"ok", longTag}, want: "单个标签最多 32 个字符"},
		{label: "notes too long", notes: longNotes, want: "备注最多 2000 个字符"},
	}
	for i, tc := range createCases {
		body := baseChannelCreateBody(fmt.Sprintf("bad%d", i))
		body["tags"] = tc.tags
		body["notes"] = tc.notes
		assertBadRequest("create "+tc.label, doChannelJSON(t, r, http.MethodPost, "/api/channels", body), tc.want)
	}
	if list, err := channels.List(); err != nil || len(list) != 0 {
		t.Fatalf("rejected creates should not persist: list=%#v err=%v", list, err)
	}

	// 边界值：规范化后正好 20 个（重复项不计数）、32 个字符、首尾空白不计入的 2000 个字符
	body := baseChannelCreateBody("edge")
	body["tags"] = append(append(manyTags(storage.MaxChannelTags-1), okTag), "T0", " t1 ")
	body["notes"] = "\n  " + okNotes + "  \n"
	created := decodeChannelMeta(t, doChannelJSON(t, r, http.MethodPost, "/api/channels", body))
	wantTags := append(manyTags(storage.MaxChannelTags-1), okTag)
	assertChannelMeta(t, "edge create", created, wantTags, okNotes)

	path := fmt.Sprintf("/api/channels/%d", created.ID)
	updateCases := []struct {
		label string
		body  map[string]any
		want  string
	}{
		{label: "too many tags", body: map[string]any{"tags": manyTags(storage.MaxChannelTags + 1)}, want: "标签最多 20 个"},
		{label: "tag too long", body: map[string]any{"tags": []string{longTag}}, want: "单个标签最多 32 个字符"},
		{label: "notes too long", body: map[string]any{"notes": longNotes}, want: "备注最多 2000 个字符"},
		{label: "valid tags with notes too long", body: map[string]any{"name": "renamed", "tags": []string{"x"}, "notes": longNotes}, want: "备注最多 2000 个字符"},
	}
	for _, tc := range updateCases {
		assertBadRequest("update "+tc.label, doChannelJSON(t, r, http.MethodPut, path, tc.body), tc.want)
	}
	// 校验失败的更新不应落库任何字段
	assertStoredChannelMeta(t, channels, created.ID, wantTags, okNotes)
	if ch, err := channels.FindByID(created.ID); err != nil || ch.Name != "edge" {
		t.Fatalf("rejected update changed channel: %#v err=%v", ch, err)
	}
}
