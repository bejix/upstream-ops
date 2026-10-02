// 管理端网关 HTTP 适配层：路由注册与 handler 委托 gateway.Service / storage。
// 风格与 channels.go 等一致：func(c *gin.Context, d *Deps)，不引入 handler struct。
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/gateway"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// registerGatewayAdmin 在管理 API 下注册 /gateway/* 路由。
func registerGatewayAdmin(g *gin.RouterGroup, d *Deps) {
	if d.Gateway == nil {
		return
	}
	gp := g.Group("/gateway")
	{
		// groups（reorder 须在 :id 之前注册，避免被当成 id）
		gp.GET("/groups", func(c *gin.Context) { listGatewayGroups(c, d) })
		gp.POST("/groups", func(c *gin.Context) { createGatewayGroup(c, d) })
		gp.PUT("/groups/reorder", func(c *gin.Context) { reorderGatewayGroups(c, d) })
		gp.GET("/groups/:id", func(c *gin.Context) { getGatewayGroup(c, d) })
		gp.PUT("/groups/:id", func(c *gin.Context) { updateGatewayGroup(c, d) })
		gp.DELETE("/groups/:id", func(c *gin.Context) { deleteGatewayGroup(c, d) })

		// keys under group
		gp.GET("/groups/:id/keys", func(c *gin.Context) { listGatewayGroupKeys(c, d) })
		gp.POST("/groups/:id/keys", func(c *gin.Context) { createGatewayGroupKey(c, d) })

		// routes under group
		gp.GET("/groups/:id/routes", func(c *gin.Context) { listGatewayGroupRoutes(c, d) })
		gp.PUT("/groups/:id/routes", func(c *gin.Context) { saveGatewayGroupRoutes(c, d) })
		gp.PUT("/groups/:id/routes/:route_id/key", func(c *gin.Context) { bindGatewayRouteKey(c, d) })
		gp.POST("/groups/:id/routes/ensure-keys", func(c *gin.Context) { ensureGatewayGroupRouteKeys(c, d) })

		// models
		gp.GET("/groups/:id/models/preview", func(c *gin.Context) { previewGatewayGroupModels(c, d) })
		gp.POST("/groups/:id/models/sync", func(c *gin.Context) { syncGatewayGroupModels(c, d) })
		gp.POST("/groups/:id/models/test", func(c *gin.Context) { testGatewayGroupModel(c, d) })

		// key ops
		gp.PUT("/keys/:id", func(c *gin.Context) { updateGatewayKey(c, d) })
		gp.DELETE("/keys/:id", func(c *gin.Context) { deleteGatewayKey(c, d) })
		gp.POST("/keys/:id/reveal", func(c *gin.Context) { revealGatewayKey(c, d) })

		// route ops
		gp.POST("/routes/:id/clear-pause", func(c *gin.Context) { clearGatewayRoutePause(c, d) })

		// providers（直连渠道）— options 须在 :id 之前注册
		gp.GET("/providers/options", func(c *gin.Context) { listGatewayProviderOptions(c, d) })
		gp.GET("/providers", func(c *gin.Context) { listGatewayProviders(c, d) })
		gp.POST("/providers", func(c *gin.Context) { createGatewayProvider(c, d) })
		gp.PUT("/providers/:id", func(c *gin.Context) { updateGatewayProvider(c, d) })
		gp.DELETE("/providers/:id", func(c *gin.Context) { deleteGatewayProvider(c, d) })
		gp.POST("/providers/:id/reveal", func(c *gin.Context) { revealGatewayProvider(c, d) })

		// usage
		gp.GET("/usage", func(c *gin.Context) { listGatewayUsage(c, d) })
		gp.GET("/usage/stats", func(c *gin.Context) { statsGatewayUsage(c, d) })
		gp.GET("/usage/models", func(c *gin.Context) { listGatewayUsageModels(c, d) })
		gp.POST("/usage/cleanup", func(c *gin.Context) { cleanupGatewayUsage(c, d) })

		// prices
		gp.GET("/prices", func(c *gin.Context) { listGatewayPrices(c, d) })
		gp.GET("/prices/defaults", func(c *gin.Context) { listGatewayDefaultPrices(c, d) })
		gp.PUT("/prices", func(c *gin.Context) { upsertGatewayPrice(c, d) })
		gp.DELETE("/prices/:id", func(c *gin.Context) { deleteGatewayPrice(c, d) })
	}
}

