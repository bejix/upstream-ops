package storage

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// Channels 渠道仓库。
type Channels struct{ db *gorm.DB }

func NewChannels(db *gorm.DB) *Channels { return &Channels{db: db} }

func (r *Channels) Create(c *Channel) error { return r.db.Create(c).Error }
func (r *Channels) Update(c *Channel) error { return r.db.Save(c).Error }
func (r *Channels) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := tx.Select("id", "name").First(&channel, id).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Where("channel_id = ?", id).Delete(&AuthSession{}).Error; err != nil {
			return err
		}
		for _, model := range []any{
			&RateSnapshot{},
			&RateChangeLog{},
			&BalanceSnapshot{},
			&CostSnapshot{},
			&MonitorLog{},
			&NotificationCooldown{},
			&UpstreamAnnouncement{},
		} {
			if err := tx.Where("channel_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("upstream_channel_id = ?", id).Delete(&NotificationLog{}).Error; err != nil {
			return err
		}
		if channel.Name != "" {
			pattern := "%" + likeEscaper.Replace(channel.Name) + "%"
			if err := tx.Where("upstream_channel_id = 0 AND (subject LIKE ? ESCAPE '!' OR body LIKE ? ESCAPE '!')", pattern, pattern).
				Delete(&NotificationLog{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&Channel{}, id).Error
	})
}
func (r *Channels) FindByID(id uint) (*Channel, error) {
	var c Channel
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}
func (r *Channels) List() ([]Channel, error) {
	var list []Channel
	if err := r.db.Order("sort_order DESC").Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListPage 按 filter 搜索、筛选、排序后分页，pageSize 为 -1 时返回全部。
// total 是筛选后的总数；filter 含未知取值时返回错误（见 ChannelListFilter.Normalize）。
//
// 按名称 / 账号排序时，SQL 只负责筛选，排序和分页在内存里做（见 sortChannelsByText）。
// 渠道数量有限，一次读出全部匹配行的开销可以接受。
func (r *Channels) ListPage(page, pageSize int, filter ChannelListFilter) ([]Channel, int64, error) {
	filter, err := filter.Normalize()
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 && pageSize != -1 {
		pageSize = 20
	}
	if filter.sortsChannelsInMemory() {
		var all []Channel
		if err := applyChannelListFilter(r.db.Model(&Channel{}), filter).Find(&all).Error; err != nil {
			return nil, 0, err
		}
		sortChannelsByText(all, filter)
		return pageChannels(all, page, pageSize), int64(len(all)), nil
	}
	var total int64
	if err := applyChannelListFilter(r.db.Model(&Channel{}), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Channel
	q := applyChannelListOrder(applyChannelListFilter(r.db.Model(&Channel{}), filter), filter)
	if pageSize != -1 {
		q = q.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
func (r *Channels) ListMonitorEnabled() ([]Channel, error) {
	var list []Channel
	if err := r.db.Where("monitor_enabled = ?", true).Order("sort_order DESC").Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
func (r *Channels) UpdateBalance(id uint, balance float64, at any, lastErr string) error {
	return r.db.Model(&Channel{}).Where("id = ?", id).Updates(map[string]any{
		"last_balance":    balance,
		"last_balance_at": at,
		"last_error":      lastErr,
	}).Error
}

// UpdateCosts 写入最近一次消费采集结果；at 为采集时间，用于"今日消费"跨天判断。
func (r *Channels) UpdateCosts(id uint, todayCost float64, totalCost float64, at time.Time) error {
	return r.db.Model(&Channel{}).Where("id = ?", id).Updates(map[string]any{
		"today_cost":    todayCost,
		"today_cost_at": at,
		"total_cost":    totalCost,
	}).Error
}
func (r *Channels) SetLastError(id uint, msg string) error {
	return r.db.Model(&Channel{}).Where("id = ?", id).Update("last_error", msg).Error
}
