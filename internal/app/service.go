package app

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"tinycdn/internal/cache"
	"tinycdn/internal/config"
	"tinycdn/internal/model"
	"tinycdn/internal/runtime"
)

type Service struct {
	store   *config.Store
	runtime *runtime.Manager
	cache   CacheController

	mu     sync.RWMutex
	config model.AppConfig
}

type CacheController interface {
	PurgeSite(context.Context, string) (int, error)
	PurgeURL(context.Context, string, string, string) (int, error)
	CacheInventory(context.Context) ([]cache.Inventory, error)
}

func NewService(store *config.Store, runtimeManager *runtime.Manager, cfg model.AppConfig) *Service {
	return &Service{
		store:   store,
		runtime: runtimeManager,
		config:  cfg,
	}
}

func (s *Service) SetCacheController(controller CacheController) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = controller
}

func (s *Service) RuntimeSnapshot() *runtime.Snapshot {
	return s.runtime.Get()
}

func (s *Service) CacheInventory(ctx context.Context) ([]cache.Inventory, error) {
	s.mu.RLock()
	controller := s.cache
	s.mu.RUnlock()
	if controller == nil {
		return nil, nil
	}
	return controller.CacheInventory(ctx)
}

func (s *Service) Config() model.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cloned, err := model.CloneConfig(s.config)
	if err != nil {
		return s.config
	}

	return cloned
}

func (s *Service) ListSites() []model.Site {
	return s.Config().Sites
}

func (s *Service) GetSite(id string) (model.Site, bool) {
	for _, site := range s.ListSites() {
		if site.ID == id {
			return site, true
		}
	}

	return model.Site{}, false
}

func (s *Service) Update(mutator func(*model.AppConfig) error) (model.AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextConfig, err := model.CloneConfig(s.config)
	if err != nil {
		return model.AppConfig{}, err
	}
	if err := mutator(&nextConfig); err != nil {
		return model.AppConfig{}, err
	}
	if err := nextConfig.Validate(); err != nil {
		return model.AppConfig{}, err
	}

	nextSnapshot, err := runtime.Compile(nextConfig)
	if err != nil {
		return model.AppConfig{}, err
	}

	previousConfig := s.config
	previousSnapshot := s.runtime.Get()

	s.runtime.Swap(nextSnapshot)
	if err := s.store.Save(nextConfig); err != nil {
		s.runtime.Swap(previousSnapshot)
		return model.AppConfig{}, err
	}

	s.config = nextConfig
	_ = previousConfig

	cloned, err := model.CloneConfig(nextConfig)
	if err != nil {
		return nextConfig, nil
	}

	return cloned, nil
}

func NewSite(input SiteInput) model.Site {
	siteID := stringsOrDefault(input.ID, uuid.NewString())
	site := model.Site{
		ID:      siteID,
		Name:    input.Name,
		Enabled: input.Enabled,
		Hosts:   append([]string(nil), input.Hosts...),
		Upstream: model.Upstream{
			URL:      input.UpstreamURL,
			HostMode: model.UpstreamHostMode(input.UpstreamHostMode),
			Host:     input.UpstreamHost,
		},
		Rules: []model.Rule{
			model.NewDefaultRule(),
		},
	}

	return site
}

type SiteInput struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	Hosts            []string `json:"hosts"`
	UpstreamURL      string   `json:"upstream_url"`
	UpstreamHostMode string   `json:"upstream_host_mode"`
	UpstreamHost     string   `json:"upstream_host"`
}

func (s *Service) CreateSite(input SiteInput) (model.Site, error) {
	updated, err := s.Update(func(cfg *model.AppConfig) error {
		cfg.Sites = append(cfg.Sites, NewSite(input))
		return nil
	})
	if err != nil {
		return model.Site{}, err
	}

	return findSite(updated.Sites, stringsOrDefault(input.ID, updated.Sites[len(updated.Sites)-1].ID))
}

func (s *Service) UpdateSite(id string, input SiteInput) (model.Site, error) {
	updated, err := s.Update(func(cfg *model.AppConfig) error {
		index := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == id })
		if index < 0 {
			return fmt.Errorf("site %q not found", id)
		}

		cfg.Sites[index].Name = input.Name
		cfg.Sites[index].Enabled = input.Enabled
		cfg.Sites[index].Hosts = append([]string(nil), input.Hosts...)
		cfg.Sites[index].Upstream.URL = input.UpstreamURL
		cfg.Sites[index].Upstream.HostMode = model.UpstreamHostMode(input.UpstreamHostMode)
		cfg.Sites[index].Upstream.Host = input.UpstreamHost
		return nil
	})
	if err != nil {
		return model.Site{}, err
	}

	return findSite(updated.Sites, id)
}

