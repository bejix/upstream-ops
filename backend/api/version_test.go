package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bejix/upstream-ops/backend/global"
	"github.com/gin-gonic/gin"
)

func TestIsVersionNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "0.2.1", current: "0.2.0", want: true},
		{latest: "v0.2.1", current: "0.2.0", want: true},
		{latest: "0.2.0", current: "v0.2.0", want: false},
		{latest: "0.1.9", current: "0.2.0", want: false},
		// 四段式：本项目发过 v0.0.10.1，旧实现写死三段会让这类版本永远检测不到更新。
		{latest: "0.0.10.2", current: "0.0.10.1", want: true},
		{latest: "0.0.10.1", current: "0.0.10.2", want: false},
		{latest: "0.0.11", current: "0.0.10.2", want: true},
		{latest: "0.0.10.2", current: "0.0.10", want: true},
		// 非法形态不应误报更新。
		{latest: "not-a-version", current: "0.0.10", want: false},
		{latest: "", current: "0.0.10", want: false},
		{latest: "1.2.3.4.5", current: "0.0.10", want: false},
	}
	for _, tt := range tests {
		if got := isVersionNewer(tt.latest, tt.current); got != tt.want {
			t.Fatalf("isVersionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestVersionEndpointReportsUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	withGitHubReleaseServer(t, http.StatusOK, `{"tag_name":"v999.0.0","html_url":"https://github.com/bejix/upstream-ops/releases/tag/v999.0.0"}`)
	resp := requestVersion(t)

	if !resp.UpdateAvailable {
		t.Fatalf("update_available = false, want true")
	}
	if resp.LatestVersion != "v999.0.0" {
		t.Fatalf("latest_version = %q, want v999.0.0", resp.LatestVersion)
	}
	if resp.ReleaseURL == "" {
		t.Fatalf("release_url is empty")
	}
}

func TestVersionEndpointReportsNoUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	withGitHubReleaseServer(t, http.StatusOK, `{"tag_name":"`+global.VERSION+`","html_url":"https://github.com/bejix/upstream-ops/releases/tag/v`+global.VERSION+`"}`)
	resp := requestVersion(t)

	if resp.UpdateAvailable {
		t.Fatalf("update_available = true, want false")
	}
	if resp.LatestVersion != global.VERSION {
		t.Fatalf("latest_version = %q, want %s", resp.LatestVersion, global.VERSION)
	}
}

func TestVersionEndpointKeepsResponseOnGitHubError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	withGitHubReleaseServer(t, http.StatusInternalServerError, `{"message":"error"}`)
	resp := requestVersion(t)

	if resp.UpdateAvailable {
		t.Fatalf("update_available = true, want false")
	}
	if strings.TrimSpace(resp.UpdateError) == "" {
		t.Fatalf("update_error is empty")
	}
	if resp.Version != global.VERSION {
		t.Fatalf("version = %q, want %s", resp.Version, global.VERSION)
	}
}

func withGitHubReleaseServer(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	oldURL := githubLatestReleaseURL
	oldClient := githubReleaseClient
	githubLatestReleaseURL = srv.URL
	githubReleaseClient = srv.Client()
	t.Cleanup(func() {
		githubLatestReleaseURL = oldURL
		githubReleaseClient = oldClient
	})
}

func requestVersion(t *testing.T) versionResponse {
	t.Helper()
	r := gin.New()
	registerVersion(r.Group("/api"), &Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp versionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}
