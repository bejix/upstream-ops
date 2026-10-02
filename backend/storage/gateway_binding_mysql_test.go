package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	gormMySQL "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openGatewayBindingMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("UOPS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("UOPS_TEST_MYSQL_DSN is not set; requires a disposable MySQL server with CREATE DATABASE permission")
	}
	config, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test MySQL DSN")
	}
	config.DBName = ""
	config.ParseTime = true
	admin, err := gorm.Open(gormMySQL.Open(config.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("cannot connect to disposable MySQL server")
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })
	databaseName := fmt.Sprintf("uops_binding_test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE `" + databaseName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE `" + databaseName + "`").Error; err != nil {
			t.Errorf("drop isolated test database: %v", err)
		}
	})
	config.DBName = databaseName
	db, err := gorm.Open(gormMySQL.Open(config.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&GatewayRoute{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGatewayRoutesMySQLBindingSnapshot(t *testing.T) {
	db := openGatewayBindingMySQLDB(t)
	routes := NewGatewayRoutes(db)
	for _, scenario := range []string{"normal", "legacy-null", "case-only-source", "case-only-cipher"} {
		t.Run(scenario, func(t *testing.T) {
			target := gatewayBindingFixture()
			target.GatewayGroupID = uint(time.Now().UnixNano() % 1000000000)
			if err := db.Create(&target).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "legacy-null" {
				if err := db.Model(&target).UpdateColumns(map[string]any{"source_api_key_cipher": nil, "updated_at": nil}).Error; err != nil {
					t.Fatal(err)
				}
			}
			expected, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			column := ""
			value := ""
			switch scenario {
			case "case-only-source":
				column, value = "source_group_name", strings.ToUpper(target.SourceGroupName)
			case "case-only-cipher":
				column, value = "source_api_key_cipher", strings.ToUpper(target.SourceAPIKeyCipher)
			}
			if column != "" {
				if err := db.Model(&target).UpdateColumn(column, value).Error; err != nil {
					t.Fatal(err)
				}
			}
			err = routes.BindSourceKeyIfUnchanged(expected, 52, "new-key", "new-cipher")
			if column != "" {
				if !errors.Is(err, ErrGatewayRouteChanged) {
					t.Fatalf("case-only snapshot change must conflict: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := routes.SaveForGroup(target.GatewayGroupID, []GatewayRoute{target}); err != nil {
				t.Fatal(err)
			}
			current, err := routes.FindByID(target.ID)
			if err != nil || current.SourceAPIKeyID != 52 || current.SourceAPIKeyCipher != "new-cipher" {
				t.Fatalf("saving stale configuration lost the new binding: %v", err)
			}
		})
	}
}

func TestGatewayRoutesMySQLSaveLocksBindingSnapshot(t *testing.T) {
	db := openGatewayBindingMySQLDB(t)
	routes := NewGatewayRoutes(db)
	target := gatewayBindingFixture()
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	expected, err := routes.FindByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	type saveContextKey struct{}
	if err := db.Callback().Query().After("gorm:query").Register("test_pause_locked_routes", func(query *gorm.DB) {
		if query.Statement.Context.Value(saveContextKey{}) != true {
			return
		}
		close(locked)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	saveDone := make(chan error, 1)
	target.Weight++
	go func() {
		saveDone <- NewGatewayRoutes(db.WithContext(context.WithValue(ctx, saveContextKey{}, true))).SaveForGroup(target.GatewayGroupID, []GatewayRoute{target})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("group save did not read its snapshot")
	}
	bindDone := make(chan error, 1)
	go func() {
		bindDone <- NewGatewayRoutes(db.WithContext(ctx)).BindSourceKeyIfUnchanged(expected, 52, "new-key", "new-cipher")
	}()
	select {
	case err := <-bindDone:
		t.Fatalf("binding must wait for the locked group snapshot: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	released = true
	if err := <-saveDone; err != nil {
		t.Fatal(err)
	}
	if err := <-bindDone; !errors.Is(err, ErrGatewayRouteChanged) {
		t.Fatalf("binding read before the group save must conflict: %v", err)
	}
	current, err := routes.FindByID(target.ID)
	if err != nil || current.Weight != target.Weight || current.SourceAPIKeyID != expected.SourceAPIKeyID {
		t.Fatalf("concurrent save produced an unexpected binding: %v", err)
	}
}