func listGatewayProviders(c *gin.Context, d *Deps) {
	q := storage.GatewayProviderQuery{
		Q:        strings.TrimSpace(c.Query("q")),
		Page:     queryInt(c, "page", 1),
		PageSize: queryInt(c, "page_size", 20),
	}
	page, err := d.Gateway.ListProviders(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

func listGatewayProviderOptions(c *gin.Context, d *Deps) {
	list, err := d.Gateway.ListProviderOptions(strings.TrimSpace(c.Query("q")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 轻量字段：不返回 cipher
	type opt struct {
		ID                 uint    `json:"id"`
		Name               string  `json:"name"`
		BaseURL            string  `json:"base_url"`
		APIKeyHint         string  `json:"api_key_hint"`
		UpstreamProtocol   string  `json:"upstream_protocol"`
		DefaultBillingRate float64 `json:"default_billing_rate"`
		Enabled            bool    `json:"enabled"`
	}
	items := make([]opt, 0, len(list))
	for _, p := range list {
		items = append(items, opt{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKeyHint: p.APIKeyHint,
			UpstreamProtocol: p.UpstreamProtocol, DefaultBillingRate: p.DefaultBillingRate, Enabled: p.Enabled,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func createGatewayProvider(c *gin.Context, d *Deps) {
	var in gateway.CreateProviderInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := d.Gateway.CreateProvider(in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func updateGatewayProvider(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var in gateway.UpdateProviderInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := d.Gateway.UpdateProvider(id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func deleteGatewayProvider(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.Gateway.DeleteProvider(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func revealGatewayProvider(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	secret, err := d.Gateway.RevealProviderKey(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": secret})
}

func listGatewayGroups(c *gin.Context, d *Deps) {
	list, err := d.Gateway.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func reorderGatewayGroups(c *gin.Context, d *Deps) {
	var body struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := d.Gateway.ReorderGroups(body.IDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	list, err := d.Gateway.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func createGatewayGroup(c *gin.Context, d *Deps) {
	var in gateway.CreateGroupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := d.Gateway.CreateGroup(in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func getGatewayGroup(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := d.Gateway.GetGroup(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func updateGatewayGroup(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var in gateway.UpdateGroupInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := d.Gateway.UpdateGroup(id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func deleteGatewayGroup(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.Gateway.DeleteGroup(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func listGatewayGroupKeys(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	list, err := d.Gateway.ListKeysByGroup(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func createGatewayGroupKey(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var in gateway.CreateKeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.GroupID = id
	res, err := d.Gateway.CreateKey(in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, res)
}

func listGatewayGroupRoutes(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	list, err := d.Gateway.ListRoutes(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func saveGatewayGroupRoutes(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Routes []gateway.RouteInput `json:"routes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	list, err := d.Gateway.SaveRoutes(id, body.Routes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func bindGatewayRouteKey(c *gin.Context, d *Deps) {
	groupID, err := parseUintParam(c, "id")
	if err != nil || groupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}
	routeID, err := parseUintParam(c, "route_id")
	if err != nil || routeID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route id"})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must contain only a positive source_api_key_id"})
		return
	}
	field, err := decoder.Token()
	if err != nil || field != "source_api_key_id" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must contain only a positive source_api_key_id"})
		return
	}
	var in gateway.BindRouteKeyInput
	if err := decoder.Decode(&in.SourceAPIKeyID); err != nil || in.SourceAPIKeyID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must contain only a positive source_api_key_id"})
		return
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must contain only a positive source_api_key_id"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must contain a single JSON object"})
		return
	}
	route, err := d.Gateway.BindRouteKey(c.Request.Context(), groupID, routeID, in)
	if err != nil {
		var bindingError *gateway.RouteKeyBindingError
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "gateway group or route not found"})
		case errors.Is(err, storage.ErrGatewayRouteChanged):
			c.JSON(http.StatusConflict, gin.H{"error": "route changed during key binding; reload the route and retry"})
		case errors.As(err, &bindingError):
			c.JSON(http.StatusBadRequest, gin.H{"error": bindingError.Message, "code": bindingError.Code})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to bind route key"})
		}
		return
	}
	c.JSON(http.StatusOK, route)
}

func ensureGatewayGroupRouteKeys(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	// 单条失败跳过，结果在 routes 明细中；不因个别路由失败而整体 4xx
	res, err := d.Gateway.EnsureRouteKeys(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func previewGatewayGroupModels(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	list, err := d.Gateway.PreviewGroupModels(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func syncGatewayGroupModels(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	// body 可选；空 body / 非法 JSON 时仅保留库内已保存的 custom
	var in gateway.SyncGroupModelsInput
	_ = c.ShouldBindJSON(&in)
	// 单渠道失败会跳过并在 routes 明细中体现，不因个别渠道失败而整体 4xx
	res, err := d.Gateway.SyncGroupModels(c.Request.Context(), id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func testGatewayGroupModel(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var in gateway.TestModelInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	results, err := d.Gateway.TestGroupModel(c.Request.Context(), id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"items":    results,
		"ok_count": okCount,
		"total":    len(results),
		"all_ok":   okCount == len(results) && len(results) > 0,
	})
}

func updateGatewayKey(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var in gateway.UpdateKeyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := d.Gateway.UpdateKey(id, in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func deleteGatewayKey(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.Gateway.DeleteKey(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func revealGatewayKey(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	secret, err := d.Gateway.RevealKey(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": secret})
}

func clearGatewayRoutePause(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.Gateway.ClearRoutePause(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func parseGatewayUsageQuery(c *gin.Context) storage.GatewayUsageQuery {
	q := storage.GatewayUsageQuery{
		Page:     queryInt(c, "page", 1),
		PageSize: queryInt(c, "page_size", 20),
	}
	if v := c.Query("group_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			q.GatewayGroupID = uint(n)
		}
	}
	if v := c.Query("gateway_key_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			q.GatewayKeyID = uint(n)
		}
	}
	if v := c.Query("channel_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			q.ChannelID = uint(n)
		}
	}
	q.Model = strings.TrimSpace(c.Query("model"))
	q.RequestID = strings.TrimSpace(c.Query("request_id"))
	if v := c.Query("request_type"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.RequestType = &n
		}
	}
	// result 优先：success | fail | client | multi | multi_success | multi_fail
	if v := strings.TrimSpace(c.Query("result")); v != "" {
		q.ResultMode = strings.ToLower(v)
	} else if v := c.Query("success"); v == "true" || v == "1" {
		q.ResultMode = "success"
		t := true
		q.SuccessOnly = &t
	} else if v == "false" || v == "0" {
		q.ResultMode = "fail"
		f := false
		q.SuccessOnly = &f
	}
	if v := c.Query("from"); v != "" {
		if t, ok := parseUsageTime(v); ok {
			q.From = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, ok := parseUsageTime(v); ok {
			q.To = &t
		}
	}
	return q
}

// parseUsageTime 兼容前端 toISOString（含毫秒）与 datetime-local（无时区=本地）。
func parseUsageTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	// JS Date.toISOString() → 带毫秒的 RFC3339，必须用 Nano
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	// datetime-local: 无时区，按服务器本地时区
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func listGatewayUsage(c *gin.Context, d *Deps) {
	q := parseGatewayUsageQuery(c)
	page, err := d.GatewayUsage.List(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

func statsGatewayUsage(c *gin.Context, d *Deps) {
	q := parseGatewayUsageQuery(c)
	stats, err := d.GatewayUsage.Stats(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// listGatewayUsageModels 使用记录中出现过的模型聚合（下拉选项）。
// 支持 group_id / gateway_key_id / from / to；忽略 model / result。
func listGatewayUsageModels(c *gin.Context, d *Deps) {
	if d.GatewayUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage storage unavailable"})
		return
	}
	q := parseGatewayUsageQuery(c)
	items, err := d.GatewayUsage.ListModels(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// cleanupGatewayUsageReq 清理访问日志。
// - all=true：删除全部
// - all=false：删除 created_at < before 的记录（before 为 RFC3339 / datetime-local）
// confirm 必须为 true，防止误触。
type cleanupGatewayUsageReq struct {
	All     bool   `json:"all"`
	Before  string `json:"before"`
	Confirm bool   `json:"confirm"`
	// DryRun 仅统计将删除的条数，不实际删除
	DryRun bool `json:"dry_run"`
}

func cleanupGatewayUsage(c *gin.Context, d *Deps) {
	if d.GatewayUsage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage storage unavailable"})
		return
	}
	var req cleanupGatewayUsageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !req.Confirm && !req.DryRun {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请确认清理操作（confirm=true）"})
		return
	}

	var (
		count int64
		err   error
	)
	if req.All {
		if req.DryRun {
			count, err = d.GatewayUsage.CountAll()
		} else {
			count, err = d.GatewayUsage.DeleteAll()
		}
	} else {
		before, ok := parseUsageTime(req.Before)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供有效的截止时间 before"})
			return
		}
		if req.DryRun {
			count, err = d.GatewayUsage.CountBefore(before)
		} else {
			count, err = d.GatewayUsage.DeleteBefore(before)
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.DryRun {
		c.JSON(http.StatusOK, gin.H{"dry_run": true, "matched": count})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": count, "all": req.All})
}

func listGatewayPrices(c *gin.Context, d *Deps) {
	list, err := d.ModelPrices.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

func listGatewayDefaultPrices(c *gin.Context, d *Deps) {
	if d.Gateway == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "gateway unavailable"})
		return
	}
	items := d.Gateway.ListDefaultPrices(c.Query("q"))
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func upsertGatewayPrice(c *gin.Context, d *Deps) {
	var item storage.ModelPriceOverride
	if err := c.ShouldBindJSON(&item); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(item.ModelName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_name is required"})
		return
	}
	item.BillingMode = strings.ToLower(strings.TrimSpace(item.BillingMode))
	if item.BillingMode == "" {
		item.BillingMode = string(gateway.BillingModeToken)
	}
	switch gateway.BillingMode(item.BillingMode) {
	case gateway.BillingModeToken, gateway.BillingModePerRequest, gateway.BillingModeImage, gateway.BillingModeVideo:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid billing_mode"})
		return
	}
	for _, price := range []float64{
		item.InputPricePerToken, item.OutputPricePerToken, item.CacheCreationPricePerToken, item.CacheReadPricePerToken,
		item.PerRequestPrice, item.ImagePrice1K, item.ImagePrice2K, item.ImagePrice4K,
		item.VideoPrice480P, item.VideoPrice720P, item.VideoPrice1080P,
	} {
		if price < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price must be >= 0"})
			return
		}
	}
	if strings.TrimSpace(item.PricingTiersJSON) != "" {
		var tiers []gateway.PricingTier
		if err := json.Unmarshal([]byte(item.PricingTiersJSON), &tiers); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pricing_tiers_json"})
			return
		}
		for _, tier := range tiers {
			if tier.PerRequestPrice < 0 || tier.MinTokens < 0 || (tier.MaxTokens != nil && *tier.MaxTokens <= tier.MinTokens) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pricing tier"})
				return
			}
		}
	}
	if err := d.ModelPrices.Upsert(&item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func deleteGatewayPrice(c *gin.Context, d *Deps) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := d.ModelPrices.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func parseUintParam(c *gin.Context, name string) (uint, error) {
	n, err := strconv.ParseUint(c.Param(name), 10, 64)
	return uint(n), err
}

func queryInt(c *gin.Context, name string, def int) int {
	v := c.Query(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
