package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
)

type AppConfig struct {
	Sites []Site `json:"sites" yaml:"sites"`
}

type Site struct {
	ID       string    `json:"id" yaml:"id"`
	Name     string    `json:"name" yaml:"name"`
	Enabled  bool      `json:"enabled" yaml:"enabled"`
	Cache    SiteCache `json:"cache" yaml:"cache"`
	Hosts    []string  `json:"hosts" yaml:"hosts"`
	Upstream Upstream  `json:"upstream" yaml:"upstream"`
	Rules    []Rule    `json:"rules" yaml:"rules"`
}

type SiteCache struct {
	OptimisticRefresh bool `json:"optimistic_refresh" yaml:"optimistic_refresh"`
}

type Upstream struct {
	URL      string           `json:"url" yaml:"url"`
	HostMode UpstreamHostMode `json:"host_mode,omitempty" yaml:"host_mode,omitempty"`
	Host     string           `json:"host,omitempty" yaml:"host,omitempty"`
}

type UpstreamHostMode string

type Rule struct {
	ID      string     `json:"id" yaml:"id"`
	Name    string     `json:"name" yaml:"name"`
	Enabled bool       `json:"enabled" yaml:"enabled"`
	System  bool       `json:"system,omitempty" yaml:"system,omitempty"`
	Match   MatchSpec  `json:"match" yaml:"match"`
	Action  RuleAction `json:"action" yaml:"action"`
}

type MatchSpec struct {
	Clauses []MatchClause `json:"clauses,omitempty" yaml:"clauses,omitempty"`
	Any     bool          `json:"any,omitempty" yaml:"any,omitempty"`
}

type MatchClause struct {
	Logical  string `json:"logical,omitempty" yaml:"logical,omitempty"`
	Field    string `json:"field" yaml:"field"`
	Operator string `json:"operator" yaml:"operator"`
	Name     string `json:"name,omitempty" yaml:"name,omitempty"`
	Value    string `json:"value,omitempty" yaml:"value,omitempty"`
}

type RuleAction struct {
	Cache CacheAction `json:"cache" yaml:"cache"`
}

type CacheAction struct {
	Mode         CacheMode `json:"mode" yaml:"mode"`
	TTL          string    `json:"ttl,omitempty" yaml:"ttl,omitempty"`
	StaleIfError string    `json:"stale_if_error,omitempty" yaml:"stale_if_error,omitempty"`
}

type CacheMode string

const (
	CacheModeFollowOrigin   CacheMode = "follow_origin"
	CacheModeBypass         CacheMode = "bypass"
	CacheModeForceCache     CacheMode = "force_cache"
	CacheModeOverrideOrigin CacheMode = "override_origin"
)

var validCacheModes = []CacheMode{
	CacheModeFollowOrigin,
	CacheModeBypass,
	CacheModeForceCache,
	CacheModeOverrideOrigin,
}

const (
	UpstreamHostModeFollowOrigin  UpstreamHostMode = "follow_origin"
	UpstreamHostModeFollowRequest UpstreamHostMode = "follow_request"
	UpstreamHostModeCustom        UpstreamHostMode = "custom"
)

var validUpstreamHostModes = []UpstreamHostMode{
	UpstreamHostModeFollowOrigin,
	UpstreamHostModeFollowRequest,
	UpstreamHostModeCustom,
}

const (
	MatchLogicalAnd = "and"
	MatchLogicalOr  = "or"

	MatchFieldHostname      = "hostname"
	MatchFieldURIPath       = "uri_path"
	MatchFieldMethod        = "method"
	MatchFieldRequestHeader = "request_header"

	MatchOperatorEquals         = "equals"
	MatchOperatorNotEquals      = "not_equals"
	MatchOperatorContains       = "contains"
	MatchOperatorNotContains    = "not_contains"
	MatchOperatorStartsWith     = "starts_with"
	MatchOperatorNotStartsWith  = "not_starts_with"
	MatchOperatorMatchesGlob    = "matches_glob"
	MatchOperatorNotMatchesGlob = "not_matches_glob"
	MatchOperatorExists         = "exists"
	MatchOperatorNotExists      = "not_exists"
)

func DefaultConfig() AppConfig {
	return AppConfig{Sites: []Site{}}
}

