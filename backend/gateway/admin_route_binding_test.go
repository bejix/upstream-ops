package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/crypto"
	"github.com/bejix/upstream-ops/backend/storage"
	"gorm.io/gorm"
)

type routeBindingUpstream struct {
	groups       []connector.APIKeyGroup
	keys         []connector.APIKey
	secret       string
	listCalls    int
	groupCalls   int
	createCalls  int
	updateCalls  int
	revealCalls  int
	beforeReveal func()
	beforeCreate func()
	pageOverride func(connector.APIKeyQuery) (*connector.APIKeyPage, error)
	createResult *connector.APIKey
	revealError  error
	groupsError  error
}

func (upstream *routeBindingUpstream) ListAPIKeys(_ context.Context, _ uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	upstream.listCalls++
	if upstream.pageOverride != nil {
		return upstream.pageOverride(query)
	}
	start := (query.Page - 1) * query.PageSize
	if start > len(upstream.keys) {
		start = len(upstream.keys)
	}
	end := start + query.PageSize
	if end > len(upstream.keys) {
		end = len(upstream.keys)
	}
	pages := (len(upstream.keys) + query.PageSize - 1) / query.PageSize
	if pages == 0 {
		pages = 1
	}
	return &connector.APIKeyPage{Items: upstream.keys[start:end], Total: int64(len(upstream.keys)), Page: query.Page, PageSize: query.PageSize, Pages: pages}, nil
}

func (upstream *routeBindingUpstream) ListAPIKeyGroups(context.Context, uint) ([]connector.APIKeyGroup, error) {
	upstream.groupCalls++
	return upstream.groups, upstream.groupsError
}

func (upstream *routeBindingUpstream) CreateAPIKey(_ context.Context, _ uint, request connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	upstream.createCalls++
	if upstream.beforeCreate != nil {
		upstream.beforeCreate()
	}
	if upstream.createResult != nil {
		return upstream.createResult, nil
	}
	key := connector.APIKey{ID: 800, Name: request.Name, Group: request.Group, GroupID: request.GroupID, Status: "active", ExpiredTime: -1}
	upstream.keys = append(upstream.keys, key)
	return &key, nil
}

func (upstream *routeBindingUpstream) UpdateAPIKey(context.Context, uint, int64, connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	upstream.updateCalls++
	return nil, errors.New("existing upstream keys must never be changed")
}

func (upstream *routeBindingUpstream) RevealAPIKey(context.Context, uint, int64) (string, error) {
	upstream.revealCalls++
	if upstream.beforeReveal != nil {
		upstream.beforeReveal()
	}
	return upstream.secret, upstream.revealError
}

type bindingTestFixture struct {
	db       *gorm.DB
	service  *Service
	upstream *routeBindingUpstream
	channel  storage.Channel
	group    storage.GatewayGroup
	route    storage.GatewayRoute
	sibling  storage.GatewayRoute
}

