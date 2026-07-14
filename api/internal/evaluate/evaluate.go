// Package evaluate implements the flag evaluation engine.
package evaluate

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"
)

type Condition struct {
	Attr  string          `json:"attr"`
	Op    string          `json:"op"` // eq | neq | in | semver_gte
	Value json.RawMessage `json:"value"`
}

type Rule struct {
	FlagKey        string
	Enabled        bool
	Archived       bool
	RolloutPercent int
	Conditions     []Condition
}

type Context struct {
	UserID     string
	Attributes map[string]string
}

// Evaluate resolves a single flag for a user. It must be deterministic: the
// same user always lands in the same rollout bucket, and raising the percent
// only ever adds users to the enabled side.
func Evaluate(r Rule, ctx Context) bool {
	if r.Archived || !r.Enabled {
		return false
	}
	for _, c := range r.Conditions {
		if !matchCondition(c, ctx.Attributes) {
			return false
		}
	}
	if r.RolloutPercent >= 100 {
		return true
	}
	if r.RolloutPercent <= 0 {
		return false
	}
	return bucket(r.FlagKey, ctx.UserID) < r.RolloutPercent
}

// bucket hashes flag key + user id into [0,100). Keyed by flag so a user is
// not stuck on the same side of every rollout in the project.
func bucket(flagKey, userID string) int {
	h := fnv.New32a()
	h.Write([]byte(flagKey))
	h.Write([]byte{':'})
	h.Write([]byte(userID))
	return int(h.Sum32() % 100)
}

func matchCondition(c Condition, attrs map[string]string) bool {
	got, present := attrs[c.Attr]
	switch c.Op {
	case "eq":
		var want string
		if json.Unmarshal(c.Value, &want) != nil {
			return false
		}
		return present && got == want
	case "neq":
		var want string
		if json.Unmarshal(c.Value, &want) != nil {
			return false
		}
		return !present || got != want
	case "in":
		var want []string
		if json.Unmarshal(c.Value, &want) != nil {
			return false
		}
		if !present {
			return false
		}
		for _, w := range want {
			if got == w {
				return true
			}
		}
		return false
	case "semver_gte":
		var want string
		if json.Unmarshal(c.Value, &want) != nil {
			return false
		}
		return present && semverGTE(got, want)
	default:
		// Unknown operator: fail closed.
		return false
	}
}

// semverGTE reports a >= b for dotted numeric versions ("2.1.0", "v2.1").
// Missing components count as 0; anything unparsable fails closed. This is
// deliberately not full SemVer (no pre-release precedence): app stores ship
// plain MAJOR.MINOR.PATCH, and fail-closed beats surprising matches.
func semverGTE(a, b string) bool {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return false
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return true
}

func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Drop build/pre-release suffixes: "2.1.0-beta.1" compares as 2.1.0.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
