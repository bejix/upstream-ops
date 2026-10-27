package monitor

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/channel"
	_ "github.com/bejix/upstream-ops/backend/connector/newapi"
	_ "github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/notify"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := storage.Open(storage.DBConfig{
		Driver: storage.DBDriverSQLite,
		Path:   filepath.Join(t.TempDir(), "monitor-test.db"),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestRefreshRatesSyncAnnouncementsAndNotify(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		var body struct {
			Event string `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode webhook: %v", err)
		}
		if body.Event != string(storage.EventAnnouncement) {
			t.Fatalf("webhook event = %q, want announcement", body.Event)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:         "webhook",
		Type:         storage.NotifyWebhook,
		ConfigCipher: mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	var statusCalls atomic.Int32
	// 公告版本由测试显式推进：GetRates 也会请求 /api/status（取分组倍率展示口径），
	// 用调用次数判断"第几次刷新"会失真。
	var announcementVersion atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			statusCalls.Add(1)
			id := 1
			title := "公告一"
			content := "首次公告"
			if announcementVersion.Load() >= 2 {
				id = 2
				title = "公告二"
				content = "新增公告"
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota_per_unit":500000,"announcements":[{"id":` +
				jsonNumber(id) + `,"title":"` + title + `","content":"` + content + `","type":"warning","publishDate":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}]}}`))
		case "/api/user/self":
			if got := r.Header.Get("Cookie"); got != "session=1" {
				t.Fatalf("cookie = %q", got)
			}
			if got := r.Header.Get("New-Api-User"); got != "7" {
				t.Fatalf("New-Api-User = %q", got)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":1000000,"used_quota":500000}}`))
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"default":{"ratio":1,"desc":"default"}}}`))
		case "/api/notice":
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	ch := &storage.Channel{
		Name:           "demo",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        apiSrv.URL,
		Username:       "u",
		PasswordCipher: mustEncrypt(t, cipher, `{"cookie":"session=1","user_id":"7"}`),
		CredentialMode: storage.CredentialModeToken,
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{
		SendMaxAttempts: 1,
	})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if got := webhookHits.Load(); got != 0 {
		t.Fatalf("first refresh webhook hits = %d, want 0", got)
	}

	announcementVersion.Store(2)
	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got := webhookHits.Load(); got != 1 {
		t.Fatalf("second refresh webhook hits = %d, want 1", got)
	}

	logs, err := notifies.ListLogs(10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Event != storage.EventAnnouncement || !logs[0].Success {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRefreshRatesSkipsAnnouncementsWhenIgnored(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:         "webhook",
		Type:         storage.NotifyWebhook,
		ConfigCipher: mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	var statusCalls atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			statusCalls.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota_per_unit":500000,"announcements":[{"id":1,"title":"公告一","content":"首次公告","type":"warning","publishDate":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}]}}`))
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"quota":1000000,"used_quota":500000}}`))
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"default":{"ratio":1,"desc":"default"}}}`))
		case "/api/notice":
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	ch := &storage.Channel{
		Name:                "demo",
		Type:                storage.ChannelTypeNewAPI,
		SiteURL:             apiSrv.URL,
		Username:            "u",
		PasswordCipher:      mustEncrypt(t, cipher, `{"cookie":"session=1","user_id":"7"}`),
		CredentialMode:      storage.CredentialModeToken,
		MonitorEnabled:      true,
		IgnoreAnnouncements: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{SendMaxAttempts: 1})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	list, total, err := announcements.ListPage(1, 10)
	if err != nil {
		t.Fatalf("list announcements: %v", err)
	}
	if total != 0 || len(list) != 0 {
		t.Fatalf("announcements should be skipped: total=%d list=%#v", total, list)
	}
	// GetRates 也会请求一次 /api/status（取分组倍率展示口径），所以这里是 1；
	// 若公告同步没被跳过，会多出一次公告用的 status 请求，变成 2。
	if got := statusCalls.Load(); got != 1 {
		t.Fatalf("status calls = %d, want 1", got)
	}
	logs, err := notifies.ListLogs(10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("notification logs = %#v", logs)
	}
}

