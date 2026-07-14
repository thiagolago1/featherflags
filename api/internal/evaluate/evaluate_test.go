package evaluate

import (
	"encoding/json"
	"fmt"
	"testing"
)

func ctx(userID string, attrs map[string]string) Context {
	return Context{UserID: userID, Attributes: attrs}
}

func TestDisabledAndArchived(t *testing.T) {
	if Evaluate(Rule{FlagKey: "f", Enabled: false, RolloutPercent: 100}, ctx("u1", nil)) {
		t.Error("disabled flag must be false")
	}
	if Evaluate(Rule{FlagKey: "f", Enabled: true, Archived: true, RolloutPercent: 100}, ctx("u1", nil)) {
		t.Error("archived flag must be false")
	}
}

func TestFullRollout(t *testing.T) {
	if !Evaluate(Rule{FlagKey: "f", Enabled: true, RolloutPercent: 100}, ctx("u1", nil)) {
		t.Error("enabled flag at 100% must be true")
	}
	if Evaluate(Rule{FlagKey: "f", Enabled: true, RolloutPercent: 0}, ctx("u1", nil)) {
		t.Error("0% rollout must be false")
	}
}

func TestRolloutIsDeterministic(t *testing.T) {
	r := Rule{FlagKey: "checkout-v2", Enabled: true, RolloutPercent: 40}
	first := Evaluate(r, ctx("user-123", nil))
	for i := 0; i < 100; i++ {
		if Evaluate(r, ctx("user-123", nil)) != first {
			t.Fatal("same user flipped between evaluations")
		}
	}
}

func TestRolloutIsMonotonic(t *testing.T) {
	// Raising the percent must never remove a user who already had the flag.
	users := make([]string, 500)
	for i := range users {
		users[i] = "user-" + string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('A'+i%26))
	}
	for pct := 10; pct <= 90; pct += 20 {
		lower := Rule{FlagKey: "f", Enabled: true, RolloutPercent: pct}
		higher := Rule{FlagKey: "f", Enabled: true, RolloutPercent: pct + 10}
		for _, u := range users {
			if Evaluate(lower, ctx(u, nil)) && !Evaluate(higher, ctx(u, nil)) {
				t.Fatalf("user %s lost flag when rollout went %d%% -> %d%%", u, pct, pct+10)
			}
		}
	}
}

func TestRolloutDistribution(t *testing.T) {
	r := Rule{FlagKey: "f", Enabled: true, RolloutPercent: 50}
	on := 0
	const n = 10000
	for i := 0; i < n; i++ {
		if Evaluate(r, ctx(fmt.Sprintf("user-%d", i), nil)) {
			on++
		}
	}
	// Loose bounds: just catch a broken hash, not enforce perfect uniformity.
	if on < n*35/100 || on > n*65/100 {
		t.Errorf("50%% rollout enabled %d of %d users", on, n)
	}
}

func TestConditions(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	cases := []struct {
		name  string
		cond  Condition
		attrs map[string]string
		want  bool
	}{
		{"eq match", Condition{Attr: "plan", Op: "eq", Value: raw(`"premium"`)}, map[string]string{"plan": "premium"}, true},
		{"eq mismatch", Condition{Attr: "plan", Op: "eq", Value: raw(`"premium"`)}, map[string]string{"plan": "free"}, false},
		{"eq missing attr", Condition{Attr: "plan", Op: "eq", Value: raw(`"premium"`)}, nil, false},
		{"neq mismatch", Condition{Attr: "plan", Op: "neq", Value: raw(`"free"`)}, map[string]string{"plan": "premium"}, true},
		{"neq missing attr passes", Condition{Attr: "plan", Op: "neq", Value: raw(`"free"`)}, nil, true},
		{"in match", Condition{Attr: "plan", Op: "in", Value: raw(`["pro","premium"]`)}, map[string]string{"plan": "pro"}, true},
		{"in miss", Condition{Attr: "plan", Op: "in", Value: raw(`["pro","premium"]`)}, map[string]string{"plan": "free"}, false},
		{"unknown op fails closed", Condition{Attr: "plan", Op: "gt", Value: raw(`"1"`)}, map[string]string{"plan": "2"}, false},
		{"semver equal", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.1.0"`)}, map[string]string{"appVersion": "2.1.0"}, true},
		{"semver newer", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.1.0"`)}, map[string]string{"appVersion": "2.2"}, true},
		{"semver older", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.0"`)}, map[string]string{"appVersion": "1.9.9"}, false},
		{"semver v-prefix and prerelease", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.1.0"`)}, map[string]string{"appVersion": "v2.1.0-beta.1"}, true},
		{"semver major beats minor.patch", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"9.9.9"`)}, map[string]string{"appVersion": "10.0.0"}, true},
		{"semver garbage fails closed", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.1.0"`)}, map[string]string{"appVersion": "latest"}, false},
		{"semver missing attr", Condition{Attr: "appVersion", Op: "semver_gte", Value: raw(`"2.1.0"`)}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Rule{FlagKey: "f", Enabled: true, RolloutPercent: 100, Conditions: []Condition{tc.cond}}
			if got := Evaluate(r, ctx("u1", tc.attrs)); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
