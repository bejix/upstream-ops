package storage

import (
	"bytes"
	"errors"
	"log"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openGatewayBindingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(DBConfig{Driver: DBDriverSQLite, Path: filepath.Join(t.TempDir(), "binding.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&GatewayRoute{}); err != nil {
		t.Fatalf("migrate routes: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func gatewayBindingFixture() GatewayRoute {
	groupID := int64(11)
	created := time.Date(2026, time.September, 1, 2, 3, 4, 0, time.UTC)
	paused := created.Add(time.Hour)
	until := paused.Add(time.Minute)
	return GatewayRoute{
		GatewayGroupID: 7, Position: 0, SourceKind: GatewayRouteSourceMonitor,
		SourceChannelID: 3, SourceGroupID: &groupID, SourceGroupName: "source-group",
		Weight: 7, RateConvertMode: "custom", RateConvertValue: 6.5,
		BillingRateMultiplier: 0.125, Enabled: true,
		ModelMappingJSON: `{"model-alias":"model-upstream"}`, UpstreamProtocol: "openai",
		Concurrency: 17, UserAgentMode: GatewayUserAgentModeCustom, UserAgentCustom: "binding-test",
		SourceAPIKeyID: 41, SourceAPIKeyName: "existing-key", SourceAPIKeyCipher: "existing-cipher",
		TempUnschedulableUntil: &until, TempUnschedulableReason: "rate limited",
		TempUnschedulableAt: &paused, TempUnschedulableRequestID: "existing-request",
		RecoverSuccessStreak: 2, CreatedAt: created, UpdatedAt: created.Add(time.Minute),
	}
}

func TestGatewayRoutesSaveForGroupSourceBinding(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(*GatewayRoute)
		change   func(*GatewayRoute)
		preserve bool
	}{
		{name: "same group ID renamed", change: func(route *GatewayRoute) { route.SourceGroupName = "renamed" }, preserve: true},
		{name: "same name different group ID", change: func(route *GatewayRoute) { groupID := int64(12); route.SourceGroupID = &groupID }},
		{name: "group ID removed", change: func(route *GatewayRoute) { route.SourceGroupID = nil }},
		{name: "group ID added", prepare: func(route *GatewayRoute) { route.SourceGroupID = nil }, change: func(route *GatewayRoute) { groupID := int64(11); route.SourceGroupID = &groupID }},
		{name: "named group whitespace", prepare: func(route *GatewayRoute) { route.SourceGroupID = nil }, change: func(route *GatewayRoute) { route.SourceGroupName = " source-group\t" }, preserve: true},
		{name: "NewAPI named group renamed", prepare: func(route *GatewayRoute) { route.SourceGroupID = nil }, change: func(route *GatewayRoute) { route.SourceGroupName = "renamed" }},
		{name: "channel replaced", change: func(route *GatewayRoute) { route.SourceChannelID++ }},
		{name: "configuration only", change: func(route *GatewayRoute) { route.Weight++; route.BillingRateMultiplier = 0.25 }, preserve: true},
		{name: "provider unchanged", prepare: func(route *GatewayRoute) {
			route.SourceKind = GatewayRouteSourceProvider
			route.GatewayProviderID = 9
			route.SourceChannelID = 0
			route.SourceGroupID = nil
			route.SourceGroupName = ""
		}, change: func(route *GatewayRoute) { route.Weight++ }, preserve: true},
		{name: "provider replaced", prepare: func(route *GatewayRoute) {
			route.SourceKind = GatewayRouteSourceProvider
			route.GatewayProviderID = 9
			route.SourceChannelID = 0
			route.SourceGroupID = nil
			route.SourceGroupName = ""
		}, change: func(route *GatewayRoute) { route.GatewayProviderID++ }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openGatewayBindingDB(t)
			routes := NewGatewayRoutes(db)
			original := gatewayBindingFixture()
			if testCase.prepare != nil {
				testCase.prepare(&original)
			}
			if err := db.Create(&original).Error; err != nil {
				t.Fatalf("create route: %v", err)
			}
			updated := original
			testCase.change(&updated)
			updated.CreatedAt = time.Time{}
			updated.SourceAPIKeyID = 999
			updated.SourceAPIKeyName = "client-supplied-key"
			updated.SourceAPIKeyCipher = "client-supplied-cipher"
			updated.TempUnschedulableUntil = nil
			updated.TempUnschedulableReason = ""
			updated.TempUnschedulableAt = nil
			updated.TempUnschedulableRequestID = ""
			updated.RecoverSuccessStreak = 0
			if err := routes.SaveForGroup(original.GatewayGroupID, []GatewayRoute{updated}); err != nil {
				t.Fatalf("save route: %v", err)
			}
			actual, err := routes.FindByID(original.ID)
			if err != nil {
				t.Fatalf("read route: %v", err)
			}
			if !actual.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("creation timestamp changed: %v", actual.CreatedAt)
			}
			if actual.Weight != updated.Weight || actual.BillingRateMultiplier != updated.BillingRateMultiplier {
				t.Fatal("configuration changes were not saved")
			}
			if testCase.preserve {
				if actual.SourceAPIKeyID != original.SourceAPIKeyID || actual.SourceAPIKeyName != original.SourceAPIKeyName || actual.SourceAPIKeyCipher != original.SourceAPIKeyCipher {
					t.Fatal("existing key was not preserved")
				}
				if !reflect.DeepEqual(actual.TempUnschedulableUntil, original.TempUnschedulableUntil) || !reflect.DeepEqual(actual.TempUnschedulableAt, original.TempUnschedulableAt) || actual.TempUnschedulableReason != original.TempUnschedulableReason || actual.TempUnschedulableRequestID != original.TempUnschedulableRequestID || actual.RecoverSuccessStreak != original.RecoverSuccessStreak {
					t.Fatal("existing pause was not preserved")
				}
			} else if actual.SourceAPIKeyID != 0 || actual.SourceAPIKeyName != "" || actual.SourceAPIKeyCipher != "" || actual.TempUnschedulableUntil != nil || actual.TempUnschedulableAt != nil || actual.TempUnschedulableReason != "" || actual.TempUnschedulableRequestID != "" || actual.RecoverSuccessStreak != 0 {
				t.Fatal("changed source retained key or pause state")
			}
		})
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedOnlyUpdatesTargetKey(t *testing.T) {
	for _, sourceKind := range []string{GatewayRouteSourceMonitor, "", GatewayRouteSourceProvider} {
		t.Run("source kind "+sourceKind, func(t *testing.T) {
			db := openGatewayBindingDB(t)
			routes := NewGatewayRoutes(db)
			target := gatewayBindingFixture()
			if sourceKind == GatewayRouteSourceProvider {
				target.SourceChannelID = 0
				target.GatewayProviderID = 8
				target.SourceGroupID = nil
				target.SourceGroupName = ""
			}
			if err := db.Create(&target).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&target).UpdateColumn("source_kind", sourceKind).Error; err != nil {
				t.Fatal(err)
			}
			sibling := gatewayBindingFixture()
			sibling.Position = 1
			if err := db.Create(&sibling).Error; err != nil {
				t.Fatal(err)
			}
			before, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeSibling, err := routes.FindByID(sibling.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := routes.BindSourceKeyIfUnchanged(before, 52, "new-key", "new-cipher"); err != nil {
				t.Fatalf("bind key: %v", err)
			}
			after, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !after.UpdatedAt.After(before.UpdatedAt) {
				t.Fatal("updated timestamp was not advanced")
			}
			want := *before
			want.SourceAPIKeyID = 52
			want.SourceAPIKeyName = "new-key"
			want.SourceAPIKeyCipher = "new-cipher"
			want.UpdatedAt = after.UpdatedAt
			if !reflect.DeepEqual(*after, want) {
				t.Fatal("binding altered a field outside key and timestamp")
			}
			afterSibling, err := routes.FindByID(sibling.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeSibling, afterSibling) {
				t.Fatal("binding altered sibling route")
			}
		})
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedRejectsStaleSnapshot(t *testing.T) {
	cases := []struct {
		name   string
		column string
		value  any
	}{
		{name: "gateway group", column: "gateway_group_id", value: 8},
		{name: "source kind", column: "source_kind", value: GatewayRouteSourceProvider},
		{name: "source channel", column: "source_channel_id", value: 4},
		{name: "provider", column: "gateway_provider_id", value: 9},
		{name: "source group ID", column: "source_group_id", value: 12},
		{name: "source group ID removed", column: "source_group_id", value: nil},
		{name: "source group name", column: "source_group_name", value: "renamed"},
		{name: "key ID", column: "source_api_key_id", value: 50},
		{name: "key name", column: "source_api_key_name", value: "concurrent-key"},
		{name: "key cipher", column: "source_api_key_cipher", value: "concurrent-cipher"},
		{name: "timestamp", column: "updated_at", value: time.Now()},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openGatewayBindingDB(t)
			routes := NewGatewayRoutes(db)
			target := gatewayBindingFixture()
			if err := db.Create(&target).Error; err != nil {
				t.Fatal(err)
			}
			expected, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&target).UpdateColumn(testCase.column, testCase.value).Error; err != nil {
				t.Fatal(err)
			}
			before, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := routes.BindSourceKeyIfUnchanged(expected, 52, "new-key", "new-cipher"); !errors.Is(err, ErrGatewayRouteChanged) {
				t.Fatalf("expected conflict, got %v", err)
			}
			after, err := routes.FindByID(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("conflicting bind mutated route")
			}
		})
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedRejectsMissingRoute(t *testing.T) {
	db := openGatewayBindingDB(t)
	routes := NewGatewayRoutes(db)
	target := gatewayBindingFixture()
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&target).Error; err != nil {
		t.Fatal(err)
	}
	for _, expected := range []*GatewayRoute{nil, {}, &target} {
		if err := routes.BindSourceKeyIfUnchanged(expected, 52, "new-key", "new-cipher"); !errors.Is(err, ErrGatewayRouteChanged) {
			t.Fatalf("expected conflict for missing route, got %v", err)
		}
	}
	if _, err := routes.FindByID(target.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted route was recreated: %v", err)
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedConcurrentBindings(t *testing.T) {
	db := openGatewayBindingDB(t)
	routes := NewGatewayRoutes(db)
	target := gatewayBindingFixture()
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	expected, err := routes.FindByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, keyID := range []int64{52, 53} {
		workers.Add(1)
		go func(keyID int64) {
			defer workers.Done()
			<-start
			results <- routes.BindSourceKeyIfUnchanged(expected, keyID, "new-key", "new-cipher")
		}(keyID)
	}
	close(start)
	workers.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrGatewayRouteChanged):
			conflicts++
		default:
			t.Fatalf("unexpected bind result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got %d and %d", successes, conflicts)
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedAcceptsLegacyNulls(t *testing.T) {
	db := openGatewayBindingDB(t)
	routes := NewGatewayRoutes(db)
	target := gatewayBindingFixture()
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&target).UpdateColumns(map[string]any{"source_api_key_cipher": nil, "updated_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	expected, err := routes.FindByID(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := routes.BindSourceKeyIfUnchanged(expected, 52, "new-key", "new-cipher"); err != nil {
		t.Fatalf("legacy null binding failed: %v", err)
	}
	if err := routes.BindSourceKeyIfUnchanged(expected, 53, "other-key", "other-cipher"); !errors.Is(err, ErrGatewayRouteChanged) {
		t.Fatalf("stale null snapshot should conflict: %v", err)
	}
}

func TestGatewayRoutesBindSourceKeyIfUnchangedDoesNotLogCiphers(t *testing.T) {
	db := openGatewayBindingDB(t)
	target := gatewayBindingFixture()
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	var captured bytes.Buffer
	loggedDB := db.Session(&gorm.Session{Logger: logger.New(log.New(&captured, "", 0), logger.Config{LogLevel: logger.Info})})
	if err := loggedDB.Exec("SELECT 1").Error; err != nil || captured.Len() == 0 {
		t.Fatal("test logger is not active")
	}
	captured.Reset()
	routes := NewGatewayRoutes(loggedDB)
	if err := routes.BindSourceKeyIfUnchanged(&target, 52, "new-key", "new-cipher"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&GatewayRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := routes.BindSourceKeyIfUnchanged(&target, 53, "other-key", "other-cipher"); err == nil {
		t.Fatal("missing table must return an error")
	}
	if captured.Len() != 0 {
		t.Fatal("binding logged a statement containing key material")
	}
}
