// 网关相关仓储：直连渠道、分组、密钥、路由、用量、模型价目覆盖。
package storage

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// ---------- GatewayProviders（直连渠道） ----------

// GatewayProviders 直连上游仓储。
type GatewayProviders struct{ db *gorm.DB }

// NewGatewayProviders 构造直连渠道仓储。
func NewGatewayProviders(db *gorm.DB) *GatewayProviders { return &GatewayProviders{db: db} }

// GatewayProviderQuery 分页列表查询。
type GatewayProviderQuery struct {
	Q        string
	Page     int
	PageSize int
	// EnabledOnly 为 true 时只返回 enabled
	EnabledOnly bool
}

// GatewayProviderPage 分页结果。
type GatewayProviderPage struct {
	Items    []GatewayProvider `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Pages    int               `json:"pages"`
}

// List 分页列表。
func (r *GatewayProviders) List(q GatewayProviderQuery) (*GatewayProviderPage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	db := r.db.Model(&GatewayProvider{})
	if q.EnabledOnly {
		db = db.Where("enabled = ?", true)
	}
	if s := strings.TrimSpace(q.Q); s != "" {
		like := "%" + s + "%"
		db = db.Where("name LIKE ? OR base_url LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []GatewayProvider
	offset := (q.Page - 1) * q.PageSize
	if err := db.Order("id DESC").Offset(offset).Limit(q.PageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	pages := int(total) / q.PageSize
	if int(total)%q.PageSize != 0 {
		pages++
	}
	return &GatewayProviderPage{
		Items:    items,
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Pages:    pages,
	}, nil
}

// ListOptions 返回启用的轻量列表（路由下拉），最多 500 条。
func (r *GatewayProviders) ListOptions(q string) ([]GatewayProvider, error) {
	db := r.db.Model(&GatewayProvider{}).Where("enabled = ?", true)
	if s := strings.TrimSpace(q); s != "" {
		like := "%" + s + "%"
		db = db.Where("name LIKE ? OR base_url LIKE ?", like, like)
	}
	var items []GatewayProvider
	if err := db.Order("name ASC").Limit(500).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindByID 按主键查询。
func (r *GatewayProviders) FindByID(id uint) (*GatewayProvider, error) {
	var item GatewayProvider
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayProviders) FindByName(name string) (*GatewayProvider, error) {
	var item GatewayProvider
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayProviders) Create(item *GatewayProvider) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayProviders) Update(item *GatewayProvider) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayProviders) Delete(id uint) error {
	return r.db.Delete(&GatewayProvider{}, id).Error
}

// GatewayGroups 网关组仓储。
type GatewayGroups struct{ db *gorm.DB }

// NewGatewayGroups 构造网关分组仓储。
func NewGatewayGroups(db *gorm.DB) *GatewayGroups { return &GatewayGroups{db: db} }

// List 分页列表。
func (r *GatewayGroups) List() ([]GatewayGroup, error) {
	var list []GatewayGroup
	// position 升序；同 position 时较新 id 在前（兼容尚未重排的旧数据）
	if err := r.db.Order("position ASC, id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// NextPosition 返回新建组应使用的 position（当前最大 + 1）。
func (r *GatewayGroups) NextPosition() (int, error) {
	var maxPos *int
	if err := r.db.Model(&GatewayGroup{}).Select("MAX(position)").Scan(&maxPos).Error; err != nil {
		return 0, err
	}
	if maxPos == nil {
		return 0, nil
	}
	return *maxPos + 1, nil
}

// Reorder 按 ids 顺序重写 position（0..n-1）。未出现在 ids 中的组保持原 position。
func (r *GatewayGroups) Reorder(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		seen := make(map[uint]struct{}, len(ids))
		for i, id := range ids {
			if id == 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			if err := tx.Model(&GatewayGroup{}).Where("id = ?", id).
				Update("position", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// FindByID 按主键查询。
func (r *GatewayGroups) FindByID(id uint) (*GatewayGroup, error) {
	var item GatewayGroup
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayGroups) FindByName(name string) (*GatewayGroup, error) {
	var item GatewayGroup
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayGroups) Create(item *GatewayGroup) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayGroups) Update(item *GatewayGroup) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayGroups) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("gateway_group_id = ?", id).Delete(&GatewayRoute{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&GatewayKey{}).Error; err != nil {
			return err
		}
		return tx.Delete(&GatewayGroup{}, id).Error
	})
}

// GatewayKeys 网关密钥仓储。
type GatewayKeys struct{ db *gorm.DB }

// NewGatewayKeys 构造网关密钥仓储。
func NewGatewayKeys(db *gorm.DB) *GatewayKeys { return &GatewayKeys{db: db} }

// List 分页列表。
func (r *GatewayKeys) List() ([]GatewayKey, error) {
	var list []GatewayKey
	if err := r.db.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListByGroupID 列出某分组下资源。
func (r *GatewayKeys) ListByGroupID(groupID uint) ([]GatewayKey, error) {
	var list []GatewayKey
	if err := r.db.Where("group_id = ?", groupID).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// FindByID 按主键查询。
func (r *GatewayKeys) FindByID(id uint) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByHash 按密钥哈希查询。
func (r *GatewayKeys) FindByHash(hash string) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.Where("key_hash = ?", hash).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayKeys) FindByName(name string) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayKeys) Create(item *GatewayKey) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayKeys) Update(item *GatewayKey) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayKeys) Delete(id uint) error {
	return r.db.Delete(&GatewayKey{}, id).Error
}

// DeleteWithGroupMutation 在同一事务内更新所属组并删除 Key。
func (r *GatewayKeys) DeleteWithGroupMutation(id uint, mutate func(*GatewayGroup) bool) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var key GatewayKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&key, id).Error; err != nil {
			return err
		}
		var group GatewayGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, key.GroupID).Error; err != nil {
			return err
		}
		if mutate != nil && mutate(&group) {
			if err := tx.Model(&GatewayGroup{}).Where("id = ?", group.ID).Update("system_prompt_rules_json", group.SystemPromptRulesJSON).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&key).Error
	})
}

// TouchLastUsed 更新密钥最近使用时间。
func (r *GatewayKeys) TouchLastUsed(id uint, at time.Time) error {
	return r.db.Model(&GatewayKey{}).Where("id = ?", id).Updates(map[string]any{
		"last_used_at": at,
	}).Error
}

// AddQuotaUsed 累加密钥已用额度。
func (r *GatewayKeys) AddQuotaUsed(id uint, amount float64) error {
	if amount == 0 {
		return nil
	}
	return r.db.Model(&GatewayKey{}).Where("id = ?", id).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", amount)).Error
}

// GatewayRoutes 网关路由仓储。
type GatewayRoutes struct{ db *gorm.DB }

var ErrGatewayRouteChanged = errors.New("gateway route changed during key binding")

// NewGatewayRoutes 构造网关路由仓储。
func NewGatewayRoutes(db *gorm.DB) *GatewayRoutes { return &GatewayRoutes{db: db} }

// ListByGroupID 列出某分组下资源。
func (r *GatewayRoutes) ListByGroupID(groupID uint) ([]GatewayRoute, error) {
	var list []GatewayRoute
	if err := r.db.Where("gateway_group_id = ?", groupID).Order("position ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	// 不过期即清 reason：调度层用 until 判断是否仍暂停；reason 作为「上次错误」保留供管理端查看
	return list, nil
}

// FindByID 按主键查询。
func (r *GatewayRoutes) FindByID(id uint) (*GatewayRoute, error) {
	var item GatewayRoute
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func SameGatewayRouteSource(left, right GatewayRoute) bool {
	if left.NormalizeSourceKind() != right.NormalizeSourceKind() {
		return false
	}
	if left.NormalizeSourceKind() == GatewayRouteSourceProvider {
		return left.GatewayProviderID == right.GatewayProviderID
	}
	if left.SourceChannelID != right.SourceChannelID {
		return false
	}
	if left.SourceGroupID != nil || right.SourceGroupID != nil {
		return left.SourceGroupID != nil && right.SourceGroupID != nil &&
			*left.SourceGroupID == *right.SourceGroupID
	}
	return strings.TrimSpace(left.SourceGroupName) == strings.TrimSpace(right.SourceGroupName)
}

func (r *GatewayRoutes) BindSourceKeyIfUnchanged(expected *GatewayRoute, keyID int64, keyName, keyCipher string) error {
	if expected == nil || expected.ID == 0 {
		return ErrGatewayRouteChanged
	}
	query := r.db.Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).Model(&GatewayRoute{}).Where(map[string]any{
		"id":                  expected.ID,
		"gateway_group_id":    expected.GatewayGroupID,
		"source_channel_id":   expected.SourceChannelID,
		"gateway_provider_id": expected.GatewayProviderID,
		"source_group_id":     expected.SourceGroupID,
		"source_api_key_id":   expected.SourceAPIKeyID,
	})
	for _, field := range []struct{ column, value string }{
		{"source_kind", expected.SourceKind},
		{"source_group_name", expected.SourceGroupName},
		{"source_api_key_name", expected.SourceAPIKeyName},
		{"source_api_key_cipher", expected.SourceAPIKeyCipher},
	} {
		comparison := "COALESCE(?, '') = ?"
		if r.db.Dialector.Name() == "mysql" {
			comparison = "BINARY COALESCE(?, '') = BINARY ?"
		}
		query = query.Where(clause.Expr{SQL: comparison, Vars: []any{clause.Column{Name: field.column}, field.value}})
	}
	if expected.UpdatedAt.IsZero() {
		query = query.Where("updated_at IS NULL OR updated_at = ?", expected.UpdatedAt)
	} else {
		query = query.Where("updated_at = ?", expected.UpdatedAt)
	}
	result := query.Updates(map[string]any{
		"source_api_key_id":     keyID,
		"source_api_key_name":   keyName,
		"source_api_key_cipher": keyCipher,
		"updated_at":            time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGatewayRouteChanged
	}
	return nil
}

// SaveForGroup 全量保存某组下的路由列表。
//
// 重要：尽量保留已有 route.ID（原地 Update），避免「删表重建」导致 usage 日志里的
// route_id 悬空，从而丢失「上游密钥名 / 源分组」等展示字段。仅真正删除的路由才 Delete。
//
// position 有 (group_id, position) 唯一索引：先写临时 position 再写最终值，避免换序冲突。
func (r *GatewayRoutes) SaveForGroup(groupID uint, list []GatewayRoute) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing []GatewayRoute
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("gateway_group_id = ?", groupID).Find(&existing).Error; err != nil {
			return err
		}
		byID := make(map[uint]GatewayRoute, len(existing))
		for _, e := range existing {
			byID[e.ID] = e
		}
		keep := make(map[uint]struct{}, len(list))

		// 阶段 1：已有行先挪到临时 position，腾出唯一索引槽位
		tmpBase := 1_000_000
		for _, e := range existing {
			if err := tx.Model(&GatewayRoute{}).Where("id = ?", e.ID).
				Update("position", tmpBase+int(e.ID)).Error; err != nil {
				return err
			}
		}

		for i := range list {
			prev, hasPrev := byID[list[i].ID]
			if !hasPrev {
				// 客户端传来的 id 不属于本组（或不存在）→ 视为新建
				list[i].ID = 0
			} else {
				keep[list[i].ID] = struct{}{}
			}
			list[i].GatewayGroupID = groupID
			list[i].Position = i
			normalizeGatewayRoute(&list[i])

			// 保留已有上游密钥 / 暂停状态：来源未变时不丢
			sameSource := hasPrev && SameGatewayRouteSource(prev, list[i])
			if sameSource {
				list[i].SourceAPIKeyID = prev.SourceAPIKeyID
				list[i].SourceAPIKeyName = prev.SourceAPIKeyName
				list[i].SourceAPIKeyCipher = prev.SourceAPIKeyCipher
				list[i].TempUnschedulableUntil = prev.TempUnschedulableUntil
				list[i].TempUnschedulableReason = prev.TempUnschedulableReason
				list[i].TempUnschedulableAt = prev.TempUnschedulableAt
				list[i].TempUnschedulableRequestID = prev.TempUnschedulableRequestID
				list[i].RecoverSuccessStreak = prev.RecoverSuccessStreak
			} else {
				list[i].SourceAPIKeyID = 0
				list[i].SourceAPIKeyName = ""
				list[i].SourceAPIKeyCipher = ""
				list[i].TempUnschedulableUntil = nil
				list[i].TempUnschedulableReason = ""
				list[i].TempUnschedulableAt = nil
				list[i].TempUnschedulableRequestID = ""
				list[i].RecoverSuccessStreak = 0
			}

			if hasPrev {
				list[i].CreatedAt = prev.CreatedAt
				if err := tx.Save(&list[i]).Error; err != nil {
					return err
				}
			} else {
				list[i].ID = 0
				if err := tx.Create(&list[i]).Error; err != nil {
					return err
				}
			}
		}
		// 删除本次未提交的旧路由
		for id := range byID {
			if _, ok := keep[id]; ok {
				continue
			}
			if err := tx.Delete(&GatewayRoute{}, id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeGatewayRoute(item *GatewayRoute) {
	kind := item.NormalizeSourceKind()
	item.SourceKind = kind
	if kind == GatewayRouteSourceProvider {
		item.SourceChannelID = 0
		item.SourceGroupID = nil
		item.SourceGroupName = ""
	} else {
		item.GatewayProviderID = 0
	}
	if item.Weight <= 0 {
		item.Weight = 1
	}
	if strings.TrimSpace(item.RateConvertMode) == "" {
		item.RateConvertMode = "raw"
	}
	// UA 策略：非法/空 → passthrough；非 custom 清空自定义串
	mode := strings.ToLower(strings.TrimSpace(item.UserAgentMode))
	switch mode {
	case GatewayUserAgentModeGroup, GatewayUserAgentModeCustom:
		item.UserAgentMode = mode
	default:
		item.UserAgentMode = GatewayUserAgentModePassthrough
	}
	item.UserAgentCustom = strings.TrimSpace(item.UserAgentCustom)
	if item.UserAgentMode != GatewayUserAgentModeCustom {
		item.UserAgentCustom = ""
	}
	// 非 custom 时 rate_convert_value 仅作占位，与网关路由倍率换算一致置 1；
	// billing_rate_multiplier 由前端按源分组换算写入（原值=源 ratio），
	// 运行时计费以 RateForRoute 为准，此处不强制改写为 1。
	if strings.TrimSpace(item.RateConvertMode) != "custom" && item.RateConvertValue == 0 {
		item.RateConvertValue = 1
	}
	if item.BillingRateMultiplier <= 0 {
		item.BillingRateMultiplier = 1
	}
	if item.Concurrency <= 0 {
		item.Concurrency = 10
	}
	up := strings.ToLower(strings.TrimSpace(item.UpstreamProtocol))
	switch up {
	case GatewayUpstreamProtocolOpenAI, "chat", "chat_completions":
		item.UpstreamProtocol = GatewayUpstreamProtocolOpenAIChat
	case GatewayUpstreamProtocolOpenAIChat,
		GatewayUpstreamProtocolOpenAIResponses,
		GatewayUpstreamProtocolAnthropic,
		GatewayUpstreamProtocolAuto:
		item.UpstreamProtocol = up
	case "responses":
		item.UpstreamProtocol = GatewayUpstreamProtocolOpenAIResponses
	case "":
		item.UpstreamProtocol = GatewayUpstreamProtocolAuto
	default:
		item.UpstreamProtocol = GatewayUpstreamProtocolAuto
	}
	item.SourceGroupName = strings.TrimSpace(item.SourceGroupName)
}

// Update 全量保存。
func (r *GatewayRoutes) Update(item *GatewayRoute) error { return r.db.Save(item).Error }

// UpdateSourceKey 更新路由绑定的上游密钥密文。
func (r *GatewayRoutes) UpdateSourceKey(id uint, keyID int64, keyName, keyCipher string) error {
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(map[string]any{
		"source_api_key_id":     keyID,
		"source_api_key_name":   keyName,
		"source_api_key_cipher": keyCipher,
	}).Error
}

// UpdateSourceGroupSnapshot 补全源分组显示名（保留 source_group_id）。
func (r *GatewayRoutes) UpdateSourceGroupSnapshot(id uint, groupID *int64, groupName string) error {
	updates := map[string]any{
		"source_group_name": strings.TrimSpace(groupName),
	}
	if groupID != nil {
		updates["source_group_id"] = *groupID
	}
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(updates).Error
}

// SetTempUnschedulable 写入冷却截止时间、错误详情，以及触发失败的请求时间/ request_id。
func (r *GatewayRoutes) SetTempUnschedulable(id uint, until time.Time, reason string, failedAt time.Time, requestID string) error {
	if failedAt.IsZero() {
		failedAt = time.Now()
	}
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(map[string]any{
		"temp_unschedulable_until":      until,
		"temp_unschedulable_reason":     reason,
		"temp_unschedulable_at":         failedAt,
		"temp_unschedulable_request_id": strings.TrimSpace(requestID),
		"recover_success_streak":        0,
	}).Error
}

// ClearTempUnschedulable 手动清除暂停时间与错误信息。
func (r *GatewayRoutes) ClearTempUnschedulable(id uint) error {
	// 用 Exec 强制写 NULL；GORM Updates(map) 对 nil 在部分版本会跳过
	return r.db.Exec(
		`UPDATE gateway_routes SET temp_unschedulable_until = NULL, temp_unschedulable_reason = '', temp_unschedulable_at = NULL, temp_unschedulable_request_id = '', recover_success_streak = 0, updated_at = ? WHERE id = ?`,
		time.Now(), id,
	).Error
}

// ClearTempUnschedulableUntil 仅结束临时暂停（恢复调度），保留 reason / request_id / at 供排查。
func (r *GatewayRoutes) ClearTempUnschedulableUntil(id uint) error {
	return r.db.Exec(
		`UPDATE gateway_routes SET temp_unschedulable_until = NULL, updated_at = ? WHERE id = ?`,
		time.Now(), id,
	).Error
}

// NoteSuccessForPauseError 路由请求成功时调用：
// 1) 立刻结束冷却（恢复可调度）；
// 2) 若仍有错误残留，累加连续成功次数；
// 3) 达到 RouteRecoverSuccessClearStreak 后清空「已恢复/错误/清除」相关展示字段。
func (r *GatewayRoutes) NoteSuccessForPauseError(id uint) error {
	if id == 0 {
		return nil
	}
	now := time.Now()
	// 仅处理仍有暂停/错误残留的路由，避免无意义写放大
	if err := r.db.Exec(
		`UPDATE gateway_routes
		 SET recover_success_streak = recover_success_streak + 1,
		     temp_unschedulable_until = NULL,
		     updated_at = ?
		 WHERE id = ?
		   AND (
		     (temp_unschedulable_reason IS NOT NULL AND temp_unschedulable_reason != '')
		     OR temp_unschedulable_until IS NOT NULL
		     OR (temp_unschedulable_request_id IS NOT NULL AND temp_unschedulable_request_id != '')
		     OR temp_unschedulable_at IS NOT NULL
		   )`,
		now, id,
	).Error; err != nil {
		return err
	}
	return r.db.Exec(
		`UPDATE gateway_routes
		 SET temp_unschedulable_until = NULL,
		     temp_unschedulable_reason = '',
		     temp_unschedulable_at = NULL,
		     temp_unschedulable_request_id = '',
		     recover_success_streak = 0,
		     updated_at = ?
		 WHERE id = ? AND recover_success_streak >= ?`,
		now, id, RouteRecoverSuccessClearStreak,
	).Error
}

// GatewayUsageLogs 使用记录仓储。
type GatewayUsageLogs struct{ db *gorm.DB }

// NewGatewayUsageLogs 构造用量日志仓储。
func NewGatewayUsageLogs(db *gorm.DB) *GatewayUsageLogs { return &GatewayUsageLogs{db: db} }

// Create 插入记录。
func (r *GatewayUsageLogs) Create(item *GatewayUsageLog) error {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	return r.db.Create(item).Error
}

// CountBefore 统计 created_at < before 的记录数。
func (r *GatewayUsageLogs) CountBefore(before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, fmt.Errorf("before time required")
	}
	var n int64
	q := r.db.Model(&GatewayUsageLog{})
	if isSQLite(r.db) {
		q = q.Where("CAST(strftime('%s', created_at) AS INTEGER) < ?", before.Unix())
	} else {
		q = q.Where("created_at < ?", before)
	}
	err := q.Count(&n).Error
	return n, err
}

// CountAll 统计全部使用记录数。
func (r *GatewayUsageLogs) CountAll() (int64, error) {
	var n int64
	err := r.db.Model(&GatewayUsageLog{}).Count(&n).Error
	return n, err
}

// DeleteBefore 删除 created_at < before 的使用记录，返回删除行数。
func (r *GatewayUsageLogs) DeleteBefore(before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, fmt.Errorf("before time required")
	}
	q := r.db.Model(&GatewayUsageLog{})
	if isSQLite(r.db) {
		q = q.Where("CAST(strftime('%s', created_at) AS INTEGER) < ?", before.Unix())
	} else {
		q = q.Where("created_at < ?", before)
	}
	res := q.Delete(&GatewayUsageLog{})
	return res.RowsAffected, res.Error
}

// DeleteAll 删除全部使用记录，返回删除行数。
// GORM 要求 Delete 带条件，故使用 1=1。
func (r *GatewayUsageLogs) DeleteAll() (int64, error) {
	res := r.db.Where("1 = 1").Delete(&GatewayUsageLog{})
	return res.RowsAffected, res.Error
}

type GatewayUsageQuery struct {
	GatewayGroupID uint
	GatewayKeyID   uint
	ChannelID      uint
	Model          string
	// RequestID 模糊匹配 request_id
	RequestID   string
	RequestType *int
	SuccessOnly *bool
	// ResultMode 结果筛选（优先于 SuccessOnly）：
	//   "" / all — 全部
	//   success / fail — 单条成功/失败（success 不含客户端断开）
	//   client / client_disconnect — 客户端主动断开（error_type=client）
	//   multi — 含重试/顺延（同 request_id 多条 或 attempt>1）
	//   multi_success — 最终成功且链路含重试/顺延（如 2/2·顺延）
	//   multi_fail — 含重试/顺延且最终失败
	ResultMode string
	From       *time.Time
	To         *time.Time
	Page       int
	PageSize   int
}

// GatewayUsageLogItem 列表展示用：原始日志 + 关联名称（非多租户用户字段）。
// 源分组 / 上游密钥名优先用日志快照字段（GatewayUsageLog.Source*）；
// 旧数据无快照时再按 route_id 回填。
type GatewayUsageLogItem struct {
	GatewayUsageLog
	GatewayKeyName   string `json:"gateway_key_name,omitempty"`
	GatewayGroupName string `json:"gateway_group_name,omitempty"`
	ChannelName      string `json:"channel_name,omitempty"`
	// ProviderName 直连渠道名：优先日志快照，否则 enrich 回填
}

type GatewayUsagePage struct {
	Items    []GatewayUsageLogItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Pages    int                   `json:"pages"`
	SumCost  float64               `json:"sum_cost"`
}

type GatewayUsageStats struct {
	TotalRequests            int64   `json:"total_requests"`
	SuccessCount             int64   `json:"success_count"`
	ErrorCount               int64   `json:"error_count"`
	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	TotalCost                float64 `json:"total_cost"`
	TotalActualCost          float64 `json:"total_actual_cost"`
	AverageDurationMS        float64 `json:"average_duration_ms"`
	// RPM/TPM：近 5 分钟均值（对齐 sub2api），与筛选时间范围无关；TPM 仅 input+output
	RPM       int64                 `json:"rpm"`
	TPM       int64                 `json:"tpm"`
	Endpoints []GatewayEndpointStat `json:"endpoints"`
}

// GatewayUsageModelOption 使用记录模型下拉选项（按 requested_model 聚合）。
type GatewayUsageModelOption struct {
	Model string `json:"model"`
	Count int64  `json:"count"`
}

type GatewayEndpointStat struct {
	Endpoint string `json:"endpoint"`
	Requests int64  `json:"requests"`
}

// applyFilters 应用用量查询过滤。
func (r *GatewayUsageLogs) applyFilters(db *gorm.DB, q GatewayUsageQuery) *gorm.DB {
	if q.GatewayGroupID > 0 {
		db = db.Where("gateway_group_id = ?", q.GatewayGroupID)
	}
	if q.GatewayKeyID > 0 {
		db = db.Where("gateway_key_id = ?", q.GatewayKeyID)
	}
	if q.ChannelID > 0 {
		db = db.Where("channel_id = ?", q.ChannelID)
	}
	if m := strings.TrimSpace(q.Model); m != "" {
		// 下拉精确匹配请求模型 / 上游模型（兼容历史模糊：含 * 或 % 时走 LIKE）
		if strings.ContainsAny(m, "*%") {
			like := strings.ReplaceAll(m, "*", "%")
			if !strings.Contains(like, "%") {
				like = "%" + like + "%"
			}
			db = db.Where("requested_model LIKE ? OR upstream_model LIKE ?", like, like)
		} else {
			db = db.Where("requested_model = ? OR upstream_model = ?", m, m)
		}
	}
	if rid := strings.TrimSpace(q.RequestID); rid != "" {
		db = db.Where("request_id LIKE ?", "%"+rid+"%")
	}
	if q.RequestType != nil {
		db = db.Where("request_type = ?", *q.RequestType)
	}
	mode := strings.ToLower(strings.TrimSpace(q.ResultMode))
	switch mode {
	case "success":
		// 纯成功：不含客户端断开（新逻辑 success=true + error_type=client）
		db = db.Where(
			"success = ? AND (error_type IS NULL OR error_type = '' OR error_type != ?)",
			true, "client",
		)
	case "fail", "false", "error":
		// 真失败：不含客户端断开（旧数据可能 success=false + client）
		db = db.Where(
			"success = ? AND (error_type IS NULL OR error_type = '' OR error_type != ?)",
			false, "client",
		)
	case "client", "client_disconnect", "disconnect":
		// 流式 commit 后客户端主动断开（新旧 success 取值都覆盖）
		db = db.Where("error_type = ?", "client")
	case "multi", "retry", "failover", "chain":
		// 含重试/顺延：返回整条链路的所有 attempt 行，便于前端「查看全部 N 次尝试」
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0
			)`,
		)
	case "multi_success", "failover_success", "chain_success":
		// 最终成功且链路有多次尝试（展示上即「成功 2/2 · 顺延」一类）
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING (COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0)
					AND SUM(CASE WHEN success = 1 OR success = true THEN 1 ELSE 0 END) > 0
			)`,
		)
	case "multi_fail", "chain_fail":
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING (COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0)
					AND SUM(CASE WHEN success = 1 OR success = true THEN 1 ELSE 0 END) = 0
			)`,
		)
	default:
		// all 或空：兼容旧 SuccessOnly
		if q.SuccessOnly != nil {
			db = db.Where("success = ?", *q.SuccessOnly)
		}
	}
	// 时间过滤：SQLite 存的是带时区的文本（如 "2026-07-14 18:40:18+08:00"），
	// 若直接与 RFC3339（含 T 的 ISO）字符串比较会得到 0 行（"近1小时"失效）。
	// 统一用 unix 秒比较。注意：strftime('%s', …) 返回 TEXT，
	// 与整数绑定参数比较在 SQLite 下会得到 0 行，必须 CAST 成 INTEGER。
	if q.From != nil {
		if isSQLite(db) {
			db = db.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ?", q.From.Unix())
		} else {
			db = db.Where("created_at >= ?", *q.From)
		}
	}
	if q.To != nil {
		if isSQLite(db) {
			db = db.Where("CAST(strftime('%s', created_at) AS INTEGER) <= ?", q.To.Unix())
		} else {
			db = db.Where("created_at <= ?", *q.To)
		}
	}
	return db
}

