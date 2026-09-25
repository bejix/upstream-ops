package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// registerDashboard 提供首页所需聚合视图。
func registerDashboard(g *gin.RouterGroup, d *Deps) {
	g.GET("/dashboard/summary", func(c *gin.Context) { dashboardSummary(c, d) })
	g.GET("/dashboard/balance-trend", func(c *gin.Context) { dashboardBalanceTrend(c, d) })
	g.GET("/dashboard/cost-trend", func(c *gin.Context) { dashboardCostTrend(c, d) })
}

type dashboardLowest struct {
	ChannelID uint     `json:"channel_id"`
	Name      string   `json:"name"`
	Balance   *float64 `json:"balance"`
}

type dashboardChannelStat struct {
	ID             uint     `json:"id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	MonitorEnabled bool     `json:"monitor_enabled"`
	LastBalance    *float64 `json:"last_balance,omitempty"`
	TodayCost      *float64 `json:"today_cost,omitempty"`
	TotalCost      *float64 `json:"total_cost,omitempty"`
	LastError      string   `json:"last_error,omitempty"`
}

func dashboardSummary(c *gin.Context, d *Deps) {
	channels, err := d.Channels.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	stats := make([]dashboardChannelStat, 0, len(channels))
	var totalBalance float64
	var todayTotalCost float64
	var totalCost float64
	var lowest *dashboardLowest
	var activeCount, failedCount int
	now := costNow()

	for _, ch := range channels {
		// 暂停监控后缓存的今日消费不会刷新，跨天后按 0 展示与汇总。
		todayCost := ch.EffectiveTodayCost(now)
		stat := dashboardChannelStat{
			ID:             ch.ID,
			Name:           ch.Name,
			Type:           string(ch.Type),
			MonitorEnabled: ch.MonitorEnabled,
			LastBalance:    ch.LastBalance,
			TodayCost:      todayCost,
			TotalCost:      ch.TotalCost,
			LastError:      ch.LastError,
		}
		stats = append(stats, stat)
		if ch.LastError != "" {
			failedCount++
		} else if ch.MonitorEnabled {
			activeCount++
		}
		if ch.LastBalance != nil {
			totalBalance += *ch.LastBalance
			if lowest == nil || (lowest.Balance == nil) || (*ch.LastBalance < *lowest.Balance) {
				bal := *ch.LastBalance
				lowest = &dashboardLowest{ChannelID: ch.ID, Name: ch.Name, Balance: &bal}
			}
		}
		if todayCost != nil {
			todayTotalCost += *todayCost
		}
		if ch.TotalCost != nil {
			totalCost += *ch.TotalCost
		}
	}

	recentChanges, err := d.Rates.ListChanges(0, 10)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"total_channels":      len(channels),
			"active_channels":     activeCount,
			"failed_channels":     failedCount,
			"total_balance":       totalBalance,
			"today_total_cost":    todayTotalCost,
			"total_cost":          totalCost,
			"lowest_balance":      lowest,
			"channels":            stats,
			"recent_rate_changes": recentChanges,
		},
	})
}

func dashboardBalanceTrend(c *gin.Context, d *Deps) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days <= 0 {
		days = 7
	}
	trend, err := d.Rates.AggregateBalanceTrend(days)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": trend})
}

func dashboardCostTrend(c *gin.Context, d *Deps) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days <= 0 {
		days = 7
	}
	trend, err := d.Rates.AggregateCostTrend(days)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": trend})
}
