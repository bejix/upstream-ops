// 上游模型目录（NewAPI）：GET /api/pricing 提供模型、分组归属与分组倍率。
package newapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector"
)

// catalogMaxBytes 单次响应读取上限。xem 全量为 24 KB；留足二开版的余量又不至于失控。
const catalogMaxBytes = 4 << 20

// newAPIPricingModel 是 /api/pricing 里 data[] 的一项。
// 只声明用到的字段，二开版多出的字段会被忽略。
type newAPIPricingModel struct {
	ModelName     string          `json:"model_name"`
	EnableGroups  []string        `json:"enable_groups"`
	ModelRatio    json.RawMessage `json:"model_ratio"`
	Completion    json.RawMessage `json:"completion_ratio"`
	ModelPrice    float64         `json:"model_price"`
	QuotaType     int             `json:"quota_type"`
	VendorID      json.RawMessage `json:"vendor_id"`
	Tags          json.RawMessage `json:"tags"`
	CacheRatio    json.RawMessage `json:"cache_ratio"`
	CreateCache   json.RawMessage `json:"create_cache_ratio"`
	BillingMode   string          `json:"billing_mode"`
	BillingExpr   string          `json:"billing_expr"`
	EndpointTypes []string        `json:"supported_endpoint_types"`
}

// newAPIPricingPayload /api/pricing 的顶层结构。
type newAPIPricingPayload struct {
	Success        bool                       `json:"success"`
	Message        string                     `json:"message"`
	Data           []newAPIPricingModel       `json:"data"`
	UsableGroup    map[string]json.RawMessage `json:"usable_group"`
	GroupRatio     map[string]json.RawMessage `json:"group_ratio"`
	Vendors        []newAPIVendor             `json:"vendors"`
	PricingVersion string                     `json:"pricing_version"`
}

type newAPIVendor struct {
	ID   any    `json:"id"`
	Name string `json:"name"`
}

// GetCatalog 拉取并归一化上游模型目录。
//
// 失败策略：pricing 接口不可用 → 返回带 Degrade 的空目录 + error，
// 让上层同时拿到「为什么没有数据」和可展示的分组配置。
func (c *Client) GetCatalog(ctx context.Context, ch *connector.Channel, session *connector.AuthSession) (*connector.Catalog, error) {
	site := strings.TrimRight(ch.SiteURL, "/")
	body, err := c.getRaw(ctx, site+"/api/pricing", session)
	if err != nil {
		// 有 session 先试，失败后允许调用方用 nil session 再试一次（部分站点定价匿名可读）。
		if session != nil {
			if anon, anonErr := c.getRaw(ctx, site+"/api/pricing", nil); anonErr == nil {
				body = anon
				err = nil
			}
		}
		if err != nil {
			return &connector.Catalog{
				Capability:     connector.CatalogCapabilityGroupModels,
				Degrade:        connector.CatalogDegradeFetchFailed,
				DegradeMessage: trimCatalogErr(err.Error(), 200),
			}, err
		}
	}
	if len(body) > catalogMaxBytes {
		msg := fmt.Sprintf("pricing response too large: %d bytes", len(body))
		return &connector.Catalog{
			Capability:     connector.CatalogCapabilityGroupModels,
			Degrade:        connector.CatalogDegradeFetchFailed,
			DegradeMessage: msg,
		}, fmt.Errorf("%s", msg)
	}

	var payload newAPIPricingPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		msg := trimCatalogErr("pricing decode: "+err.Error(), 200)
		return &connector.Catalog{
			Capability:     connector.CatalogCapabilityGroupModels,
			Degrade:        connector.CatalogDegradeFetchFailed,
			DegradeMessage: msg,
		}, fmt.Errorf("%s", msg)
	}

	catalog := &connector.Catalog{
		Capability:     connector.CatalogCapabilityGroupModels,
		PricingVersion: payload.PricingVersion,
	}
	// 部分二开版把整个响应包在 {success,message,data} 里；data 为对象时再解一层。
	vendors := newAPIVendorNameMap(payload.Vendors)

	// 分组倍率与描述：仅当前账号可见（usable_group）的分组有值。
	ratios := make(map[string]float64, len(payload.GroupRatio))
	for name, raw := range payload.GroupRatio {
		if v, ok := jsonNumber(raw); ok {
			ratios[name] = v
		}
	}
	descs := make(map[string]string, len(payload.UsableGroup))
	for name, raw := range payload.UsableGroup {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			descs[name] = strings.TrimSpace(s)
		}
	}

	groupModels := make(map[string]int)
	models := make([]connector.CatalogModel, 0, len(payload.Data))
	for _, m := range payload.Data {
		name := strings.TrimSpace(m.ModelName)
		if name == "" {
			continue
		}
		groups := make([]string, 0, len(m.EnableGroups))
		seen := make(map[string]struct{}, len(m.EnableGroups))
		for _, g := range m.EnableGroups {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if _, ok := seen[g]; ok {
				continue
			}
			seen[g] = struct{}{}
			groups = append(groups, g)
			groupModels[g]++
		}
		sort.Strings(groups)

		item := connector.CatalogModel{
			Name:        name,
			Groups:      groups,
			BillingMode: strings.TrimSpace(m.BillingMode),
			BillingExpr: strings.TrimSpace(m.BillingExpr),
			ModelPrice:  m.ModelPrice,
			QuotaType:   m.QuotaType,
			Endpoints:   cleanStrings(m.EndpointTypes),
		}
		if v, ok := jsonNumber(m.ModelRatio); ok {
			item.ModelRatio = v
		}
		if v, ok := jsonNumber(m.Completion); ok {
			item.CompletionRatio = v
		}
		if v, ok := jsonNumber(m.CacheRatio); ok {
			item.CacheRatio = &v
		}
		if v, ok := jsonNumber(m.CreateCache); ok {
			item.CreateCacheRatio = &v
		}
		item.Vendor = lookupVendor(vendors, m.VendorID)
		item.Tags = splitTags(m.Tags)
		if item.BillingExpr != "" && item.BillingMode == "" {
			item.BillingMode = "tiered_expr"
		}
		item.BillingBrief = BriefBillingExpr(item.BillingExpr)
		models = append(models, item)
	}

	// 分组集合 = 有模型的 ∪ 账号已授权的。两者不一致是常态（授权了但没在售 / 有模型但未授权）。
	names := make([]string, 0, len(groupModels)+len(descs))
	seenGroup := make(map[string]struct{}, len(groupModels)+len(descs))
	for name := range groupModels {
		seenGroup[name] = struct{}{}
		names = append(names, name)
	}
	for name := range descs {
		if _, ok := seenGroup[name]; !ok {
			seenGroup[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)

	groups := make([]connector.CatalogGroup, 0, len(names))
	unauthorized := 0
	for _, name := range names {
		ratio, hasRatio := ratios[name]
		// 「授权」的口径是出现在 usable_group 里，与 group_ratio 无关：
		// 部分二开版的 group_ratio 会带上全部分组（含当前账号无权使用的），
		// 不能因为「有倍率」就判定为已授权。
		_, authorized := descs[name]
		if !authorized {
			unauthorized++
		}
		groups = append(groups, connector.CatalogGroup{
			Name:        name,
			Description: descs[name],
			Ratio:       ratio,
			HasRatio:    hasRatio,
			Authorized:  authorized,
			ModelCount:  groupModels[name],
		})
	}

	if unauthorized > 0 {
		catalog.Notes = append(catalog.Notes,
			fmt.Sprintf("%d 个分组被上游模型引用，但不在当前账号的授权列表中，可能无法调用", unauthorized))
	}
	if len(models) == 0 {
		catalog.Notes = append(catalog.Notes,
			"上游定价接口没有返回任何模型：该站点可能未开启定价功能，或全部模型已下架")
	}
	catalog.Groups = groups
	catalog.Models = models
	return catalog, nil
}

// jsonNumber 兼容 "0.5" / 0.5 / null 三种写法（二开版常见）。
func jsonNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, false
		}
		var parsed float64
		if _, err := fmt.Sscanf(s, "%g", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// splitTags 兼容 tags 的两种形态：字符串（逗号分隔）与字符串数组。
func splitTags(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		out := make([]string, 0, 4)
		for _, part := range strings.Split(s, ",") {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, v)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return cleanStrings(arr)
	}
	return nil
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func newAPIVendorNameMap(vendors []newAPIVendor) map[string]string {
	out := make(map[string]string, len(vendors))
	for _, v := range vendors {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}
		switch id := v.ID.(type) {
		case float64:
			out[fmt.Sprintf("%d", int64(id))] = name
		case string:
			if s := strings.TrimSpace(id); s != "" {
				out[s] = name
			}
		}
	}
	return out
}