func isSQLite(db *gorm.DB) bool {
	if db == nil || db.Dialector == nil {
		return false
	}
	return strings.EqualFold(db.Dialector.Name(), "sqlite")
}

// List 分页列表。
func (r *GatewayUsageLogs) List(q GatewayUsageQuery) (*GatewayUsagePage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var sum float64
	_ = db.Session(&gorm.Session{}).Select("COALESCE(SUM(actual_cost),0)").Scan(&sum).Error

	var rows []GatewayUsageLog
	offset := (q.Page - 1) * q.PageSize
	if err := db.Order("id DESC").Offset(offset).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	items, err := r.enrichUsageItems(rows)
	if err != nil {
		return nil, err
	}
	pages := int(total) / q.PageSize
	if int(total)%q.PageSize != 0 {
		pages++
	}
	return &GatewayUsagePage{
		Items:    items,
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Pages:    pages,
		SumCost:  sum,
	}, nil
}

// enrichUsageItems 补全用量展示字段。
func (r *GatewayUsageLogs) enrichUsageItems(rows []GatewayUsageLog) ([]GatewayUsageLogItem, error) {
	if len(rows) == 0 {
		return []GatewayUsageLogItem{}, nil
	}
	keyIDs := map[uint]struct{}{}
	groupIDs := map[uint]struct{}{}
	channelIDs := map[uint]struct{}{}
	providerIDs := map[uint]struct{}{}
	routeIDs := map[uint]struct{}{}
	for _, row := range rows {
		if row.GatewayKeyID > 0 {
			keyIDs[row.GatewayKeyID] = struct{}{}
		}
		if row.GatewayGroupID > 0 {
			groupIDs[row.GatewayGroupID] = struct{}{}
		}
		if row.ChannelID > 0 {
			channelIDs[row.ChannelID] = struct{}{}
		}
		if row.GatewayProviderID > 0 {
			providerIDs[row.GatewayProviderID] = struct{}{}
		}
		if row.RouteID > 0 {
			routeIDs[row.RouteID] = struct{}{}
		}
	}

	keyNames := map[uint]string{}
	if len(keyIDs) > 0 {
		ids := uintKeys(keyIDs)
		var keys []GatewayKey
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&keys).Error; err != nil {
			return nil, err
		}
		for _, k := range keys {
			keyNames[k.ID] = k.Name
		}
	}
	groupNames := map[uint]string{}
	if len(groupIDs) > 0 {
		ids := uintKeys(groupIDs)
		var groups []GatewayGroup
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&groups).Error; err != nil {
			return nil, err
		}
		for _, g := range groups {
			groupNames[g.ID] = g.Name
		}
	}
	channelNames := map[uint]string{}
	if len(channelIDs) > 0 {
		ids := uintKeys(channelIDs)
		var channels []Channel
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return nil, err
		}
		for _, ch := range channels {
			channelNames[ch.ID] = ch.Name
		}
	}
	providerNames := map[uint]string{}
	if len(providerIDs) > 0 {
		ids := uintKeys(providerIDs)
		var providers []GatewayProvider
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&providers).Error; err != nil {
			return nil, err
		}
		for _, p := range providers {
			providerNames[p.ID] = p.Name
		}
	}
	// 仅对「快照为空 / 仅 id 占位」的旧日志按 route_id 回填
	needRouteLookup := false
	for _, row := range rows {
		sg := strings.TrimSpace(row.SourceGroupName)
		if strings.TrimSpace(row.SourceAPIKeyName) == "" ||
			sg == "" || isUsageSourceGroupIDPlaceholder(sg) ||
			(sg == "" && row.SourceGroupID == nil) {
			if row.RouteID > 0 {
				needRouteLookup = true
				break
			}
		}
	}
	routeMeta := map[uint]struct {
		SourceGroupName  string
		SourceGroupID    *int64
		SourceAPIKeyName string
		SourceAPIKeyID   int64
	}{}
	if needRouteLookup && len(routeIDs) > 0 {
		ids := uintKeys(routeIDs)
		var routes []GatewayRoute
		if err := r.db.Select(
			"id", "source_group_name", "source_group_id", "source_api_key_name", "source_api_key_id",
		).Where("id IN ?", ids).Find(&routes).Error; err != nil {
			return nil, err
		}
		for _, rt := range routes {
			sg := strings.TrimSpace(rt.SourceGroupName)
			// 路由上已是真实名则原样用；空名才回退 id:N
			if sg == "" && rt.SourceGroupID != nil {
				sg = fmt.Sprintf("id:%d", *rt.SourceGroupID)
			}
			routeMeta[rt.ID] = struct {
				SourceGroupName  string
				SourceGroupID    *int64
				SourceAPIKeyName string
				SourceAPIKeyID   int64
			}{
				SourceGroupName:  sg,
				SourceGroupID:    rt.SourceGroupID,
				SourceAPIKeyName: strings.TrimSpace(rt.SourceAPIKeyName),
				SourceAPIKeyID:   rt.SourceAPIKeyID,
			}
		}
	}

	out := make([]GatewayUsageLogItem, 0, len(rows))
	for _, row := range rows {
		// 规范化快照展示：有 id 无 name 时补 "id:N"
		if strings.TrimSpace(row.SourceGroupName) == "" && row.SourceGroupID != nil {
			row.SourceGroupName = fmt.Sprintf("id:%d", *row.SourceGroupID)
		}
		// 旧日志：无快照 / 仅 id 占位时，从仍存活的 route 回填真实分组名
		if m, ok := routeMeta[row.RouteID]; ok {
			if strings.TrimSpace(row.SourceAPIKeyName) == "" && m.SourceAPIKeyName != "" {
				row.SourceAPIKeyName = m.SourceAPIKeyName
			}
			if row.SourceAPIKeyID == 0 && m.SourceAPIKeyID != 0 {
				row.SourceAPIKeyID = m.SourceAPIKeyID
			}
			routeName := strings.TrimSpace(m.SourceGroupName)
			rowName := strings.TrimSpace(row.SourceGroupName)
			if routeName != "" && !isUsageSourceGroupIDPlaceholder(routeName) {
				if rowName == "" || isUsageSourceGroupIDPlaceholder(rowName) {
					row.SourceGroupName = routeName
				}
			} else if rowName == "" && routeName != "" {
				row.SourceGroupName = routeName
			}
			if row.SourceGroupID == nil && m.SourceGroupID != nil {
				row.SourceGroupID = m.SourceGroupID
			}
		}
		if strings.TrimSpace(row.ProviderName) == "" && row.GatewayProviderID > 0 {
			row.ProviderName = providerNames[row.GatewayProviderID]
		}
		// 直连渠道：无监控渠道名时用 provider 名填 channel_name，便于旧 UI 展示
		chName := channelNames[row.ChannelID]
		if chName == "" && row.ProviderName != "" {
			chName = row.ProviderName
		}
		out = append(out, GatewayUsageLogItem{
			GatewayUsageLog:  row,
			GatewayKeyName:   keyNames[row.GatewayKeyID],
			GatewayGroupName: groupNames[row.GatewayGroupID],
			ChannelName:      chName,
		})
	}
	return out, nil
}