func newBindingTestFixture(t *testing.T, channelType storage.ChannelType) *bindingTestFixture {
	t.Helper()
	db := openGatewayTestDB(t)
	cipher, err := crypto.NewCipher("binding-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	channel := storage.Channel{Name: "binding-supplier", Type: channelType, SiteURL: "https://upstream.example", Username: "fixture", PasswordCipher: "cipher"}
	group := storage.GatewayGroup{Name: "binding-group", ModelsJSON: `[{"id":"model-a"}]`}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	var sourceID *int64
	if channelType == storage.ChannelTypeSub2API {
		value := int64(7)
		sourceID = &value
	}
	route := storage.GatewayRoute{GatewayGroupID: group.ID, SourceChannelID: channel.ID, SourceGroupName: "source", SourceGroupID: sourceID, Weight: 9, BillingRateMultiplier: 0.035, ModelMappingJSON: `{"model-a":"private-a"}`, Enabled: true, Concurrency: 17, UserAgentMode: "custom", UserAgentCustom: "preserve-agent"}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	sibling := route
	sibling.ID = 0
	sibling.Position = 1
	sibling.SourceAPIKeyID = 30
	sibling.SourceAPIKeyName = "other-route-key"
	sibling.SourceAPIKeyCipher, err = cipher.Encrypt("sk-sibling-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&sibling).Error; err != nil {
		t.Fatal(err)
	}
	upstream := &routeBindingUpstream{
		groups: []connector.APIKeyGroup{{ID: sourceID, Name: "source", Ratio: 0.25}},
		keys:   []connector.APIKey{{ID: 41, Name: "user-managed-key", Status: "active", Group: "source", GroupID: sourceID, ExpiredTime: -1, Quota: 20, QuotaUsed: 3, RateLimit1d: 5, AllowIPs: "192.0.2.1", ModelLimits: "model-a", ModelLimitsEnabled: true}},
		secret: "sk-selected-secret",
	}
	service := NewService(storage.NewGatewayGroups(db), storage.NewGatewayKeys(db), storage.NewGatewayRoutes(db), nil, nil, storage.NewChannels(db), upstream, cipher, nil)
	if err := db.First(&route, route.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&sibling, sibling.ID).Error; err != nil {
		t.Fatal(err)
	}
	return &bindingTestFixture{db: db, service: service, upstream: upstream, channel: channel, group: group, route: route, sibling: sibling}
}

func TestBindRouteKeyOnlyChangesTargetBindingAndIsIdempotent(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	beforeKeys, _ := json.Marshal(fixture.upstream.keys)
	fixture.service.modelsCache[fixture.group.ID] = modelsCacheEntry{body: []byte("target")}
	fixture.service.modelsCache[999] = modelsCacheEntry{body: []byte("other")}
	bound, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41})
	if err != nil {
		t.Fatal(err)
	}
	if bound.SourceAPIKeyID != 41 || bound.SourceAPIKeyName != "user-managed-key" {
		t.Fatalf("incorrect bound identity: %+v", bound)
	}
	secret, err := fixture.service.Cipher.Decrypt(bound.SourceAPIKeyCipher)
	if err != nil || secret != fixture.upstream.secret {
		t.Fatalf("encrypted key mismatch: %v", err)
	}
	unchanged := *bound
	unchanged.SourceAPIKeyID = fixture.route.SourceAPIKeyID
	unchanged.SourceAPIKeyName = fixture.route.SourceAPIKeyName
	unchanged.SourceAPIKeyCipher = fixture.route.SourceAPIKeyCipher
	unchanged.UpdatedAt = fixture.route.UpdatedAt
	if !reflect.DeepEqual(unchanged, fixture.route) {
		t.Fatal("non-binding fields changed")
	}
	sibling, err := fixture.service.Routes.FindByID(fixture.sibling.ID)
	if err != nil || !reflect.DeepEqual(*sibling, fixture.sibling) {
		t.Fatalf("sibling changed: %v", err)
	}
	if _, exists := fixture.service.modelsCache[fixture.group.ID]; exists {
		t.Fatal("target models cache was not invalidated")
	}
	if _, exists := fixture.service.modelsCache[999]; !exists {
		t.Fatal("unrelated models cache was invalidated")
	}
	again, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41})
	if err != nil || !reflect.DeepEqual(bound, again) {
		t.Fatalf("binding not idempotent: %v", err)
	}
	afterKeys, _ := json.Marshal(fixture.upstream.keys)
	if string(beforeKeys) != string(afterKeys) || fixture.upstream.updateCalls != 0 || fixture.upstream.createCalls != 0 {
		t.Fatal("remote keys were mutated")
	}
	encoded, _ := json.Marshal(bound)
	if strings.Contains(string(encoded), fixture.upstream.secret) || strings.Contains(string(encoded), bound.SourceAPIKeyCipher) {
		t.Fatal("binding response exposed secret")
	}
}

func TestBindRouteKeyFindsKeyBeyondOneHundredPages(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	template := fixture.upstream.keys[0]
	fixture.upstream.keys = make([]connector.APIKey, 10001)
	for index := range fixture.upstream.keys {
		fixture.upstream.keys[index] = template
		fixture.upstream.keys[index].ID = int64(index + 1)
	}
	bound, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 10001})
	if err != nil || bound.SourceAPIKeyID != 10001 || fixture.upstream.listCalls != 101 {
		t.Fatalf("full pagination failed: calls=%d err=%v", fixture.upstream.listCalls, err)
	}
}