func (s *Service) DeleteSite(id string) error {
	_, err := s.Update(func(cfg *model.AppConfig) error {
		index := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == id })
		if index < 0 {
			return fmt.Errorf("site %q not found", id)
		}

		cfg.Sites = append(cfg.Sites[:index], cfg.Sites[index+1:]...)
		return nil
	})
	return err
}

func (s *Service) ListRules(siteID string) ([]model.Rule, error) {
	site, ok := s.GetSite(siteID)
	if !ok {
		return nil, fmt.Errorf("site %q not found", siteID)
	}

	return site.Rules, nil
}

func (s *Service) CreateRule(siteID string, rule model.Rule) (model.Rule, error) {
	if stringsOrDefault(rule.ID, "") == "" {
		rule.ID = uuid.NewString()
	}

	updated, err := s.Update(func(cfg *model.AppConfig) error {
		siteIndex := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == siteID })
		if siteIndex < 0 {
			return fmt.Errorf("site %q not found", siteID)
		}

		rules := cfg.Sites[siteIndex].Rules
		if len(rules) == 0 {
			return fmt.Errorf("site %q has no default rule", siteID)
		}

		insertAt := len(rules) - 1
		cfg.Sites[siteIndex].Rules = append(rules[:insertAt], append([]model.Rule{rule}, rules[insertAt:]...)...)
		return nil
	})
	if err != nil {
		return model.Rule{}, err
	}

	site, err := findSite(updated.Sites, siteID)
	if err != nil {
		return model.Rule{}, err
	}

	return findRule(site.Rules, rule.ID)
}

func (s *Service) UpdateRule(siteID, ruleID string, rule model.Rule) (model.Rule, error) {
	updated, err := s.Update(func(cfg *model.AppConfig) error {
		siteIndex := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == siteID })
		if siteIndex < 0 {
			return fmt.Errorf("site %q not found", siteID)
		}

		ruleIndex := slices.IndexFunc(cfg.Sites[siteIndex].Rules, func(existing model.Rule) bool { return existing.ID == ruleID })
		if ruleIndex < 0 {
			return fmt.Errorf("rule %q not found", ruleID)
		}

		existing := cfg.Sites[siteIndex].Rules[ruleIndex]
		rule.ID = ruleID
		rule.System = existing.System
		if existing.System {
			rule.Match = model.MatchSpec{Any: true}
			rule.Enabled = true
		}

		cfg.Sites[siteIndex].Rules[ruleIndex] = rule
		return nil
	})
	if err != nil {
		return model.Rule{}, err
	}

	site, err := findSite(updated.Sites, siteID)
	if err != nil {
		return model.Rule{}, err
	}

	return findRule(site.Rules, ruleID)
}

func (s *Service) DeleteRule(siteID, ruleID string) error {
	_, err := s.Update(func(cfg *model.AppConfig) error {
		siteIndex := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == siteID })
		if siteIndex < 0 {
			return fmt.Errorf("site %q not found", siteID)
		}

		ruleIndex := slices.IndexFunc(cfg.Sites[siteIndex].Rules, func(rule model.Rule) bool { return rule.ID == ruleID })
		if ruleIndex < 0 {
			return fmt.Errorf("rule %q not found", ruleID)
		}
		if cfg.Sites[siteIndex].Rules[ruleIndex].System {
			return fmt.Errorf("system rule %q cannot be deleted", ruleID)
		}

		cfg.Sites[siteIndex].Rules = append(cfg.Sites[siteIndex].Rules[:ruleIndex], cfg.Sites[siteIndex].Rules[ruleIndex+1:]...)
		return nil
	})
	return err
}

