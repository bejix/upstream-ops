package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/connector"
	_ "github.com/bejix/upstream-ops/backend/connector/newapi"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
)

type fakeHTTPConfigConnector struct {
	cfg connector.HTTPConfig
}

func (f *fakeHTTPConfigConnector) SetHTTPConfig(cfg connector.HTTPConfig) {
	f.cfg = cfg
}

func testService(t *testing.T) (*Service, *crypto.Cipher) {
	t.Helper()
	db, err := storage.Open(storage.DBConfig{
		Driver: storage.DBDriverSQLite,
		Path:   filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	cipher, err := crypto.NewCipher("12345678901234567890123456789012")
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	svc := NewService(
		storage.NewChannels(db),
		storage.NewAuthSessions(db),
		storage.NewCaptchas(db),
		storage.NewRates(db),
		storage.NewMonitorLogs(db),
		cipher,
	)
	return svc, cipher
}

func TestResolveAppliesProxyOnlyWhenEnabled(t *testing.T) {
	svc, cipher := testService(t)
	svc.UpdateProxyConfig(config.ProxyConfig{
		Enabled:  true,
		Protocol: "socks5",
		Host:     "127.0.0.1",
		Port:     1080,
		Username: "u",
		Password: "p",
	})

	enc, err := cipher.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	withProxy := &storage.Channel{
		ID:             1,
		Name:           "with-proxy",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: enc,
		ProxyEnabled:   true,
	}
	resolved, err := svc.Resolve(context.Background(), withProxy)
	if err != nil {
		t.Fatalf("resolve with proxy: %v", err)
	}
	if resolved.ProxyURL != "socks5://u:p@127.0.0.1:1080" {
		t.Fatalf("proxy url = %q", resolved.ProxyURL)
	}

	withoutProxy := *withProxy
	withoutProxy.ProxyEnabled = false
	resolved, err = svc.Resolve(context.Background(), &withoutProxy)
	if err != nil {
		t.Fatalf("resolve without proxy: %v", err)
	}
	if resolved.ProxyURL != "" {
		t.Fatalf("proxy url = %q, want empty", resolved.ProxyURL)
	}
}

func TestResolveSkipsProxyWhenGlobalProxyDisabled(t *testing.T) {
	svc, cipher := testService(t)
	svc.UpdateProxyConfig(config.ProxyConfig{
		Protocol: "socks5",
		Host:     "127.0.0.1",
		Port:     1080,
	})
	enc, err := cipher.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	resolved, err := svc.Resolve(context.Background(), &storage.Channel{
		ID:             1,
		Name:           "with-proxy",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: enc,
		ProxyEnabled:   true,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ProxyURL != "" {
		t.Fatalf("proxy url = %q, want empty", resolved.ProxyURL)
	}
}

func TestApplyHTTPConfigUsesUpstreamConfig(t *testing.T) {
	svc, _ := testService(t)
	svc.UpdateUpstreamConfig(config.UpstreamConfig{
		TimeoutSeconds: 45,
		UserAgent:      "custom-agent",
	})

	conn := &fakeHTTPConfigConnector{}
	svc.ApplyHTTPConfig(conn)

	if conn.cfg.Timeout != 45*time.Second {
		t.Fatalf("timeout = %s", conn.cfg.Timeout)
	}
	if conn.cfg.UserAgent != "custom-agent" {
		t.Fatalf("user agent = %q", conn.cfg.UserAgent)
	}
}

func TestApplyHTTPConfigUsesDefaults(t *testing.T) {
	svc, _ := testService(t)
	conn := &fakeHTTPConfigConnector{}
	svc.ApplyHTTPConfig(conn)

	if conn.cfg.Timeout != time.Duration(config.DefaultUpstreamTimeoutSeconds)*time.Second {
		t.Fatalf("timeout = %s", conn.cfg.Timeout)
	}
	if conn.cfg.UserAgent != config.DefaultUpstreamUserAgent {
		t.Fatalf("user agent = %q", conn.cfg.UserAgent)
	}
}

func TestBuildSessionFromSub2APITokenCredentialIncludesRefreshToken(t *testing.T) {
	svc, cipher := testService(t)
	enc, err := cipher.Encrypt(`{"access_token":"access","refresh_token":"refresh"}`)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	session, err := svc.buildSessionFromToken(&storage.Channel{
		Type:           storage.ChannelTypeSub2API,
		PasswordCipher: enc,
	})
	if err != nil {
		t.Fatalf("buildSessionFromToken: %v", err)
	}
	if session.AccessToken != "access" {
		t.Fatalf("access token = %q, want access", session.AccessToken)
	}
	if session.RefreshToken != "refresh" {
		t.Fatalf("refresh token = %q, want refresh", session.RefreshToken)
	}
}

func TestPersistSessionStoresRefreshToken(t *testing.T) {
	svc, _ := testService(t)
	session := &connector.AuthSession{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := svc.persistSession(7, session); err != nil {
		t.Fatalf("persistSession: %v", err)
	}

	saved, err := svc.AuthSessions.FindByChannel(7)
	if err != nil {
		t.Fatalf("FindByChannel: %v", err)
	}
	if saved == nil {
		t.Fatal("saved session is nil")
	}
	if saved.RefreshTokenCipher == "" {
		t.Fatal("refresh token cipher is empty")
	}

	decrypted, err := svc.decryptSession(saved)
	if err != nil {
		t.Fatalf("decryptSession: %v", err)
	}
	if decrypted.RefreshToken != "refresh" {
		t.Fatalf("refresh token = %q, want refresh", decrypted.RefreshToken)
	}
}

func TestPersistTokenCredentialStoresSub2APIRefreshToken(t *testing.T) {
	svc, cipher := testService(t)
	oldCred, err := cipher.Encrypt(`{"access_token":"old","refresh_token":"old-refresh"}`)
	if err != nil {
		t.Fatalf("encrypt old credential: %v", err)
	}
	ch := &storage.Channel{
		Name:           "sub",
		Type:           storage.ChannelTypeSub2API,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: oldCred,
		CredentialMode: storage.CredentialModeToken,
		MonitorEnabled: true,
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	err = svc.persistTokenCredential(ch, &connector.AuthSession{
		AccessToken:  "new",
		RefreshToken: "new-refresh",
	})
	if err != nil {
		t.Fatalf("persistTokenCredential: %v", err)
	}

	updated, err := svc.Channels.FindByID(ch.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	raw, err := cipher.Decrypt(updated.PasswordCipher)
	if err != nil {
		t.Fatalf("decrypt credential: %v", err)
	}
	var cred Sub2APITokenCredential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		t.Fatalf("unmarshal credential: %v", err)
	}
	if cred.AccessToken != "new" {
		t.Fatalf("access token = %q, want new", cred.AccessToken)
	}
	if cred.RefreshToken != "new-refresh" {
		t.Fatalf("refresh token = %q, want new-refresh", cred.RefreshToken)
	}
}

func TestClearLoginInfoClearsTokenCredentialAndSession(t *testing.T) {
	svc, cipher := testService(t)
	enc, err := cipher.Encrypt(`{"cookie":"session=abc","user_id":"1"}`)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	ch := &storage.Channel{
		Name:           "newapi-token",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "memo",
		PasswordCipher: enc,
		CredentialMode: storage.CredentialModeToken,
		MonitorEnabled: true,
		LastError:      "old error",
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := svc.persistSession(ch.ID, &connector.AuthSession{
		AccessToken:  "access",
		RefreshToken: "refresh",
		Cookie:       "session=abc",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("persistSession: %v", err)
	}

	updated, err := svc.ClearLoginInfo(ch.ID)
	if err != nil {
		t.Fatalf("ClearLoginInfo: %v", err)
	}
	if updated.PasswordCipher != "" {
		t.Fatalf("password cipher = %q, want empty", updated.PasswordCipher)
	}
	if updated.LastError != "" {
		t.Fatalf("last error = %q, want empty", updated.LastError)
	}
	saved, err := svc.AuthSessions.FindByChannel(ch.ID)
	if err != nil {
		t.Fatalf("FindByChannel: %v", err)
	}
	if saved != nil {
		t.Fatalf("auth session = %#v, want nil", saved)
	}
	if _, err := svc.buildSessionFromToken(updated); err == nil {
		t.Fatal("buildSessionFromToken after clear succeeded, want error")
	}
}

func TestClearLoginInfoKeepsPasswordCredential(t *testing.T) {
	svc, cipher := testService(t)
	enc, err := cipher.Encrypt("password")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	ch := &storage.Channel{
		Name:           "password-channel",
		Type:           storage.ChannelTypeSub2API,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: enc,
		CredentialMode: storage.CredentialModePassword,
		MonitorEnabled: true,
		LastError:      "old error",
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := svc.persistSession(ch.ID, &connector.AuthSession{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("persistSession: %v", err)
	}

	updated, err := svc.ClearLoginInfo(ch.ID)
	if err != nil {
		t.Fatalf("ClearLoginInfo: %v", err)
	}
	if updated.PasswordCipher != enc {
		t.Fatal("password credential was cleared in password mode")
	}
	saved, err := svc.AuthSessions.FindByChannel(ch.ID)
	if err != nil {
		t.Fatalf("FindByChannel: %v", err)
	}
	if saved != nil {
		t.Fatalf("auth session = %#v, want nil", saved)
	}
}

func TestTestLoginFailsWhenSessionAuthFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "bad"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":7}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"success":false,"message":"Unauthorized, not logged in and no access token provided"}`, http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc, cipher := testService(t)
	enc, err := cipher.Encrypt("p")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ch := &storage.Channel{
		Name:           "bad-session",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        srv.URL,
		Username:       "u",
		PasswordCipher: enc,
		CredentialMode: storage.CredentialModePassword,
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	err = svc.TestLogin(context.Background(), ch.ID)
	if err == nil {
		t.Fatal("TestLogin succeeded, want auth failure")
	}
	if !strings.Contains(err.Error(), "登录后鉴权失败") {
		t.Fatalf("error = %q", err.Error())
	}
	saved, err := svc.AuthSessions.FindByChannel(ch.ID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if saved != nil {
		t.Fatalf("saved session = %#v, want nil", saved)
	}
}

func TestEnsureSessionNewAPIRefreshFailureDoesNotLogin(t *testing.T) {
	var loginCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"success":false,"code":"AUTH_ORIGIN_FORBIDDEN","message":"Forbidden"}`, http.StatusForbidden)
	})
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		http.Error(w, "unexpected login", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc, ch, resolved, conn := newStoredNewAPIChannel(t, srv.URL)
	_, err := svc.EnsureSession(context.Background(), ch, resolved, conn)
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("EnsureSession error = %v, want refresh status 403", err)
	}
	if got := loginCalls.Load(); got != 0 {
		t.Fatalf("login calls = %d, want 0", got)
	}
	if saved, findErr := svc.AuthSessions.FindByChannel(ch.ID); findErr != nil || saved == nil {
		t.Fatalf("stored session after refresh failure = %#v, err = %v; want preserved", saved, findErr)
	}
}

func TestEnsureSessionNewAPIUnauthorizedRefreshRelogsIn(t *testing.T) {
	var loginCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"success":false,"code":"AUTH_UNAUTHORIZED","message":"Unauthorized"}`, http.StatusUnauthorized)
	})
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "new-refresh"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"new-access","access_expires_at":1900000000,"user":{"id":9}}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":9}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc, ch, resolved, conn := newStoredNewAPIChannel(t, srv.URL)
	session, err := svc.EnsureSession(context.Background(), ch, resolved, conn)
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if got := loginCalls.Load(); got != 1 {
		t.Fatalf("login calls = %d, want 1", got)
	}
	if session.AccessToken != "new-access" || session.RefreshToken != "new-refresh" {
		t.Fatalf("session = %#v", session)
	}
}

func TestEnsureSessionSerializesRefreshByChannel(t *testing.T) {
	var refreshCalls atomic.Int32
	firstRefreshStarted := make(chan struct{})
	secondRefreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		call := refreshCalls.Add(1)
		if call == 1 {
			firstOnce.Do(func() { close(firstRefreshStarted) })
		} else {
			secondOnce.Do(func() { close(secondRefreshStarted) })
		}
		<-releaseRefresh
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "rotated-refresh"})
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"access_token":"rotated-access","access_expires_at":1900000000,"user":{"id":9}}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":9}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc, ch, resolved, conn := newStoredNewAPIChannel(t, srv.URL)
	type result struct {
		session *connector.AuthSession
		err     error
	}
	results := make(chan result, 2)
	go func() {
		session, err := svc.EnsureSession(context.Background(), ch, resolved, conn)
		results <- result{session: session, err: err}
	}()
	<-firstRefreshStarted
	go func() {
		session, err := svc.EnsureSession(context.Background(), ch, resolved, conn)
		results <- result{session: session, err: err}
	}()

	select {
	case <-secondRefreshStarted:
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRefresh)

	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("EnsureSession: %v", result.err)
		}
		if result.session.AccessToken != "rotated-access" {
			t.Fatalf("access token = %q, want rotated-access", result.session.AccessToken)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func newStoredNewAPIChannel(t *testing.T, siteURL string) (*Service, *storage.Channel, *connector.Channel, connector.Connector) {
	t.Helper()
	svc, cipher := testService(t)
	passwordCipher, err := cipher.Encrypt("password")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	ch := &storage.Channel{
		Name:           "newapi-session-refresh",
		Type:           storage.ChannelTypeNewAPI,
		SiteURL:        siteURL,
		Username:       "user",
		PasswordCipher: passwordCipher,
		CredentialMode: storage.CredentialModePassword,
	}
	if err := svc.Channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := svc.persistSession(ch.ID, &connector.AuthSession{
		UserID:       "9",
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	resolved, err := svc.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("resolve channel: %v", err)
	}
	conn, err := connector.For(connector.TypeNewAPI)
	if err != nil {
		t.Fatalf("new NewAPI connector: %v", err)
	}
	return svc, ch, resolved, conn
}
