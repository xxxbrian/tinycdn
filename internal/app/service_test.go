package app

import (
	"context"
	"path/filepath"
	"testing"

	"tinycdn/internal/config"
	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

type fakeCacheController struct {
	purgeSiteCalls []string
	purgeURLCalls  []struct {
		siteID   string
		path     string
		rawQuery string
	}
}

func (f *fakeCacheController) PurgeSite(_ context.Context, siteID string) (int, error) {
	f.purgeSiteCalls = append(f.purgeSiteCalls, siteID)
	return 5, nil
}

func (f *fakeCacheController) PurgeURL(_ context.Context, siteID, path, rawQuery string) (int, error) {
	f.purgeURLCalls = append(f.purgeURLCalls, struct {
		siteID   string
		path     string
		rawQuery string
	}{siteID: siteID, path: path, rawQuery: rawQuery})
	return 2, nil
}

func TestCreateRulePreservesDisabledState(t *testing.T) {
	cfg := model.AppConfig{
		Sites: []model.Site{
			{
				ID:      "site-1",
				Name:    "Site 1",
				Enabled: true,
				Hosts:   []string{"example.com"},
				Upstream: model.Upstream{
					URL: "https://origin.example.com",
				},
				Rules: []model.Rule{
					model.NewDefaultRule(),
				},
			},
		},
	}

	snapshot, err := runtime.Compile(cfg)
	if err != nil {
		t.Fatalf("compile runtime: %v", err)
	}

	service := NewService(
		config.NewStore(filepath.Join(t.TempDir(), "config.yaml")),
		runtime.NewManager(snapshot),
		cfg,
	)

	created, err := service.CreateRule("site-1", model.Rule{
		Name:    "Disabled rule",
		Enabled: false,
		Match: model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldHostname,
					Operator: model.MatchOperatorEquals,
					Value:    "example.com",
				},
			},
		},
		Action: model.RuleAction{
			Cache: model.CacheAction{
				Mode: model.CacheModeBypass,
			},
		},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if created.Enabled {
		t.Fatalf("expected disabled rule to remain disabled")
	}
}

func TestPurgeCacheBySite(t *testing.T) {
	service := newTestService(t)
	controller := &fakeCacheController{}
	service.SetCacheController(controller)

	result, err := service.PurgeCache(context.Background(), "site-1", PurgeCacheInput{All: true})
	if err != nil {
		t.Fatalf("purge site cache: %v", err)
	}
	if result.Scope != "site" || result.Purged != 5 {
		t.Fatalf("unexpected purge result: %#v", result)
	}
	if len(controller.purgeSiteCalls) != 1 || controller.purgeSiteCalls[0] != "site-1" {
		t.Fatalf("unexpected purge site calls: %#v", controller.purgeSiteCalls)
	}
}

func TestPurgeCacheByURLValidatesSiteHosts(t *testing.T) {
	service := newTestService(t)
	controller := &fakeCacheController{}
	service.SetCacheController(controller)

	result, err := service.PurgeCache(context.Background(), "site-1", PurgeCacheInput{
		URLs: []string{"https://example.com/assets/app.js?rev=1", "/assets/other.js"},
	})
	if err != nil {
		t.Fatalf("purge cache by url: %v", err)
	}
	if result.Scope != "url" || result.Purged != 4 {
		t.Fatalf("unexpected purge result: %#v", result)
	}
	if len(controller.purgeURLCalls) != 2 {
		t.Fatalf("unexpected purge url calls: %#v", controller.purgeURLCalls)
	}
	if controller.purgeURLCalls[0].path != "/assets/app.js" || controller.purgeURLCalls[0].rawQuery != "rev=1" {
		t.Fatalf("unexpected absolute url parse result: %#v", controller.purgeURLCalls[0])
	}
	if controller.purgeURLCalls[1].path != "/assets/other.js" || controller.purgeURLCalls[1].rawQuery != "" {
		t.Fatalf("unexpected path purge result: %#v", controller.purgeURLCalls[1])
	}

	if _, err := service.PurgeCache(context.Background(), "site-1", PurgeCacheInput{
		URLs: []string{"https://evil.example/assets/app.js"},
	}); err == nil {
		t.Fatalf("expected foreign host purge to fail")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	cfg := model.AppConfig{
		Sites: []model.Site{
			{
				ID:      "site-1",
				Name:    "Site 1",
				Enabled: true,
				Hosts:   []string{"example.com"},
				Upstream: model.Upstream{
					URL: "https://origin.example.com",
				},
				Rules: []model.Rule{
					model.NewDefaultRule(),
				},
			},
		},
	}

	snapshot, err := runtime.Compile(cfg)
	if err != nil {
		t.Fatalf("compile runtime: %v", err)
	}

	return NewService(
		config.NewStore(filepath.Join(t.TempDir(), "config.yaml")),
		runtime.NewManager(snapshot),
		cfg,
	)
}
