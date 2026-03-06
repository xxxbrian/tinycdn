package runtime

import (
	"net/http/httptest"
	"testing"

	"tinycdn/internal/model"
)

func TestMatchesRuleClauses(t *testing.T) {
	t.Run("and clauses", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://cdn.example.com/api/users", nil)
		req.Host = "api.example.com"

		match := model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldHostname,
					Operator: model.MatchOperatorEquals,
					Value:    "api.example.com",
				},
				{
					Logical:  model.MatchLogicalAnd,
					Field:    model.MatchFieldURIPath,
					Operator: model.MatchOperatorStartsWith,
					Value:    "/api/",
				},
			},
		}

		if !matchesRule(match, req) {
			t.Fatalf("expected AND clauses to match request")
		}
	})

	t.Run("or clauses", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://cdn.example.com/assets/app.js", nil)
		req.Host = "static.example.com"

		match := model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldHostname,
					Operator: model.MatchOperatorEquals,
					Value:    "api.example.com",
				},
				{
					Logical:  model.MatchLogicalOr,
					Field:    model.MatchFieldURIPath,
					Operator: model.MatchOperatorMatchesGlob,
					Value:    "/assets/**",
				},
			},
		}

		if !matchesRule(match, req) {
			t.Fatalf("expected OR clauses to match request")
		}
	})

	t.Run("and has higher precedence than or", func(t *testing.T) {
		req := httptest.NewRequest("POST", "https://cdn.example.com/assets/app.js", nil)
		req.Host = "static.example.com"

		match := model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldHostname,
					Operator: model.MatchOperatorEquals,
					Value:    "static.example.com",
				},
				{
					Logical:  model.MatchLogicalOr,
					Field:    model.MatchFieldURIPath,
					Operator: model.MatchOperatorStartsWith,
					Value:    "/api/",
				},
				{
					Logical:  model.MatchLogicalAnd,
					Field:    model.MatchFieldMethod,
					Operator: model.MatchOperatorEquals,
					Value:    "GET",
				},
			},
		}

		if !matchesRule(match, req) {
			t.Fatalf("expected first OR group to win before later AND clause")
		}
	})

	t.Run("header exists and method equals", func(t *testing.T) {
		req := httptest.NewRequest("POST", "https://cdn.example.com/login", nil)
		req.Host = "app.example.com"
		req.Header.Set("Authorization", "Bearer token")

		match := model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldMethod,
					Operator: model.MatchOperatorEquals,
					Value:    "POST",
				},
				{
					Logical:  model.MatchLogicalAnd,
					Field:    model.MatchFieldRequestHeader,
					Operator: model.MatchOperatorExists,
					Name:     "Authorization",
				},
			},
		}

		if !matchesRule(match, req) {
			t.Fatalf("expected method/header clauses to match request")
		}
	})

	t.Run("not contains", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://cdn.example.com/private/report", nil)
		req.Host = "app.example.com"

		match := model.MatchSpec{
			Clauses: []model.MatchClause{
				{
					Field:    model.MatchFieldURIPath,
					Operator: model.MatchOperatorNotContains,
					Value:    "/private/",
				},
			},
		}

		if matchesRule(match, req) {
			t.Fatalf("expected not_contains clause to reject request")
		}
	})
}