func uintKeys(m map[uint]struct{}) []uint {
	ids := make([]uint, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	return ids
}

// isUsageSourceGroupIDPlaceholder 识别 usage 快照里「id:31」这类无真实分组名的占位。
func isUsageSourceGroupIDPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	lower = strings.ReplaceAll(lower, "：", ":")
	if strings.HasPrefix(lower, "id:") {
		rest := strings.TrimPrefix(lower, "id:")
		return rest != "" && isAllASCIIDigits(rest)
	}
	if strings.HasPrefix(lower, "源id:") {
		rest := strings.TrimPrefix(lower, "源id:")
		return rest != "" && isAllASCIIDigits(rest)
	}
	return false
}

func isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Stats 用量聚合统计。
func (r *GatewayUsageLogs) Stats(q GatewayUsageQuery) (*GatewayUsageStats, error) {
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	type aggRow struct {
		TotalRequests            int64
		SuccessCount             int64
		ErrorCount               int64
		TotalInputTokens         int64
		TotalOutputTokens        int64
		TotalCacheCreationTokens int64
		TotalCacheReadTokens     int64
		TotalCost                float64
		TotalActualCost          float64
		AvgDurationMS            float64
	}
	var row aggRow
	if err := db.Session(&gorm.Session{}).Select(`
		COUNT(*) as total_requests,
		COALESCE(SUM(CASE WHEN success THEN 1 ELSE 0 END),0) as success_count,
		COALESCE(SUM(CASE WHEN success THEN 0 ELSE 1 END),0) as error_count,
		COALESCE(SUM(input_tokens),0) as total_input_tokens,
		COALESCE(SUM(output_tokens),0) as total_output_tokens,
		COALESCE(SUM(cache_creation_tokens),0) as total_cache_creation_tokens,
		COALESCE(SUM(cache_read_tokens),0) as total_cache_read_tokens,
		COALESCE(SUM(total_cost),0) as total_cost,
		COALESCE(SUM(actual_cost),0) as total_actual_cost,
		COALESCE(AVG(duration_ms),0) as avg_duration_ms
	`).Scan(&row).Error; err != nil {
		return nil, err
	}
	var endpoints []GatewayEndpointStat
	_ = r.applyFilters(r.db.Model(&GatewayUsageLog{}), q).
		Select("inbound_endpoint as endpoint, COUNT(*) as requests").
		Where("inbound_endpoint <> ''").
		Group("inbound_endpoint").
		Order("requests DESC").
		Limit(20).
		Scan(&endpoints).Error

	totalTokens := row.TotalInputTokens + row.TotalOutputTokens + row.TotalCacheCreationTokens + row.TotalCacheReadTokens
	rpm, tpm := r.performanceRPMAndTPM(q)
	return &GatewayUsageStats{
		TotalRequests:            row.TotalRequests,
		SuccessCount:             row.SuccessCount,
		ErrorCount:               row.ErrorCount,
		TotalInputTokens:         row.TotalInputTokens,
		TotalOutputTokens:        row.TotalOutputTokens,
		TotalCacheCreationTokens: row.TotalCacheCreationTokens,
		TotalCacheReadTokens:     row.TotalCacheReadTokens,
		TotalTokens:              totalTokens,
		TotalCost:                row.TotalCost,
		TotalActualCost:          row.TotalActualCost,
		AverageDurationMS:        row.AvgDurationMS,
		RPM:                      rpm,
		TPM:                      tpm,
		Endpoints:                endpoints,
	}, nil
}