func NewDefaultRule() Rule {
	return Rule{
		ID:      "default",
		Name:    "Default Rule",
		Enabled: true,
		System:  true,
		Match: MatchSpec{
			Any: true,
		},
		Action: RuleAction{
			Cache: CacheAction{
				Mode: CacheModeFollowOrigin,
			},
		},
	}
}

func CloneConfig(cfg AppConfig) (AppConfig, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return AppConfig{}, err
	}

	var cloned AppConfig
	if err := json.Unmarshal(b, &cloned); err != nil {
		return AppConfig{}, err
	}

	return cloned, nil
}

func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}

	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}

	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return strings.TrimSpace(strings.ToLower(parsedHost))
	}

	return host
}

func (cfg AppConfig) Validate() error {
	siteIDs := map[string]struct{}{}
	hostOwners := map[string]string{}

	for siteIndex, site := range cfg.Sites {
		if err := validateSite(site); err != nil {
			return fmt.Errorf("sites[%d]: %w", siteIndex, err)
		}

		if _, ok := siteIDs[site.ID]; ok {
			return fmt.Errorf("sites[%d]: duplicate site id %q", siteIndex, site.ID)
		}
		siteIDs[site.ID] = struct{}{}

		for _, host := range site.Hosts {
			normalized := NormalizeHost(host)
			if owner, ok := hostOwners[normalized]; ok {
				return fmt.Errorf("sites[%d]: host %q is already used by site %q", siteIndex, normalized, owner)
			}
			hostOwners[normalized] = site.ID
		}
	}

	return nil
}

func validateSite(site Site) error {
	if strings.TrimSpace(site.ID) == "" {
		return errors.New("site id is required")
	}
	if strings.TrimSpace(site.Name) == "" {
		return errors.New("site name is required")
	}
	if len(site.Hosts) == 0 {
		return errors.New("at least one host is required")
	}

	for _, host := range site.Hosts {
		if NormalizeHost(host) == "" {
			return fmt.Errorf("invalid host %q", host)
		}
	}

	upstreamURL, err := url.Parse(site.Upstream.URL)
	if err != nil || upstreamURL == nil {
		return fmt.Errorf("invalid upstream url %q", site.Upstream.URL)
	}
	if upstreamURL.Scheme != "http" && upstreamURL.Scheme != "https" {
		return fmt.Errorf("upstream url must use http or https, got %q", site.Upstream.URL)
	}
	if upstreamURL.Host == "" {
		return fmt.Errorf("upstream url must include host, got %q", site.Upstream.URL)
	}

	hostMode := site.Upstream.HostMode
	if hostMode == "" {
		hostMode = UpstreamHostModeFollowOrigin
	}
	if !slices.Contains(validUpstreamHostModes, hostMode) {
		return fmt.Errorf("unsupported upstream host mode %q", site.Upstream.HostMode)
	}
	normalizedHost, err := NormalizeUpstreamRequestHost(site.Upstream.Host)
	if err != nil {
		return fmt.Errorf("invalid upstream host %q: %w", site.Upstream.Host, err)
	}
	switch hostMode {
	case UpstreamHostModeCustom:
		if normalizedHost == "" {
			return errors.New("custom upstream host mode requires host")
		}
	default:
		if normalizedHost != "" {
			return errors.New("upstream host is only supported when host mode is custom")
		}
	}

	if len(site.Rules) == 0 {
		return errors.New("site must contain at least one rule")
	}

	ruleIDs := map[string]struct{}{}
	for index, rule := range site.Rules {
		if err := validateRule(rule, index == len(site.Rules)-1); err != nil {
			return fmt.Errorf("rules[%d]: %w", index, err)
		}
		if _, ok := ruleIDs[rule.ID]; ok {
			return fmt.Errorf("rules[%d]: duplicate rule id %q", index, rule.ID)
		}
		ruleIDs[rule.ID] = struct{}{}
	}

	return nil
}

func NormalizeUpstreamRequestHost(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	parseCandidate := value
	if !strings.Contains(parseCandidate, "://") {
		parseCandidate = "//" + parseCandidate
	}

	parsed, err := url.Parse(parseCandidate)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", errors.New("host is required")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("host must not include a path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("host must not include query or fragment")
	}
	if parsed.User != nil {
		return "", errors.New("host must not include user info")
	}

	return strings.ToLower(parsed.Host), nil
}