func TestBindRouteKeyRefreshesNameAndRejectsConcurrentDeletion(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	input := BindRouteKeyInput{SourceAPIKeyID: 41}
	if _, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, input); err != nil {
		t.Fatal(err)
	}
	fixture.upstream.keys[0].Name = "renamed-by-owner"
	bound, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, input)
	if err != nil || bound.SourceAPIKeyName != "renamed-by-owner" || fixture.upstream.updateCalls != 0 {
		t.Fatalf("existing key rename did not refresh locally: %v", err)
	}
	fixture.upstream.beforeReveal = func() {
		if err := fixture.db.Delete(&storage.GatewayRoute{}, fixture.route.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, input); !errors.Is(err, storage.ErrGatewayRouteChanged) {
		t.Fatalf("deletion during idempotent bind must conflict: %v", err)
	}
}

func TestRouteKeyOperationsRespectCancelledContext(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeNewAPI)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.BindRouteKey(ctx, fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41}); !errors.Is(err, context.Canceled) {
		t.Fatalf("binding ignored cancellation: %v", err)
	}
	if _, err := fixture.service.EnsureRouteKeys(ctx, fixture.group.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("ensure ignored cancellation: %v", err)
	}
	if fixture.upstream.groupCalls != 0 || fixture.upstream.listCalls != 0 || fixture.upstream.revealCalls != 0 || fixture.upstream.createCalls != 0 {
		t.Fatal("cancelled operations contacted upstream")
	}
}

func TestBindRouteKeyRejectsInvalidIdentityOrState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bindingTestFixture)
		keyID  int64
	}{
		{"unknown key", func(*bindingTestFixture) {}, 999},
		{"zero key", func(*bindingTestFixture) {}, 0},
		{"same name different group id", func(fixture *bindingTestFixture) {
			wrongID := int64(8)
			fixture.upstream.keys[0].GroupID = &wrongID
			fixture.upstream.keys[0].GroupName = "source"
		}, 41},
		{"missing group id", func(fixture *bindingTestFixture) { fixture.upstream.keys[0].GroupID = nil }, 41},
		{"source group removed same name remains", func(fixture *bindingTestFixture) { wrongID := int64(8); fixture.upstream.groups[0].ID = &wrongID }, 41},
		{"disabled", func(fixture *bindingTestFixture) { fixture.upstream.keys[0].Status = "disabled" }, 41},
		{"unknown status", func(fixture *bindingTestFixture) { fixture.upstream.keys[0].Status = "" }, 41},
		{"expired", func(fixture *bindingTestFixture) {
			expired := time.Now().Add(-time.Minute)
			fixture.upstream.keys[0].ExpiresAt = &expired
		}, 41},
		{"unix expired", func(fixture *bindingTestFixture) {
			fixture.upstream.keys[0].ExpiredTime = time.Now().Add(-time.Minute).Unix()
		}, 41},
		{"ambiguous source group", func(fixture *bindingTestFixture) {
			fixture.upstream.groups = append(fixture.upstream.groups, fixture.upstream.groups[0])
		}, 41},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
			test.mutate(fixture)
			_, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: test.keyID})
			if err == nil || fixture.upstream.revealCalls != 0 || fixture.upstream.createCalls != 0 || fixture.upstream.updateCalls != 0 {
				t.Fatalf("unsafe invalid binding: %v", err)
			}
			current, _ := fixture.service.Routes.FindByID(fixture.route.ID)
			if !reflect.DeepEqual(*current, fixture.route) {
				t.Fatal("invalid bind changed route")
			}
		})
	}
}

func TestBindRouteKeyNewAPIRequiresExactTrimmedGroup(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeNewAPI)
	fixture.upstream.keys[0].Group = " source "
	if _, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41}); err != nil {
		t.Fatal(err)
	}
	fixture.upstream.keys[0].Group = "Source"
	if _, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41}); err == nil {
		t.Fatal("NewAPI group names matched case insensitively")
	}
}