func (s *Service) ReorderRules(siteID string, ruleIDs []string) ([]model.Rule, error) {
	updated, err := s.Update(func(cfg *model.AppConfig) error {
		siteIndex := slices.IndexFunc(cfg.Sites, func(site model.Site) bool { return site.ID == siteID })
		if siteIndex < 0 {
			return fmt.Errorf("site %q not found", siteID)
		}

		existing := cfg.Sites[siteIndex].Rules
		if len(existing) != len(ruleIDs) {
			return fmt.Errorf("reorder payload must include all rules")
		}

		byID := make(map[string]model.Rule, len(existing))
		for _, rule := range existing {
			byID[rule.ID] = rule
		}

		reordered := make([]model.Rule, 0, len(ruleIDs))
		for _, ruleID := range ruleIDs {
			rule, ok := byID[ruleID]
			if !ok {
				return fmt.Errorf("unknown rule %q", ruleID)
			}
			delete(byID, ruleID)
			reordered = append(reordered, rule)
		}
		if len(byID) != 0 {
			return fmt.Errorf("reorder payload omitted rules")
		}

		cfg.Sites[siteIndex].Rules = reordered
		return nil
	})
	if err != nil {
		return nil, err
	}

	site, err := findSite(updated.Sites, siteID)
	if err != nil {
		return nil, err
	}

	return site.Rules, nil
}

type PurgeCacheInput struct {
	All  bool     `json:"all,omitempty"`
	URLs []string `json:"urls,omitempty"`
}

type PurgeCacheResult struct {
	Purged int      `json:"purged"`
	Scope  string   `json:"scope"`
	URLs   []string `json:"urls,omitempty"`
}

func (s *Service) PurgeCache(ctx context.Context, siteID string, input PurgeCacheInput) (PurgeCacheResult, error) {
	site, ok := s.GetSite(siteID)
	if !ok {
		return PurgeCacheResult{}, fmt.Errorf("site %q not found", siteID)
	}

	s.mu.RLock()
	controller := s.cache
	s.mu.RUnlock()
	if controller == nil {
		return PurgeCacheResult{}, fmt.Errorf("cache controller unavailable")
	}

	switch {
	case input.All && len(input.URLs) == 0:
		purged, err := controller.PurgeSite(ctx, siteID)
		if err != nil {
			return PurgeCacheResult{}, err
		}
		return PurgeCacheResult{Purged: purged, Scope: "site"}, nil
	case !input.All && len(input.URLs) > 0:
		purged := 0
		for _, rawURL := range input.URLs {
			parsed, err := parseSitePurgeURL(site, rawURL)
			if err != nil {
				return PurgeCacheResult{}, err
			}
			deleted, err := controller.PurgeURL(ctx, siteID, parsed.Path, parsed.RawQuery)
			if err != nil {
				return PurgeCacheResult{}, err
			}
			purged += deleted
		}
		return PurgeCacheResult{Purged: purged, Scope: "url", URLs: append([]string(nil), input.URLs...)}, nil
	default:
		return PurgeCacheResult{}, fmt.Errorf("purge request must specify either all=true or one or more urls")
	}
}

func parseSitePurgeURL(site model.Site, rawValue string) (*url.URL, error) {
	if rawValue == "" {
		return nil, fmt.Errorf("purge url cannot be empty")
	}

	if strings.HasPrefix(rawValue, "/") {
		parsed, err := url.ParseRequestURI(rawValue)
		if err != nil {
			return nil, fmt.Errorf("invalid purge url %q: %w", rawValue, err)
		}
		return parsed, nil
	}

	parsed, err := url.Parse(rawValue)
	if err != nil {
		return nil, fmt.Errorf("invalid purge url %q: %w", rawValue, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("purge url %q must be an absolute URL or an absolute path", rawValue)
	}
	if !slices.Contains(site.Hosts, parsed.Hostname()) {
		return nil, fmt.Errorf("purge url host %q does not belong to site %q", parsed.Hostname(), site.ID)
	}
	return parsed, nil
}

func findSite(sites []model.Site, id string) (model.Site, error) {
	index := slices.IndexFunc(sites, func(site model.Site) bool { return site.ID == id })
	if index < 0 {
		return model.Site{}, fmt.Errorf("site %q not found", id)
	}
	return sites[index], nil
}

func findRule(rules []model.Rule, id string) (model.Rule, error) {
	index := slices.IndexFunc(rules, func(rule model.Rule) bool { return rule.ID == id })
	if index < 0 {
		return model.Rule{}, fmt.Errorf("rule %q not found", id)
	}
	return rules[index], nil
}

func stringsOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
