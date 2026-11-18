package client

import "testing"

func TestDecisionSchedulerCoalescesEventsAndRetainsEventsWhileRunning(t *testing.T) {
	s := newDecisionScheduler()
	s.Mark(decisionActionDone)
	s.Mark(decisionArrival)
	if !s.Begin() {
		t.Fatal("Begin() = false with pending events")
	}
	if s.Begin() {
		t.Fatal("Begin() started a second decision while one was running")
	}
	s.Mark(decisionNeedChange)
	s.Finish()
	if !s.Pending() {
		t.Fatal("event marked during inference was lost")
	}
	if !s.Begin() {
		t.Fatal("Begin() = false after finishing with a pending event")
	}
	s.Finish()
	if s.Pending() {
		t.Fatal("events were not cleared when a decision began")
	}
}

func TestCrossedNeedThreshold(t *testing.T) {
	for _, tc := range []struct {
		old, next int
		want      bool
	}{
		{50, 40, false},
		{41, 39, true},
		{25, 19, true},
		{19, 18, false},
		{10, 45, false},
	} {
		if got := crossedNeedThreshold(tc.old, tc.next); got != tc.want {
			t.Errorf("crossedNeedThreshold(%d, %d) = %v, want %v", tc.old, tc.next, got, tc.want)
		}
	}
}

func TestShouldTriggerAfterIntentExcludesConversationAndObservation(t *testing.T) {
	for _, action := range []string{"CHAT", "LOOK", "STATUS", ""} {
		if shouldTriggerAfterIntent(action) {
			t.Errorf("shouldTriggerAfterIntent(%q) = true, want false", action)
		}
	}
	for _, action := range []string{"EAT", "REST", "MOVE", "FORAGE"} {
		if !shouldTriggerAfterIntent(action) {
			t.Errorf("shouldTriggerAfterIntent(%q) = false, want true", action)
		}
	}
}
