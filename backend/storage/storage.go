// Package storage 提供 GORM 仓储与领域模型持久化。
package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqliteDriver "github.com/glebarez/sqlite"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DBDriver string

const (
	DBDriverSQLite DBDriver = "sqlite"
	DBDriverMySQL  DBDriver = "mysql"
)

type DBConfig struct {
	Driver       DBDriver
	Path         string
	Host         string
	Port         int
	User         string
	Password     string
	Name         string
	MaxOpenConns int
	MaxIdleConns int
}

func (c DBConfig) SQLitePath() string {
	if strings.TrimSpace(c.Path) != "" {
		return c.Path
	}
	return "./data/upstream-ops.db"
}

func (c DBConfig) MySQLDSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Name,
	)
}

// newGormLogger 关掉 GORM 默认 logger 对 ErrRecordNotFound 的告警噪音。
//
// 业务代码（如 Rates.Upsert）显式处理了"找不到就插入"，这种情况下 GORM 默认仍会
// 把 record not found 当 Warn 打出来，造成日志看起来满是错误其实没问题。
// IgnoreRecordNotFoundError = true 可以静默这类预期内的"未找到"。
func newGormLogger() logger.Interface {
	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
}

func Open(cfg DBConfig) (*gorm.DB, error) {
	driver := DBDriver(strings.ToLower(string(cfg.Driver)))
	if driver == "" {
		driver = DBDriverSQLite
	}

	var dialector gorm.Dialector
	switch driver {
	case DBDriverSQLite:
		path := cfg.SQLitePath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
		dialector = sqliteDriver.Open(path)
	case DBDriverMySQL:
		dialector = mysqlDriver.Open(cfg.MySQLDSN())
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	switch driver {
	case DBDriverSQLite:
		if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			return nil, fmt.Errorf("set sqlite journal mode: %w", err)
		}
		if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
			return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	default:
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	return db, nil
}

// AutoMigrate 启动时自动同步表结构。
func AutoMigrate(db *gorm.DB) error {
	if err := dropDeletedAtColumns(db); err != nil {
		return err
	}
	if err := dropObsoleteRateSiteColumns(db); err != nil {
		return err
	}
	// 仅 AutoMigrate 当前模型（网关未发布，不做「密钥→组」等历史数据迁移）
	if err := db.AutoMigrate(
		&Channel{},
		&AuthSession{},
		&CaptchaConfig{},
		&RateSnapshot{},
		&RateChangeLog{},
		&UpstreamAnnouncement{},
		&BalanceSnapshot{},
		&CostSnapshot{},
		&NotificationChannel{},
		&NotificationLog{},
		&NotificationCooldown{},
		&MonitorLog{},
		&GatewayGroup{},
		&GatewayKey{},
		&GatewayRoute{},
		&GatewayProvider{},
		&GatewayUsageLog{},
		&ModelPriceOverride{},
	); err != nil {
		return err
	}
	return backfillChannelTodayCostAt(db)
}

// backfillChannelTodayCostAt 给升级前写入的 today_cost 补上采集时间 today_cost_at。
//
// 旧版本只写 today_cost 不记时间。不能在读取时拿 last_balance_at 代替：余额单独刷新
// （兑换码充值、消费采集失败等）会把它更新到今天，已经过期的 today_cost 又会被当成今日数据。
// 所以在迁移时一次性回填：优先取该渠道最近一条消费快照的 sampled_at（与 today_cost
// 同一轮采集写入）；快照已被清理的，退回当时的 last_balance_at。两者都没有的保持 NULL，
// EffectiveTodayCost 原样返回。
//
// 只处理 today_cost_at IS NULL 的行，重复执行无副作用；子查询读的是另一张表，
// SQLite 与 MySQL 的 UPDATE 都支持。
func backfillChannelTodayCostAt(db *gorm.DB) error {
	const pending = "today_cost_at IS NULL AND today_cost IS NOT NULL"
	if err := db.Exec("UPDATE channels SET today_cost_at = (" +
		"SELECT MAX(cost_snapshots.sampled_at) FROM cost_snapshots WHERE cost_snapshots.channel_id = channels.id" +
		") WHERE " + pending +
		" AND EXISTS (SELECT 1 FROM cost_snapshots WHERE cost_snapshots.channel_id = channels.id)").Error; err != nil {
		return fmt.Errorf("backfill channels.today_cost_at from cost_snapshots: %w", err)
	}
	if err := db.Exec("UPDATE channels SET today_cost_at = last_balance_at WHERE " + pending +
		" AND last_balance_at IS NOT NULL").Error; err != nil {
		return fmt.Errorf("backfill channels.today_cost_at from last_balance_at: %w", err)
	}
	return nil
}

func dropObsoleteRateSiteColumns(db *gorm.DB) error {
	targets := []struct {
		table string
		model any
	}{
		{table: "rate_snapshots", model: &RateSnapshot{}},
		{table: "rate_change_logs", model: &RateChangeLog{}},
	}
	for _, target := range targets {
		if !db.Migrator().HasTable(target.model) {
			continue
		}
		hasColumn, err := tableHasColumn(db, target.table, "site_id")
		if err != nil {
			return fmt.Errorf("inspect obsolete %s.site_id: %w", target.table, err)
		}
		if !hasColumn {
			continue
		}
		if err := db.Migrator().DropColumn(target.model, "site_id"); err != nil {
			return fmt.Errorf("drop obsolete %s.site_id: %w", target.table, err)
		}
		hasColumn, err = tableHasColumn(db, target.table, "site_id")
		if err != nil {
			return fmt.Errorf("inspect obsolete %s.site_id after drop: %w", target.table, err)
		}
		if hasColumn && db.Dialector.Name() == "sqlite" {
			if err := db.Exec("ALTER TABLE " + target.table + " DROP COLUMN site_id").Error; err != nil {
				return fmt.Errorf("drop sqlite obsolete %s.site_id: %w", target.table, err)
			}
		}
	}
	return nil
}

func dropDeletedAtColumns(db *gorm.DB) error {
	targets := []struct {
		table string
		model any
	}{
		{table: "channels", model: &Channel{}},
		{table: "captcha_configs", model: &CaptchaConfig{}},
		{table: "notification_channels", model: &NotificationChannel{}},
	}

	for _, target := range targets {
		if !db.Migrator().HasTable(target.model) {
			continue
		}
		hasColumn, err := tableHasColumn(db, target.table, "deleted_at")
		if err != nil {
			return fmt.Errorf("inspect %s.deleted_at: %w", target.table, err)
		}
		if !hasColumn {
			continue
		}
		if err := db.Exec("DELETE FROM " + target.table + " WHERE deleted_at IS NOT NULL").Error; err != nil {
			return fmt.Errorf("delete soft-deleted rows from %s: %w", target.table, err)
		}
		if db.Migrator().HasIndex(target.model, "idx_"+target.table+"_deleted_at") {
			if err := db.Migrator().DropIndex(target.model, "idx_"+target.table+"_deleted_at"); err != nil {
				return fmt.Errorf("drop %s deleted_at index: %w", target.table, err)
			}
		}
		if err := db.Migrator().DropColumn(target.model, "deleted_at"); err != nil {
			return fmt.Errorf("drop %s.deleted_at: %w", target.table, err)
		}
		hasColumn, err = tableHasColumn(db, target.table, "deleted_at")
		if err != nil {
			return fmt.Errorf("inspect %s.deleted_at after drop: %w", target.table, err)
		}
		if hasColumn && db.Dialector.Name() == "sqlite" {
			if err := db.Exec("ALTER TABLE " + target.table + " DROP COLUMN deleted_at").Error; err != nil {
				return fmt.Errorf("drop sqlite %s.deleted_at: %w", target.table, err)
			}
		}
	}
	return nil
}

func tableHasColumn(db *gorm.DB, table, column string) (bool, error) {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false, err
	}
	for _, c := range columns {
		if strings.EqualFold(c.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}