func TestRefreshRatesEmitsRateAddedAndRemoved(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:         "webhook",
		Type:         storage.NotifyWebhook,
		ConfigCipher: mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	var call atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":50}}`))
		case "/api/v1/usage/dashboard/stats":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"today_actual_cost":1,"total_actual_cost":2}}`))
		case "/api/v1/groups/available":
			n := call.Add(1)
			if n == 1 {
				_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
					{"id":1,"name":"alpha","description":"a","rate_multiplier":1.0},
					{"id":2,"name":"beta","description":"b","rate_multiplier":2.0}
				]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"id":2,"name":"beta","description":"b","rate_multiplier":2.0},
				{"id":3,"name":"gamma","description":"c","rate_multiplier":3.0}
			]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/announcements":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	ch := &storage.Channel{
		Name:                "sub",
		Type:                storage.ChannelTypeSub2API,
		SiteURL:             apiSrv.URL,
		Username:            "u",
		PasswordCipher:      mustEncrypt(t, cipher, `{"access_token":"token"}`),
		CredentialMode:      storage.CredentialModeToken,
		MonitorEnabled:      true,
		SubscriptionEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{
		SendMaxAttempts: 1,
	})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	logs, err := notifies.ListLogs(10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("first refresh logs = %#v", logs)
	}
	snapshots, err := rates.ListByChannel(ch.ID)
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	remoteGroupIDs := map[string]int64{}
	for _, snapshot := range snapshots {
		if snapshot.RemoteGroupID != nil {
			remoteGroupIDs[snapshot.ModelName] = *snapshot.RemoteGroupID
		}
	}
	if remoteGroupIDs["alpha"] != 1 || remoteGroupIDs["beta"] != 2 {
		t.Fatalf("remote group ids = %#v", remoteGroupIDs)
	}

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	logs, err = notifies.ListLogs(20)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	var rateBatch int
	for _, log := range logs {
		if log.Event == storage.EventRateChanged {
			rateBatch++
			if !strings.Contains(log.Subject, "渠道分组变动") ||
				!strings.Contains(log.Subject, "1 个渠道 · 2 项") ||
				!strings.Contains(log.Body, "# 渠道分组变动") ||
				!strings.Contains(log.Body, "gamma") ||
				!strings.Contains(log.Body, "alpha") {
				t.Fatalf("unexpected rate batch log = %#v", log)
			}
		}
	}
	if rateBatch != 1 {
		t.Fatalf("rateBatch=%d logs=%#v", rateBatch, logs)
	}
	if got := webhookHits.Load(); got != 1 {
		t.Fatalf("webhook hits = %d, want 1", got)
	}

	list, err := rates.ListByChannel(ch.ID)
	if err != nil {
		t.Fatalf("list rates: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("rate snapshots len = %d, want 2", len(list))
	}
	got := map[string]bool{}
	for _, item := range list {
		got[item.ModelName] = true
	}
	if !got["beta"] || !got["gamma"] || got["alpha"] {
		t.Fatalf("rate snapshots = %#v", got)
	}
}

func TestRefreshRatesAfterChannelReuseDoesNotEmitOldStructureChange(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:         "webhook",
		Type:         storage.NotifyWebhook,
		ConfigCipher: mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":50}}`))
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"id":1,"name":"new","description":"n","rate_multiplier":1.0}
			]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/announcements":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	old := &storage.Channel{
		ID:             1,
		Name:           "old",
		Type:           storage.ChannelTypeSub2API,
		SiteURL:        apiSrv.URL,
		Username:       "u",
		PasswordCipher: mustEncrypt(t, cipher, `{"access_token":"token"}`),
		CredentialMode: storage.CredentialModeToken,
		MonitorEnabled: true,
	}
	if err := channels.Create(old); err != nil {
		t.Fatalf("create old channel: %v", err)
	}
	if _, err := rates.Upsert(&storage.RateSnapshot{
		ChannelID:  old.ID,
		ModelName:  "old",
		Ratio:      1,
		LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert old snapshot: %v", err)
	}
	if err := channels.Delete(old.ID); err != nil {
		t.Fatalf("delete old channel: %v", err)
	}

	ch := &storage.Channel{
		ID:             old.ID,
		Name:           "new",
		Type:           storage.ChannelTypeSub2API,
		SiteURL:        apiSrv.URL,
		Username:       "u",
		PasswordCipher: mustEncrypt(t, cipher, `{"access_token":"token"}`),
		CredentialMode: storage.CredentialModeToken,
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create new channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{
		SendMaxAttempts: 1,
	})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	logs, err := notifies.ListLogs(10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	for _, log := range logs {
		if log.Event == storage.EventRateStructureChanged {
			t.Fatalf("unexpected structure change log: %#v", log)
		}
	}
	if got := webhookHits.Load(); got != 0 {
		t.Fatalf("webhook hits = %d, want 0", got)
	}
}

func TestRateEventSubscriptionFiltersGroups(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:          "webhook",
		Type:          storage.NotifyWebhook,
		ConfigCipher:  mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
		Subscriptions: `[{"channel_id":1,"mode":"groups","groups":["beta"]}]`,
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	var call atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":50}}`))
		case "/api/v1/usage/dashboard/stats":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"today_actual_cost":1,"total_actual_cost":2}}`))
		case "/api/v1/groups/available":
			n := call.Add(1)
			if n == 1 {
				_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
					{"id":1,"name":"alpha","description":"a","rate_multiplier":1.0}
				]}`))
				return
			}
			if n == 2 {
				_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
					{"id":1,"name":"alpha","description":"a","rate_multiplier":1.0},
					{"id":2,"name":"beta","description":"b","rate_multiplier":2.0},
					{"id":3,"name":"gamma","description":"c","rate_multiplier":3.0}
				]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"id":1,"name":"alpha","description":"a","rate_multiplier":1.0},
				{"id":3,"name":"gamma","description":"c","rate_multiplier":3.0}
			]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/announcements":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	ch := &storage.Channel{
		Name:                "sub",
		Type:                storage.ChannelTypeSub2API,
		SiteURL:             apiSrv.URL,
		Username:            "u",
		PasswordCipher:      mustEncrypt(t, cipher, `{"access_token":"token"}`),
		CredentialMode:      storage.CredentialModeToken,
		MonitorEnabled:      true,
		SubscriptionEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{
		SendMaxAttempts: 1,
	})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if err := svc.RefreshRates(context.Background(), ch); err != nil {
		t.Fatalf("third refresh: %v", err)
	}
	logs, err := notifies.ListLogs(20)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	var foundAddedBeta, foundRemovedBeta bool
	for _, log := range logs {
		if log.Event != storage.EventRateChanged {
			continue
		}
		if strings.Contains(log.Body, "## 新增分组（1）") &&
			strings.Contains(log.Body, "beta") {
			foundAddedBeta = true
		}
		if strings.Contains(log.Body, "## 移除分组（1）") &&
			strings.Contains(log.Body, "beta") {
			foundRemovedBeta = true
		}
	}
	if !foundAddedBeta || !foundRemovedBeta {
		t.Fatalf("logs = %#v", logs)
	}
	if got := webhookHits.Load(); got != 2 {
		t.Fatalf("webhook hits = %d, want 2", got)
	}
}

