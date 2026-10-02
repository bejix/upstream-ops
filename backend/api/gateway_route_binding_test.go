package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/auth"
	"github.com/bejix/upstream-ops/backend/config"
	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/gateway"
	"github.com/bejix/upstream-ops/backend/runtimeconfig"
	"github.com/bejix/upstream-ops/backend/storage"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type bindingChannelAPI struct {
	keys         []connector.APIKey
	groups       []connector.APIKeyGroup
	listError    error
	revealError  error
	secret       string
	readCalls    int
	createCalls  int
	updateCalls  int
	revealedIDs  []int64
	beforeReveal func()
}

func (upstream *bindingChannelAPI) ListAPIKeys(_ context.Context, _ uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	upstream.readCalls++
	return &connector.APIKeyPage{Items: upstream.keys, Total: int64(len(upstream.keys)), Page: query.Page, PageSize: query.PageSize, Pages: 1}, upstream.listError
}

func (upstream *bindingChannelAPI) ListAPIKeyGroups(context.Context, uint) ([]connector.APIKeyGroup, error) {
	upstream.readCalls++
	return upstream.groups, nil
}

func (upstream *bindingChannelAPI) CreateAPIKey(context.Context, uint, connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	upstream.createCalls++
	return nil, errors.New("unexpected key creation")
}

func (upstream *bindingChannelAPI) UpdateAPIKey(context.Context, uint, int64, connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	upstream.updateCalls++
	return nil, errors.New("unexpected key update")
}

func (upstream *bindingChannelAPI) RevealAPIKey(_ context.Context, _ uint, keyID int64) (string, error) {
	upstream.revealedIDs = append(upstream.revealedIDs, keyID)
	if upstream.beforeReveal != nil {
		upstream.beforeReveal()
	}
	return upstream.secret, upstream.revealError
}

type routeBindingFixture struct {
	db       *gorm.DB
	router   *gin.Engine
	service  *gateway.Service
	upstream *bindingChannelAPI
	group    storage.GatewayGroup
	target   storage.GatewayRoute
	token    string
}

