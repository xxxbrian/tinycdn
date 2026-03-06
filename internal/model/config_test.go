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
}
