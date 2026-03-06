package model

import "testing"

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