func TestSubscriptionUsageAlertsAndCooldown(t *testing.T) {
	db := openTestDB(t)
	channels := storage.NewChannels(db)
	authSessions := storage.NewAuthSessions(db)
	captchas := storage.NewCaptchas(db)
	announcements := storage.NewUpstreamAnnouncements(db)
	rates := storage.NewRates(db)
	monitorLogs := storage.NewMonitorLogs(db)
	notifies := storage.NewNotifications(db)
	cipher, err := crypto.NewCipher("secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	var webhookHits atomic.Int32
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHits.Add(1)
		var body struct {
			Event string `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode webhook: %v", err)
		}
		if body.Event != string(storage.EventSubscriptionDailyLow) &&
			body.Event != string(storage.EventSubscriptionMonthlyLow) &&
			body.Event != string(storage.EventSubscriptionExpiring) {
			t.Fatalf("unexpected event = %q", body.Event)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhookSrv.Close()

	if err := notifies.CreateChannel(&storage.NotificationChannel{
		Name:         "webhook",
		Type:         storage.NotifyWebhook,
		ConfigCipher: mustEncrypt(t, cipher, `{"url":"`+webhookSrv.URL+`"}`),
	}); err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	expiresAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":50}}`))
		case "/api/v1/usage/dashboard/stats":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"today_actual_cost":1,"total_actual_cost":2}}`))
		case "/api/v1/subscriptions/progress":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"subscription":{"id":9,"group_id":3,"status":"active","starts_at":"2026-01-01T00:00:00Z","expires_at":"` + expiresAt + `","group":{"id":3,"name":"pro"}},"progress":{"id":9,"group_name":"pro","expires_at":"` + expiresAt + `","expires_in_days":0,"daily":{"limit_usd":10,"used_usd":9,"remaining_usd":1,"percentage":90,"window_start":"2026-01-01T00:00:00Z","resets_at":"2026-01-02T00:00:00Z","resets_in_seconds":3600},"weekly":{"limit_usd":0,"used_usd":0,"remaining_usd":0,"percentage":0},"monthly":{"limit_usd":100,"used_usd":96,"remaining_usd":4,"percentage":96,"window_start":"2026-01-01T00:00:00Z","resets_at":"2026-02-01T00:00:00Z","resets_in_seconds":3600}}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	ch := &storage.Channel{
		Name:                "sub",
		Type:                storage.ChannelTypeSub2API,
		SiteURL:             apiSrv.URL,
		Username:            "u",
		PasswordCipher:      mustEncrypt(t, cipher, `{"access_token":"token"}`),
		CredentialMode:      storage.CredentialModeToken,
		MonitorEnabled:      true,
		SubscriptionEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	channelSvc := channel.NewService(channels, authSessions, captchas, rates, monitorLogs, cipher)
	dispatcher := notify.NewDispatcher(notifies, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)), notify.Policy{
		SubscriptionDailyRemainingThresholdPct:   15,
		SubscriptionWeeklyRemainingThresholdPct:  15,
		SubscriptionMonthlyRemainingThresholdPct: 5,
		SubscriptionExpiryThreshold:              3 * time.Hour,
		SubscriptionAlertCooldown:                time.Hour,
		SendMaxAttempts:                          1,
	})
	svc := NewService(channels, announcements, rates, monitorLogs, channelSvc, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	svc.ScanAllBalances(context.Background())
	if got := webhookHits.Load(); got != 3 {
		t.Fatalf("webhook hits = %d, want 3", got)
	}
	svc.ScanAllBalances(context.Background())
	if got := webhookHits.Load(); got != 3 {
		t.Fatalf("webhook hits after cooldown = %d, want 3", got)
	}

	logs, err := notifies.ListLogs(10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	seen := map[storage.NotificationEvent]bool{}
	for _, log := range logs {
		seen[log.Event] = true
	}
	if !seen[storage.EventSubscriptionDailyLow] || seen[storage.EventSubscriptionWeeklyLow] || !seen[storage.EventSubscriptionMonthlyLow] || !seen[storage.EventSubscriptionExpiring] {
		t.Fatalf("events = %#v", seen)
	}
}

func mustEncrypt(t *testing.T, cipher *crypto.Cipher, plain string) string {
	t.Helper()
	out, err := cipher.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return out
}

func jsonNumber(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}