// ListModels 聚合使用记录中的 requested_model，供筛选下拉。
// 沿用组/密钥/时间筛选；忽略 model / result / request_id（避免自过滤）。
func (r *GatewayUsageLogs) ListModels(q GatewayUsageQuery) ([]GatewayUsageModelOption, error) {
	q.Model = ""
	q.RequestID = ""
	q.ResultMode = ""
	q.SuccessOnly = nil
	q.RequestType = nil

	type row struct {
		Model string
		Count int64
	}
	var rows []row
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	if err := db.Session(&gorm.Session{}).
		Select("requested_model as model, COUNT(*) as count").
		Where("requested_model IS NOT NULL AND requested_model != ''").
		Group("requested_model").
		Order("count DESC, requested_model ASC").
		Limit(500).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]GatewayUsageModelOption, 0, len(rows))
	for _, row := range rows {
		m := strings.TrimSpace(row.Model)
		if m == "" {
			continue
		}
		out = append(out, GatewayUsageModelOption{Model: m, Count: row.Count})
	}
	return out, nil
}

// performanceRPMAndTPM 近 5 分钟平均：RPM = 请求数/5，TPM = (input+output)/5。
// 沿用组/密钥/模型等筛选，但忽略调用方传入的 From/To（实时吞吐与统计窗口无关）。
func (r *GatewayUsageLogs) performanceRPMAndTPM(q GatewayUsageQuery) (rpm, tpm int64) {
	const windowMinutes int64 = 5
	from := time.Now().Add(-time.Duration(windowMinutes) * time.Minute)
	qPerf := q
	qPerf.From = &from
	qPerf.To = nil
	type perfRow struct {
		RequestCount int64
		TokenCount   int64
	}
	var row perfRow
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), qPerf)
	if err := db.Session(&gorm.Session{}).Select(`
		COUNT(*) as request_count,
		COALESCE(SUM(input_tokens + output_tokens), 0) as token_count
	`).Scan(&row).Error; err != nil {
		return 0, 0
	}
	return row.RequestCount / windowMinutes, row.TokenCount / windowMinutes
}

