// Package evaluate implements the flag evaluation engine.
package evaluate

import (
	"encoding/json"
	"hash/fnv"
)

type Condition struct {
	Attr  string          `json:"attr"`
	Op    string          `json:"op"` // eq | neq | in
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
	default:
		// Unknown operator: fail closed.
		return false
	}
}
