// Package rule is to control the audit rule behaviors
package rule

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/xtls/xray-core/common/errors"
)

// detectResultSet is a thread-safe set of api.DetectResult values.
// api.DetectResult must be comparable (no slice/map fields) to be used as a map key.
type detectResultSet struct {
	mu      sync.Mutex
	entries map[api.DetectResult]struct{}
}

func newDetectResultSet() *detectResultSet {
	return &detectResultSet{entries: make(map[api.DetectResult]struct{})}
}

// add inserts r and reports whether it was a new entry.
func (s *detectResultSet) add(r api.DetectResult) bool {
	s.mu.Lock()
	_, exists := s.entries[r]
	if !exists {
		s.entries[r] = struct{}{}
	}
	s.mu.Unlock()
	return !exists
}

// drainAll removes and returns all entries.
func (s *detectResultSet) drainAll() []api.DetectResult {
	s.mu.Lock()
	out := make([]api.DetectResult, 0, len(s.entries))
	for r := range s.entries {
		out = append(out, r)
	}
	s.entries = make(map[api.DetectResult]struct{})
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
		if !reflect.DeepEqual(oldRuleList, newRuleList) {
			r.inboundRule.Store(tag, newRuleList)
		}
	}
	return nil
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

	result := api.DetectResult{UID: uid, RuleID: hitRuleID}

	// Fast path: set already exists for this tag – no allocation needed.
	if sv, ok := r.inboundDetectResult.Load(tag); ok {
		sv.(*detectResultSet).add(result)
		return true
	}
	// Slow path: first hit for this tag, create and race to store.
	newSet := newDetectResultSet()
	actual, _ := r.inboundDetectResult.LoadOrStore(tag, newSet)
	actual.(*detectResultSet).add(result)

	return true
}