func validateRule(rule Rule, isLast bool) error {
	if strings.TrimSpace(rule.ID) == "" {
		return errors.New("rule id is required")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is required")
	}

	if !slices.Contains(validCacheModes, rule.Action.Cache.Mode) {
		return fmt.Errorf("unsupported cache mode %q", rule.Action.Cache.Mode)
	}

	if rule.Action.Cache.TTL != "" {
		if rule.Action.Cache.Mode == CacheModeFollowOrigin || rule.Action.Cache.Mode == CacheModeBypass {
			return errors.New("ttl is only supported for force_cache or override_origin")
		}
		if err := validatePositiveDuration(rule.Action.Cache.TTL); err != nil {
			return fmt.Errorf("invalid ttl: %w", err)
		}
	}
	if rule.Action.Cache.StaleIfError != "" {
		if rule.Action.Cache.Mode == CacheModeFollowOrigin || rule.Action.Cache.Mode == CacheModeBypass {
			return errors.New("stale_if_error is only supported for force_cache or override_origin")
		}
		if err := validatePositiveDuration(rule.Action.Cache.StaleIfError); err != nil {
			return fmt.Errorf("invalid stale_if_error: %w", err)
		}
	}

	if rule.Match.Any {
		if !isLast {
			return errors.New("catch-all rule must be the last rule")
		}
		if !rule.System {
			return errors.New("catch-all rule must be a system rule")
		}
		if !rule.Enabled {
			return errors.New("catch-all rule must stay enabled")
		}

		return nil
	}

	if isLast {
		return errors.New("last rule must be the system catch-all rule")
	}

	clauses := rule.Match.Clauses
	if len(clauses) == 0 {
		return errors.New("rule must define at least one match condition")
	}

	for index, clause := range clauses {
		if index == 0 && clause.Logical != "" {
			return errors.New("first match clause cannot define a logical operator")
		}
		if index > 0 && clause.Logical != MatchLogicalAnd && clause.Logical != MatchLogicalOr {
			return fmt.Errorf("match clause %d must use logical %q or %q", index, MatchLogicalAnd, MatchLogicalOr)
		}
		if err := validateMatchClause(clause); err != nil {
			return fmt.Errorf("match clause %d: %w", index, err)
		}
	}

	return nil
}

func validateMatchClause(clause MatchClause) error {
	if strings.TrimSpace(clause.Field) == "" {
		return errors.New("field is required")
	}
	if strings.TrimSpace(clause.Operator) == "" {
		return errors.New("operator is required")
	}

	switch clause.Field {
	case MatchFieldHostname:
		switch clause.Operator {
		case MatchOperatorEquals, MatchOperatorNotEquals, MatchOperatorContains, MatchOperatorNotContains, MatchOperatorStartsWith, MatchOperatorNotStartsWith:
			if strings.TrimSpace(clause.Value) == "" {
				return errors.New("hostname matcher requires a value")
			}
		default:
			return fmt.Errorf("unsupported hostname operator %q", clause.Operator)
		}
	case MatchFieldURIPath:
		switch clause.Operator {
		case MatchOperatorEquals, MatchOperatorNotEquals, MatchOperatorContains, MatchOperatorNotContains, MatchOperatorStartsWith, MatchOperatorNotStartsWith, MatchOperatorMatchesGlob, MatchOperatorNotMatchesGlob:
			if strings.TrimSpace(clause.Value) == "" {
				return errors.New("path matcher requires a value")
			}
		default:
			return fmt.Errorf("unsupported uri_path operator %q", clause.Operator)
		}
	case MatchFieldMethod:
		switch clause.Operator {
		case MatchOperatorEquals, MatchOperatorNotEquals:
			if strings.TrimSpace(clause.Value) == "" {
				return errors.New("method matcher requires a value")
			}
		default:
			return fmt.Errorf("unsupported method operator %q", clause.Operator)
		}
	case MatchFieldRequestHeader:
		if strings.TrimSpace(clause.Name) == "" {
			return errors.New("request header matcher requires a header name")
		}
		switch clause.Operator {
		case MatchOperatorExists, MatchOperatorNotExists:
		case MatchOperatorEquals, MatchOperatorNotEquals, MatchOperatorContains, MatchOperatorNotContains:
			if strings.TrimSpace(clause.Value) == "" {
				return errors.New("request header matcher requires a value")
			}
		default:
			return fmt.Errorf("unsupported request_header operator %q", clause.Operator)
		}
	default:
		return fmt.Errorf("unsupported match field %q", clause.Field)
	}

	return nil
}