// ModelPriceOverrides 价格覆盖表。
type ModelPriceOverrides struct{ db *gorm.DB }

// NewModelPriceOverrides 构造模型价目覆盖仓储。
func NewModelPriceOverrides(db *gorm.DB) *ModelPriceOverrides {
	return &ModelPriceOverrides{db: db}
}

// List 分页列表。
func (r *ModelPriceOverrides) List() ([]ModelPriceOverride, error) {
	var list []ModelPriceOverride
	if err := r.db.Order("model_name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *ModelPriceOverrides) FindByModel(name string) (*ModelPriceOverride, error) {
	var item ModelPriceOverride
	if err := r.db.Where("model_name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Upsert 按模型名插入或更新价目覆盖。
func (r *ModelPriceOverrides) Upsert(item *ModelPriceOverride) error {
	item.ModelName = strings.TrimSpace(item.ModelName)
	if item.ID == 0 {
		var existing ModelPriceOverride
		if err := r.db.Where("model_name = ?", item.ModelName).First(&existing).Error; err == nil {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}
	}
	return r.db.Save(item).Error
}

// Delete 按主键删除。
func (r *ModelPriceOverrides) Delete(id uint) error {
	return r.db.Delete(&ModelPriceOverride{}, id).Error
}

// ListEnabledMap 返回启用中的价目覆盖 map。
func (r *ModelPriceOverrides) ListEnabledMap() (map[string]ModelPriceOverride, error) {
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	out := make(map[string]ModelPriceOverride, len(list))
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		out[item.ModelName] = item
	}
	return out, nil
}
