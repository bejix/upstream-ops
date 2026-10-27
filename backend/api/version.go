package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/global"
	"github.com/gin-gonic/gin"
)

const (
	githubRepoURL              = "https://github.com/bejix/upstream-ops"
	defaultGitHubLatestRelease = "https://api.github.com/repos/bejix/upstream-ops/releases/latest"
)

var (
	githubLatestReleaseURL = defaultGitHubLatestRelease
	githubReleaseClient    = &http.Client{Timeout: 2 * time.Second}
)

type versionResponse struct {
	Name            string `json:"name"`
	Title           string `json:"title"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	RepoURL         string `json:"repo_url"`
	ReleaseURL      string `json:"release_url"`
	UpdateError     string `json:"update_error"`
}

type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func registerVersion(api *gin.RouterGroup, d *Deps) {
	api.GET("/version", func(c *gin.Context) {
		force := c.Query("force") == "1" || strings.EqualFold(c.Query("force"), "true")
		c.JSON(http.StatusOK, buildVersionResponse(c.Request.Context(), d, force))
	})
}

func buildVersionResponse(ctx context.Context, d *Deps, force bool) versionResponse {
	app := config.AppConfig{Title: "UpstreamOps"}
	proxyCfg := config.ProxyConfig{}
	if d != nil && d.Runtime != nil {
		if cfg, err := config.LoadFile(d.Runtime.ConfigPath()); err == nil {
			app = cfg.App
		}
		proxyCfg = d.Runtime.CurrentProxy()
	}

	resp := versionResponse{
		Name:    "upstream-ops",
		Title:   app.Title,
		Version: global.VERSION,
		RepoURL: githubRepoURL,
	}

	latest, releaseURL, err := fetchLatestGitHubRelease(ctx, versionCheckClient(proxyCfg, force))
	if err != nil {
		resp.UpdateError = err.Error()
		return resp
	}
	resp.LatestVersion = latest
	resp.ReleaseURL = releaseURL
	resp.UpdateAvailable = isVersionNewer(latest, global.VERSION)
	return resp
}

func versionCheckClient(proxyCfg config.ProxyConfig, force bool) *http.Client {
	if !proxyCfg.VersionCheckEnabled && !force {
		return githubReleaseClient
	}
	proxyURL, err := proxyCfg.ActiveURL()
	if err != nil || proxyURL == "" {
		return githubReleaseClient
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return githubReleaseClient
	}
	return &http.Client{
		Timeout: githubReleaseClient.Timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
		},
	}
}

func fetchLatestGitHubRelease(ctx context.Context, client *http.Client) (string, string, error) {
	if client == nil {
		client = githubReleaseClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "upstream-ops")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("github latest release status %d", resp.StatusCode)
	}

	var release githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return "", "", errors.New("github latest release missing tag_name")
	}
	if strings.TrimSpace(release.HTMLURL) == "" {
		release.HTMLURL = githubRepoURL
	}
	return release.TagName, release.HTMLURL, nil
}

func isVersionNewer(latest, current string) bool {
	lv, ok := parseVersion(latest)
	if !ok {
		return false
	}
	cv, ok := parseVersion(current)
	if !ok {
		return false
	}
	// 逐段比较；两段长度不同时，短的一侧补 0（1.2 == 1.2.0，1.2.0.1 > 1.2.0）。
	n := len(lv)
	if len(cv) > n {
		n = len(cv)
	}
	for i := 0; i < n; i++ {
		var l, c int
		if i < len(lv) {
			l = lv[i]
		}
		if i < len(cv) {
			c = cv[i]
		}
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}
	return false
}

// parseVersion 解析点分版本号，返回各段数字。
//
// 支持三段与四段：本项目历史上发过 v0.0.10.1 这类四段 tag，
// 早期实现写死 len(parts) != 3 会让这些版本的「检查更新」静默失效
// （isVersionNewer 直接返回 false）。这里改为接受任意 1~4 段。
func parseVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