func lookupVendor(vendors map[string]string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if v, ok := jsonNumber(raw); ok {
		return vendors[fmt.Sprintf("%d", int64(v))]
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return vendors[strings.TrimSpace(s)]
	}
	return ""
}

func trimCatalogErr(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

var (
	exprInPrice  = regexp.MustCompile(`(?:^|[\s(])p\s*\*\s*([0-9.]+)`)
	exprOutPrice = regexp.MustCompile(`(?:^|[\s(])c\s*\*\s*([0-9.]+)`)
	exprFixed    = regexp.MustCompile(`fixed\(\s*([0-9.]+)\s*\)`)
	exprSeconds  = regexp.MustCompile(`u\(\s*"seconds"\s*\)\s*\*\s*([0-9.]+)`)
	exprByLen    = regexp.MustCompile(`\blen\s*[<>=]`)
)

// BriefBillingExpr 把上游计费表达式压缩成一行可读摘要。
//
// 表达式来自 NewAPI 二开版（如码少），形如：
//
//	tier("base", p * 4 + c * 20 + cr * 0.2 + cc * 5)
//	len < 272000 ? tier("默认", p * 5 + c * 30) : tier("长文本", p * 10)
//	tier("base", fixed(0.02))
//
// 解析失败不影响主流程：返回「自定义表达式」，原文照旧通过 tooltip 展示。
func BriefBillingExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	if m := exprFixed.FindStringSubmatch(expr); m != nil {
		parts = append(parts, "$"+trimZero(m[1])+"/次")
	}
	if m := exprSeconds.FindStringSubmatch(expr); m != nil {
		parts = append(parts, "$"+trimZero(m[1])+"/秒")
	}
	if m := exprInPrice.FindStringSubmatch(expr); m != nil {
		parts = append(parts, "入×"+trimZero(m[1]))
	}
	if m := exprOutPrice.FindStringSubmatch(expr); m != nil {
		parts = append(parts, "出×"+trimZero(m[1]))
	}
	if exprByLen.MatchString(expr) {
		parts = append(parts, "按长度分档")
	}
	if len(parts) == 0 {
		return "自定义表达式"
	}
	return strings.Join(parts, " · ")
}

// trimZero 去掉小数末尾多余的 0，但绝不能碰整数部分的 0。
//
// 反例：早期实现直接 TrimRight("0") 会把 "20" 变成 "2"，把倍率 20 显示成 2。
// 只有存在小数点时才裁小数部分。
func trimZero(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, ".") {
		if s == "" {
			return "0"
		}
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
