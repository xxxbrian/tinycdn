package runtime

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"tinycdn/internal/model"
)

const (
	DefaultManagedTTL        = 5 * time.Minute
	MaxManagedStaleRetention = 365 * 24 * time.Hour
	DefaultOptimisticSWR     = 365 * 24 * time.Hour
)

type Snapshot struct {
	Version string
	Sites   []*CompiledSite
	Hosts   map[string]*CompiledSite
}

type CompiledSite struct {
	Source   model.Site
	Upstream *url.URL
	Proxy    *httputil.ReverseProxy
	Rules    []CompiledRule
}

type CompiledRule struct {
	Source          model.Rule
	TTL             time.Duration
	HasTTL          bool
	StaleIfError    time.Duration
	HasStaleIfError bool
}

type Manager struct {
	current atomic.Pointer[Snapshot]
}

func NewManager(snapshot *Snapshot) *Manager {
	manager := &Manager{}
	manager.Swap(snapshot)
	return manager
}

func (m *Manager) Get() *Snapshot {
	return m.current.Load()
}

func (m *Manager) Swap(snapshot *Snapshot) {
	m.current.Store(snapshot)
}

func Compile(cfg model.AppConfig) (*Snapshot, error) {
	snapshot := &Snapshot{
		Version: time.Now().UTC().Format(time.RFC3339Nano),
		Sites:   make([]*CompiledSite, 0, len(cfg.Sites)),
		Hosts:   map[string]*CompiledSite{},
	}

	for _, site := range cfg.Sites {
		compiledSite, err := compileSite(site)
		if err != nil {
			return nil, fmt.Errorf("compile site %q: %w", site.ID, err)
		}

		snapshot.Sites = append(snapshot.Sites, compiledSite)
		for _, host := range site.Hosts {
			snapshot.Hosts[model.NormalizeHost(host)] = compiledSite
		}
	}

	return snapshot, nil
}

func compileSite(site model.Site) (*CompiledSite, error) {
	upstream, err := url.Parse(site.Upstream.URL)
	if err != nil {
		return nil, err
	}

	compiled := &CompiledSite{
		Source:   site,
		Upstream: upstream,
		Rules:    make([]CompiledRule, 0, len(site.Rules)),
	}

	for _, rule := range site.Rules {
		compiledRule, err := compileRule(rule)
		if err != nil {
			return nil, fmt.Errorf("compile rule %q: %w", rule.ID, err)
		}
		compiled.Rules = append(compiled.Rules, compiledRule)
	}

	compiled.Proxy = buildReverseProxy(compiled)

	return compiled, nil
}

func compileRule(rule model.Rule) (CompiledRule, error) {
	ttl, hasTTL, err := model.ParseOptionalDuration(rule.Action.Cache.TTL)
	if err != nil {
		return CompiledRule{}, err
	}
	staleIfError, hasStaleIfError, err := model.ParseOptionalDuration(rule.Action.Cache.StaleIfError)
	if err != nil {
		return CompiledRule{}, err
	}
	return CompiledRule{
		Source:          rule,
		TTL:             ttl,
		HasTTL:          hasTTL,
		StaleIfError:    staleIfError,
		HasStaleIfError: hasStaleIfError,
	}, nil
}

func (s *Snapshot) SiteByHost(host string) *CompiledSite {
	if s == nil {
		return nil
	}

	return s.Hosts[model.NormalizeHost(host)]
}

func (s *CompiledSite) MatchRule(r *http.Request) *CompiledRule {
	for index := range s.Rules {
		rule := &s.Rules[index]
		if !rule.Source.Enabled {
			continue
		}
		if matchesRule(rule.Source.Match, r) {
			return rule
		}
	}

	return nil
}

func matchesRule(match model.MatchSpec, r *http.Request) bool {
	if match.Any {
		return true
	}

	if len(match.Clauses) == 0 {
		return false
	}

	groupResult := matchesClause(match.Clauses[0], r)
	result := false
	for _, clause := range match.Clauses[1:] {
		switch clause.Logical {
		case model.MatchLogicalOr:
			result = result || groupResult
			groupResult = matchesClause(clause, r)
		default:
			groupResult = groupResult && matchesClause(clause, r)
		}
	}

	return result || groupResult
}

func matchesClause(clause model.MatchClause, r *http.Request) bool {
	path := r.URL.Path

	switch clause.Field {
	case model.MatchFieldHostname:
		host := model.NormalizeHost(r.Host)
		switch clause.Operator {
		case model.MatchOperatorEquals:
			return strings.EqualFold(host, strings.TrimSpace(clause.Value))
		case model.MatchOperatorNotEquals:
			return !strings.EqualFold(host, strings.TrimSpace(clause.Value))
		case model.MatchOperatorContains:
			return strings.Contains(strings.ToLower(host), strings.ToLower(strings.TrimSpace(clause.Value)))
		case model.MatchOperatorNotContains:
			return !strings.Contains(strings.ToLower(host), strings.ToLower(strings.TrimSpace(clause.Value)))
		case model.MatchOperatorStartsWith:
			return strings.HasPrefix(strings.ToLower(host), strings.ToLower(strings.TrimSpace(clause.Value)))
		case model.MatchOperatorNotStartsWith:
			return !strings.HasPrefix(strings.ToLower(host), strings.ToLower(strings.TrimSpace(clause.Value)))
		}
	case model.MatchFieldURIPath:
		switch clause.Operator {
		case model.MatchOperatorEquals:
			return path == clause.Value
		case model.MatchOperatorNotEquals:
			return path != clause.Value
		case model.MatchOperatorContains:
			return strings.Contains(path, clause.Value)
		case model.MatchOperatorNotContains:
			return !strings.Contains(path, clause.Value)
		case model.MatchOperatorStartsWith:
			return strings.HasPrefix(path, clause.Value)
		case model.MatchOperatorNotStartsWith:
			return !strings.HasPrefix(path, clause.Value)
		case model.MatchOperatorMatchesGlob:
			return pathMatchesGlob(path, clause.Value)
		case model.MatchOperatorNotMatchesGlob:
			return !pathMatchesGlob(path, clause.Value)
		}
	case model.MatchFieldMethod:
		method := strings.ToUpper(r.Method)
		switch clause.Operator {
		case model.MatchOperatorEquals:
			return strings.EqualFold(method, strings.TrimSpace(clause.Value))
		case model.MatchOperatorNotEquals:
			return !strings.EqualFold(method, strings.TrimSpace(clause.Value))
		}
	case model.MatchFieldRequestHeader:
		value := r.Header.Get(clause.Name)
		switch clause.Operator {
		case model.MatchOperatorExists:
			return value != ""
		case model.MatchOperatorNotExists:
			return value == ""
		case model.MatchOperatorEquals:
			return value == clause.Value
		case model.MatchOperatorNotEquals:
			return value != clause.Value
		case model.MatchOperatorContains:
			return strings.Contains(value, clause.Value)
		case model.MatchOperatorNotContains:
			return !strings.Contains(value, clause.Value)
		}
	}

	return false
}
func pathMatchesGlob(path string, pattern string) bool {
	ok, err := doublestar.Match(pattern, strings.TrimPrefix(path, "/"))
	if err != nil || !ok {
		ok, err = doublestar.Match(pattern, path)
	}
	return err == nil && ok
}