func newRouteBindingFixture(t *testing.T) *routeBindingFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	cipher, err := crypto.NewCipher("test-route-key-binding")
	if err != nil {
		t.Fatal(err)
	}
	oldCipher, err := cipher.Encrypt("sk-previous-route-secret")
	if err != nil {
		t.Fatal(err)
	}
	sourceGroupID := int64(7)
	otherGroupID := int64(8)
	upstream := &bindingChannelAPI{
		keys: []connector.APIKey{
			{ID: 42, Name: "same-name", GroupID: &otherGroupID, GroupName: "other", Status: "active", UnlimitedQuota: true},
			{ID: 41, Name: "same-name", GroupID: &sourceGroupID, GroupName: "source", Status: "active", UnlimitedQuota: true},
		},
		groups: []connector.APIKeyGroup{{ID: &sourceGroupID, Name: "source", Ratio: 0.25}, {ID: &otherGroupID, Name: "other", Ratio: 0.5}},
		secret: "sk-selected-route-secret",
	}
	channel := storage.Channel{Name: "supplier", Type: storage.ChannelTypeSub2API, SiteURL: "https://supplier.example", Username: "test", PasswordCipher: "encrypted", MonitorEnabled: true}
	group := storage.GatewayGroup{Name: "test-models", ModelsJSON: `[{"id":"model-a"}]`}
	for _, item := range []any{&channel, &group} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	target := storage.GatewayRoute{GatewayGroupID: group.ID, SourceChannelID: channel.ID, SourceGroupID: &sourceGroupID, SourceGroupName: "source", SourceAPIKeyID: 31, SourceAPIKeyName: "old", SourceAPIKeyCipher: oldCipher, Enabled: true, Position: 0, Weight: 3, BillingRateMultiplier: 0.0385, ModelMappingJSON: `{"public":"private"}`, UserAgentMode: "custom", UserAgentCustom: "fixture"}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	for position := 1; position <= 2; position++ {
		sibling := target
		sibling.ID = 0
		sibling.Position = position
		sibling.SourceAPIKeyID = int64(31 + position)
		if err := db.Create(&sibling).Error; err != nil {
			t.Fatal(err)
		}
		if position == 2 {
			if err := db.Model(&sibling).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	service := gateway.NewService(storage.NewGatewayGroups(db), storage.NewGatewayKeys(db), storage.NewGatewayRoutes(db), storage.NewGatewayUsageLogs(db), storage.NewModelPriceOverrides(db), storage.NewChannels(db), upstream, cipher, nil)
	authService, err := auth.New("test-admin", "test-password", "test-auth-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authService.Login("test-admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeconfig.New("", "", nil, nil, nil, service, authService, nil, config.ProxyConfig{}, config.UpstreamConfig{}, config.GatewayConfig{}, nil)
	router := gin.New()
	Register(router, &Deps{DB: db, Gateway: service, Runtime: runtime})
	return &routeBindingFixture{db: db, router: router, service: service, upstream: upstream, group: group, target: target, token: token}
}

func (fixture *routeBindingFixture) request(groupID, routeID uint, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/gateway/groups/%d/routes/%d/key", groupID, routeID), strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func (fixture *routeBindingFixture) routes(t *testing.T) []storage.GatewayRoute {
	t.Helper()
	routes, err := fixture.service.Routes.ListByGroupID(fixture.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

func TestBindGatewayRouteKeyRequiresAdminAuthentication(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	before := fixture.routes(t)
	for _, token := range []string{"", "invalid-token"} {
		response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":41}`, token)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if fixture.upstream.readCalls != 0 || !reflect.DeepEqual(before, fixture.routes(t)) {
		t.Fatal("unauthorized request accessed upstream or changed routes")
	}
}

func TestBindGatewayRouteKeyOnlyChangesSelectedRoute(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	before := fixture.routes(t)
	response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":41}`, fixture.token)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	after := fixture.routes(t)
	if len(after) != len(before) || !reflect.DeepEqual(before[1:], after[1:]) {
		t.Fatal("binding modified sibling routes, including a disabled route")
	}
	bound := after[0]
	secret, err := fixture.service.Cipher.Decrypt(bound.SourceAPIKeyCipher)
	if err != nil || secret != fixture.upstream.secret || bound.SourceAPIKeyID != 41 || bound.SourceAPIKeyName != "same-name" {
		t.Fatal("requested key was not bound correctly")
	}
	bound.SourceAPIKeyID, bound.SourceAPIKeyName, bound.SourceAPIKeyCipher, bound.UpdatedAt = before[0].SourceAPIKeyID, before[0].SourceAPIKeyName, before[0].SourceAPIKeyCipher, before[0].UpdatedAt
	if !reflect.DeepEqual(before[0], bound) {
		t.Fatal("binding changed unrelated target route settings")
	}
	var returned storage.GatewayRoute
	if err := json.Unmarshal(response.Body.Bytes(), &returned); err != nil || returned.ID != fixture.target.ID || returned.SourceAPIKeyID != 41 {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	for _, forbidden := range []string{fixture.upstream.secret, after[0].SourceAPIKeyCipher, "source_api_key_cipher", "sk-previous-route-secret"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatal("response exposed a key or cipher")
		}
	}
	if fixture.upstream.createCalls != 0 || fixture.upstream.updateCalls != 0 || !reflect.DeepEqual(fixture.upstream.revealedIDs, []int64{41}) {
		t.Fatal("binding touched an unrequested upstream key")
	}
}

func TestBindGatewayRouteKeyRejectsInvalidBodies(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	before := fixture.routes(t)
	for _, body := range []string{"", "{", "null", "[]", `{}`, `{"source_api_key_id":0}`, `{"source_api_key_id":-1}`, `{"source_api_key_id":1.5}`, `{"source_api_key_id":"41"}`, `{"source_api_key_id":null}`, `{"source_api_key_id":9223372036854775808}`, `{"source_api_key_id":41,"secret":"do-not-echo"}`, `{"secret":"do-not-echo","source_api_key_id":41}`, `{"source_api_key_id":41,"source_api_key_id":42}`, `{"SOURCE_API_KEY_ID":41}`, `{"source_api_key_id":41} {}`, `{"source_api_key_id":41} trailing`, `{"source_api_key_id":` + strings.Repeat(" ", 4096) + `41}`} {
		t.Run(body, func(t *testing.T) {
			response := fixture.request(fixture.group.ID, fixture.target.ID, body, fixture.token)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "do-not-echo") {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
	if fixture.upstream.readCalls != 0 || !reflect.DeepEqual(before, fixture.routes(t)) {
		t.Fatal("invalid input accessed upstream or changed routes")
	}
}

func TestBindGatewayRouteKeyRejectsInvalidPathIDs(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	for _, path := range []string{"/api/gateway/groups/0/routes/1/key", "/api/gateway/groups/1/routes/0/key", "/api/gateway/groups/invalid/routes/1/key", "/api/gateway/groups/1/routes/-1/key", "/api/gateway/groups/18446744073709551616/routes/1/key", "/api/gateway/groups/1/routes/18446744073709551616/key"} {
		request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"source_api_key_id":41}`))
		request.Header.Set("Authorization", "Bearer "+fixture.token)
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if fixture.upstream.readCalls != 0 {
		t.Fatal("invalid target accessed upstream")
	}
}

func TestBindGatewayRouteKeyRejectsMissingOrWrongGroup(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	otherGroup := storage.GatewayGroup{Name: "other-gateway-group"}
	if err := fixture.db.Create(&otherGroup).Error; err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]uint{{9999, fixture.target.ID}, {otherGroup.ID, fixture.target.ID}, {fixture.group.ID, 9999}} {
		response := fixture.request(target[0], target[1], `{"source_api_key_id":41}`, fixture.token)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if fixture.upstream.readCalls != 0 {
		t.Fatal("missing target accessed upstream")
	}
}

func TestBindGatewayRouteKeyRejectsSameNameInOtherSource(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	before := fixture.routes(t)
	response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":42}`, fixture.token)
	if response.Code != http.StatusBadRequest || !reflect.DeepEqual(before, fixture.routes(t)) || len(fixture.upstream.revealedIDs) != 0 {
		t.Fatalf("foreign source key was not rejected safely: status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestBindGatewayRouteKeyRejectsUnknownKeyID(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	before := fixture.routes(t)
	response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":9999}`, fixture.token)
	if response.Code != http.StatusBadRequest || !reflect.DeepEqual(before, fixture.routes(t)) || len(fixture.upstream.revealedIDs) != 0 {
		t.Fatalf("unknown key was not rejected safely: status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestBindGatewayRouteKeyRedactsUpstreamErrors(t *testing.T) {
	for _, stage := range []string{"list", "reveal"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newRouteBindingFixture(t)
			before := fixture.routes(t)
			upstreamError := errors.New("supplier failed: Authorization: Bearer sk-do-not-echo")
			if stage == "list" {
				fixture.upstream.listError = upstreamError
			} else {
				fixture.upstream.revealError = upstreamError
			}
			response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":41}`, fixture.token)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "sk-do-not-echo") || strings.Contains(response.Body.String(), "Authorization") || !reflect.DeepEqual(before, fixture.routes(t)) {
				t.Fatalf("unsafe error response: status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestBindGatewayRouteKeyReportsConcurrentChange(t *testing.T) {
	fixture := newRouteBindingFixture(t)
	fixture.upstream.beforeReveal = func() {
		if err := fixture.db.Model(&storage.GatewayRoute{}).Where("id = ?", fixture.target.ID).Update("source_group_name", "changed-during-reveal").Error; err != nil {
			t.Fatal(err)
		}
	}
	response := fixture.request(fixture.group.ID, fixture.target.ID, `{"source_api_key_id":41}`, fixture.token)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	stored, err := fixture.service.Routes.FindByID(fixture.target.ID)
	if err != nil || stored.SourceGroupName != "changed-during-reveal" || stored.SourceAPIKeyID != fixture.target.SourceAPIKeyID || stored.SourceAPIKeyCipher != fixture.target.SourceAPIKeyCipher {
		t.Fatal("concurrent change was overwritten")
	}
}
