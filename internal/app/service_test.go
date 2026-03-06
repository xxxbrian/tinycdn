package app

import (
	"path/filepath"
	"testing"

	"tinycdn/internal/config"
	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

func TestCreateRulePreservesDisabledState(t *testing.T) {
	cfg := model.AppConfig{
		Sites: []model.Site{
			{
				ID:      "site-1",
				Name:    "Site 1",
				Enabled: true,
				Cache:   model.SiteCache{},
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
