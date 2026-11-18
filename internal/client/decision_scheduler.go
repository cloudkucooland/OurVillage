package client

import (
	"sort"
	"sync"
)

type decisionReason string

const (
	decisionIdleInterval decisionReason = "idle_interval"
	decisionActionDone   decisionReason = "action_completed"
	decisionArrival      decisionReason = "arrived"
	decisionNeedChange   decisionReason = "need_threshold"
)

// decisionScheduler coalesces world changes into one pending cognition turn.
// It stores reasons rather than prompts so a turn always sees current state.
type decisionScheduler struct {
	mu      sync.Mutex
	pending map[decisionReason]struct{}
	running bool
}

func newDecisionScheduler() *decisionScheduler {
	return &decisionScheduler{pending: make(map[decisionReason]struct{})}
}

func (s *decisionScheduler) Mark(reason decisionReason) {
	s.mu.Lock()
	s.pending[reason] = struct{}{}
	s.mu.Unlock()
}

func (s *decisionScheduler) Begin() bool {
	_, ok := s.BeginWithReasons()
	return ok
}

func (s *decisionScheduler) BeginWithReasons() ([]decisionReason, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running || len(s.pending) == 0 {
		return nil, false
	}
	s.running = true
	reasons := make([]decisionReason, 0, len(s.pending))
	for reason := range s.pending {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool { return reasons[i] < reasons[j] })
	clear(s.pending)
	return reasons, true
}

func reasonStrings(reasons []decisionReason) []string {
	result := make([]string, len(reasons))
	for i, reason := range reasons {
		result[i] = string(reason)
	}
	return result
}

func (s *decisionScheduler) Finish() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *decisionScheduler) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

func (s *decisionScheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func crossedNeedThreshold(oldValue, newValue int) bool {
	for _, threshold := range [...]int{40, 20} {
		if oldValue >= threshold && newValue < threshold {
			return true
		}
	}
	return false
}

func shouldTriggerAfterIntent(action string) bool {
	switch action {
	case "", "CHAT", "LOOK", "STATUS":
		return false
	default:
		return true
	}
}
