package model

import (
	"strings"
	"testing"
)

func TestValidateMatchClauseRejectsListOperators(t *testing.T) {
	clause := MatchClause{
		Field:    MatchFieldHostname,
		Operator: "is_one_of",
		Value:    "example.com",
	}

	if err := validateMatchClause(clause); err == nil {
		t.Fatalf("expected hostname list operator to be rejected")
	}
}

func TestValidateSiteUpstreamHostMode(t *testing.T) {
	base := Site{
		ID:      "site-1",
		Name:    "Site 1",
		Enabled: true,
		Hosts:   []string{"cdn.example.com"},
		Upstream: Upstream{
			URL: "https://origin.example.com",
		},
		Rules: []Rule{NewDefaultRule()},
	}

	t.Run("defaults to follow_origin", func(t *testing.T) {
		if err := validateSite(base); err != nil {
			t.Fatalf("expected default upstream host mode to validate: %v", err)
		}
	})

	t.Run("custom requires host", func(t *testing.T) {
		site := base
		site.Upstream.HostMode = UpstreamHostModeCustom

		err := validateSite(site)
		if err == nil || !strings.Contains(err.Error(), "requires host") {
			t.Fatalf("expected custom mode to require host, got %v", err)
		}
	})

	t.Run("custom accepts explicit host", func(t *testing.T) {
		site := base
		site.Upstream.HostMode = UpstreamHostModeCustom
		site.Upstream.Host = "a.com"

		if err := validateSite(site); err != nil {
			t.Fatalf("expected custom upstream host to validate: %v", err)
		}
	})

	t.Run("follow_request rejects custom host", func(t *testing.T) {
		site := base
		site.Upstream.HostMode = UpstreamHostModeFollowRequest
		site.Upstream.Host = "a.com"

		err := validateSite(site)
		if err == nil || !strings.Contains(err.Error(), "only supported when host mode is custom") {
			t.Fatalf("expected follow_request to reject explicit host, got %v", err)
		}
	})

	t.Run("rejects upstream path prefix", func(t *testing.T) {
		site := base
		site.Upstream.URL = "https://origin.example.com/base"

		err := validateSite(site)
		if err == nil || !strings.Contains(err.Error(), "path prefix") {
			t.Fatalf("expected upstream path prefix to be rejected, got %v", err)
		}
	})

	t.Run("rejects upstream query", func(t *testing.T) {
		site := base
		site.Upstream.URL = "https://origin.example.com?preview=1"

		err := validateSite(site)
		if err == nil || !strings.Contains(err.Error(), "query parameters") {
			t.Fatalf("expected upstream query to be rejected, got %v", err)
		}
	})

	t.Run("rejects upstream fragment", func(t *testing.T) {
		site := base
		site.Upstream.URL = "https://origin.example.com#frag"

		err := validateSite(site)
		if err == nil || !strings.Contains(err.Error(), "fragment") {
			t.Fatalf("expected upstream fragment to be rejected, got %v", err)
		}
	})
}

func TestValidateRuleOptimisticMode(t *testing.T) {
	rule := Rule{
		ID:      "rule-1",
		Name:    "Rule 1",
		Enabled: true,
		Match: MatchSpec{
			Clauses: []MatchClause{
				{
					Field:    MatchFieldHostname,
					Operator: MatchOperatorEquals,
					Value:    "example.com",
				},
			},
		},
		Action: RuleAction{
			Cache: CacheAction{
				Mode:       CacheModeBypass,
				Optimistic: true,
			},
		},
	}

	err := validateRule(rule, false)
	if err == nil || !strings.Contains(err.Error(), "optimistic is not supported for bypass") {
		t.Fatalf("expected bypass to reject optimistic mode, got %v", err)
	}
}
