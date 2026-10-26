// Package rule is to control the audit rule behaviors
package rule

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/xtls/xray-core/common/errors"
)

type detectKey struct {
	UID    int
	RuleID int
}

// detectResultSet is a thread-safe counter of (uid, rule_id) hits.
type detectResultSet struct {
	mu      sync.Mutex
	entries map[detectKey]int
}

func newDetectResultSet() *detectResultSet {
	return &detectResultSet{entries: make(map[detectKey]int)}
}

func (s *detectResultSet) add(uid, ruleID int) {
	s.mu.Lock()
	s.entries[detectKey{UID: uid, RuleID: ruleID}]++
	s.mu.Unlock()
}

// drainAll removes and returns all entries with counts.
func (s *detectResultSet) drainAll() []api.DetectResult {
	s.mu.Lock()
	out := make([]api.DetectResult, 0, len(s.entries))
	for k, n := range s.entries {
		out = append(out, api.DetectResult{UID: k.UID, RuleID: k.RuleID, Count: n})
	}
	s.entries = make(map[detectKey]int)
	s.mu.Unlock()
	return out
}

// RuleManager manages per-inbound audit rules and detection results.
type RuleManager struct {
	inboundRule         sync.Map // key: tag (string) → []api.DetectRule  (immutable slices, replaced atomically)
	inboundDetectResult sync.Map // key: tag (string) → *detectResultSet
}

func New() *RuleManager {
	return &RuleManager{}
}

// UpdateRule atomically replaces the rule list for a tag.
// A full replacement is always safe: Detect() loads the slice atomically.
func (r *RuleManager) UpdateRule(tag string, newRuleList []api.DetectRule) error {
	if v, loaded := r.inboundRule.LoadOrStore(tag, newRuleList); loaded {
		oldRuleList := v.([]api.DetectRule)
		if !detectRulesEqual(oldRuleList, newRuleList) {
			r.inboundRule.Store(tag, newRuleList)
		}
	}
	return nil
}

// detectRulesEqual compares rule ID and pattern source text only — not the
// internal *regexp.Regexp state (DeepEqual is expensive and unstable).
func detectRulesEqual(a, b []api.DetectRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
		as, bs := "", ""
		if a[i].Pattern != nil {
			as = a[i].Pattern.String()
		}
		if b[i].Pattern != nil {
			bs = b[i].Pattern.String()
		}
		if as != bs {
			return false
		}
	}
	return true
}

// GetDetectResult atomically drains and returns all detection results for tag.
func (r *RuleManager) GetDetectResult(tag string) (*[]api.DetectResult, error) {
	if v, ok := r.inboundDetectResult.Load(tag); ok {
		results := v.(*detectResultSet).drainAll()
		return &results, nil
	}
	empty := []api.DetectResult{}
	return &empty, nil
}

// DeleteRule removes all rule data for a tag.
func (r *RuleManager) DeleteRule(tag string) {
	r.inboundRule.Delete(tag)
	r.inboundDetectResult.Delete(tag)
}

// Detect checks destination against the rule list for tag.
// It is safe to call concurrently from many goroutines.
func (r *RuleManager) Detect(tag string, destination string, email string) bool {
	v, ok := r.inboundRule.Load(tag)
	if !ok {
		return false
	}
	ruleList := v.([]api.DetectRule) // immutable snapshot

	hitRuleID := -1
	for _, rule := range ruleList {
		if rule.Pattern != nil && rule.Pattern.Match([]byte(destination)) {
			hitRuleID = rule.ID
			break
		}
	}
	if hitRuleID == -1 {
		return false
	}

	// Parse UID from the email tag ("inboundTag|email|uid").
	parts := strings.Split(email, "|")
	uid, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		errors.LogDebug(context.Background(),
			fmt.Sprintf("Record illegal behavior failed! Cannot parse uid from: %s", email))
		return true // still reject, just can't record
	}

	// Fast path: set already exists for this tag – no allocation needed.
	if sv, ok := r.inboundDetectResult.Load(tag); ok {
		sv.(*detectResultSet).add(uid, hitRuleID)
		return true
	}
	// Slow path: first hit for this tag, create and race to store.
	newSet := newDetectResultSet()
	actual, _ := r.inboundDetectResult.LoadOrStore(tag, newSet)
	actual.(*detectResultSet).add(uid, hitRuleID)

	return true
}