func TestBindRouteKeyRejectsMaskedKeysAndSanitizesUpstreamErrors(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	for _, secret := range []string{"", "sk-***", "sk-…", "sk-...", "sk-abc\n", "sk abc", "sk-\x00abc"} {
		fixture.upstream.secret = secret
		if _, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41}); err == nil {
			t.Fatalf("invalid secret was accepted: %q", secret)
		}
	}
	fixture.upstream.revealError = errors.New("provider body contains sk-private and token=private")
	_, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41})
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "token=") {
		t.Fatalf("unsafe upstream error: %v", err)
	}
}

func TestBindRouteKeyRejectsConcurrentRouteEdit(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	fixture.upstream.beforeReveal = func() {
		if err := fixture.db.Model(&storage.GatewayRoute{}).Where("id = ?", fixture.route.ID).Update("source_group_id", 99).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err := fixture.service.BindRouteKey(context.Background(), fixture.group.ID, fixture.route.ID, BindRouteKeyInput{SourceAPIKeyID: 41})
	if !errors.Is(err, storage.ErrGatewayRouteChanged) {
		t.Fatalf("want conflict, got %v", err)
	}
	current, _ := fixture.service.Routes.FindByID(fixture.route.ID)
	if current.SourceAPIKeyID != 0 || *current.SourceGroupID != 99 {
		t.Fatal("concurrent route change overwritten")
	}
}

func TestEnsureRouteKeysSkipsExistingDisabledAndProvider(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	fixture.upstream.keys[0].Name = fixture.service.stableUpstreamKeyName(fixture.channel.ID, fixture.route.SourceGroupID, fixture.route.SourceGroupName)
	beforeKeys, _ := json.Marshal(fixture.upstream.keys)
	disabled := fixture.route
	disabled.ID = 0
	disabled.Position = 2
	if err := fixture.db.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&disabled).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	provider := storage.GatewayRoute{GatewayGroupID: fixture.group.ID, Position: 3, SourceKind: storage.GatewayRouteSourceProvider, GatewayProviderID: 77, Enabled: true}
	if err := fixture.db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.OKCount != 1 || result.SkipCount != 3 || result.FailCount != 0 {
		t.Fatalf("unexpected ensure result: %+v %v", result, err)
	}
	afterKeys, _ := json.Marshal(fixture.upstream.keys)
	if string(beforeKeys) != string(afterKeys) || fixture.upstream.createCalls != 0 || fixture.upstream.updateCalls != 0 || fixture.upstream.revealCalls != 1 {
		t.Fatal("ensure mutated or inspected unrelated keys")
	}
	listCalls := fixture.upstream.listCalls
	result, err = fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.SkipCount != 4 || fixture.upstream.listCalls != listCalls {
		t.Fatal("bound routes were not skipped on repeated ensure")
	}
}

func TestEnsureRouteKeysFailsClosedOnInvalidPagination(t *testing.T) {
	tests := []struct {
		name string
		page func(connector.APIKeyQuery) (*connector.APIKeyPage, error)
	}{
		{"error", func(connector.APIKeyQuery) (*connector.APIKeyPage, error) { return nil, errors.New("sk-private") }},
		{"nil", func(connector.APIKeyQuery) (*connector.APIKeyPage, error) { return nil, nil }},
		{"missing total", func(query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
			return &connector.APIKeyPage{Page: query.Page, PageSize: 100, Pages: 1, Items: []connector.APIKey{{ID: 1}}}, nil
		}},
		{"wrong page", func(connector.APIKeyQuery) (*connector.APIKeyPage, error) {
			return &connector.APIKeyPage{Page: 2, PageSize: 100, Pages: 1}, nil
		}},
		{"truncated", func(query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
			return &connector.APIKeyPage{Page: query.Page, PageSize: 100, Pages: 2, Total: 101, Items: []connector.APIKey{{ID: 1}}}, nil
		}},
		{"duplicate", func(query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
			return &connector.APIKeyPage{Page: query.Page, PageSize: 100, Pages: 1, Total: 2, Items: []connector.APIKey{{ID: 1}, {ID: 1}}}, nil
		}},
		{"cycle", func(query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
			return &connector.APIKeyPage{Page: query.Page, PageSize: 1, Pages: 2, Total: 2, Items: []connector.APIKey{{ID: 1}}}, nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
			fixture.upstream.pageOverride = test.page
			result, err := fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
			if err != nil || result.FailCount != 1 || fixture.upstream.createCalls != 0 || fixture.upstream.updateCalls != 0 || fixture.upstream.revealCalls != 0 || strings.Contains(result.Routes[0].Error, "sk-private") {
				t.Fatalf("unsafe pagination handling: %+v %v", result, err)
			}
		})
	}
}

func TestEnsureRouteKeysRejectsStableNameInWrongGroup(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeSub2API)
	wrongID := int64(8)
	fixture.upstream.keys[0].Name = fixture.service.stableUpstreamKeyName(fixture.channel.ID, fixture.route.SourceGroupID, fixture.route.SourceGroupName)
	fixture.upstream.keys[0].GroupID = &wrongID
	result, err := fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.FailCount != 1 || fixture.upstream.updateCalls != 0 || fixture.upstream.createCalls != 0 {
		t.Fatalf("same-name foreign key was modified: %+v %v", result, err)
	}
}

func TestEnsureRouteKeysConcurrentGroupsCreateOnlyOnce(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeNewAPI)
	fixture.upstream.keys = nil
	otherGroup := storage.GatewayGroup{Name: "other-group"}
	if err := fixture.db.Create(&otherGroup).Error; err != nil {
		t.Fatal(err)
	}
	otherRoute := fixture.route
	otherRoute.ID = 0
	otherRoute.GatewayGroupID = otherGroup.ID
	if err := fixture.db.Create(&otherRoute).Error; err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for _, groupID := range []uint{fixture.group.ID, otherGroup.ID} {
		workers.Add(1)
		go func(targetGroup uint) {
			defer workers.Done()
			result, err := fixture.service.EnsureRouteKeys(context.Background(), targetGroup)
			if err == nil && result.FailCount != 0 {
				err = fmt.Errorf("ensure failed: %+v", result.Routes)
			}
			results <- err
		}(groupID)
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fixture.upstream.createCalls != 1 || fixture.upstream.updateCalls != 0 {
		t.Fatal("concurrent ensures created or changed multiple keys")
	}
	first, _ := fixture.service.Routes.FindByID(fixture.route.ID)
	second, _ := fixture.service.Routes.FindByID(otherRoute.ID)
	if first.SourceAPIKeyID != 800 || second.SourceAPIKeyID != 800 {
		t.Fatal("stable key was not shared")
	}
}

func TestEnsureRouteKeysCreateConflictRetainsAndReusesKey(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeNewAPI)
	fixture.upstream.keys = nil
	fixture.upstream.beforeReveal = func() {
		if err := fixture.db.Model(&storage.GatewayRoute{}).Where("id = ?", fixture.route.ID).Update("weight", 19).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.FailCount != 1 || !strings.Contains(result.Routes[0].Error, "#800") || fixture.upstream.createCalls != 1 {
		t.Fatalf("created-key conflict not safely reported: %+v %v", result, err)
	}
	fixture.upstream.beforeReveal = nil
	result, err = fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.OKCount != 1 || fixture.upstream.createCalls != 1 || fixture.upstream.updateCalls != 0 {
		t.Fatalf("retry did not reuse retained key: %+v %v", result, err)
	}
}

func TestEnsureRouteKeysDoesNotTrustCreateResult(t *testing.T) {
	fixture := newBindingTestFixture(t, storage.ChannelTypeNewAPI)
	fixture.upstream.createResult = &connector.APIKey{ID: 41, Name: fixture.service.stableUpstreamKeyName(fixture.channel.ID, nil, "source"), Group: "source", Status: "active", ExpiredTime: -1}
	result, err := fixture.service.EnsureRouteKeys(context.Background(), fixture.group.ID)
	if err != nil || result.FailCount != 1 || fixture.upstream.revealCalls != 0 || fixture.upstream.updateCalls != 0 {
		t.Fatalf("unexpected existing key from create was bound: %+v %v", result, err)
	}
}
