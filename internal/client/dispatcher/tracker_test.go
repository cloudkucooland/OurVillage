package dispatcher

import (
	"testing"
	"time"
)

func TestCorrelationTracker_ResolveSuccess(t *testing.T) {
	tracker := NewCorrelationTracker(10)
	defer tracker.Close()

	intentID := "req_test_123"
	resultCh := tracker.Register(intentID, 2*time.Second)

	if tracker.PendingCount() != 1 {
		t.Fatalf("expected 1 pending intent, got %d", tracker.PendingCount())
	}

	serverPayload := []byte(`{"intent_id":"req_test_123","status":"SUCCESS","message":"Moved to Town Mill"}`)
	res, matched := tracker.Resolve(serverPayload)

	if !matched {
		t.Fatal("expected Resolve to return matched=true")
	}
	if res.IntentID != intentID {
		t.Errorf("expected intent_id %s, got %s", intentID, res.IntentID)
	}
	if res.Status != "SUCCESS" {
		t.Errorf("expected status SUCCESS, got %s", res.Status)
	}

	select {
	case chRes := <-resultCh:
		if chRes.Status != "SUCCESS" {
			t.Errorf("channel received unexpected status: %s", chRes.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for resultCh emission")
	}

	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending intents after resolution, got %d", tracker.PendingCount())
	}

	history := tracker.History()
	if len(history) != 1 || history[0].IntentID != intentID {
		t.Errorf("unexpected history: %+v", history)
	}
}

func TestCorrelationTracker_Timeout(t *testing.T) {
	tracker := NewCorrelationTracker(10)
	defer tracker.Close()

	intentID := "req_timeout_test"
	resultCh := tracker.Register(intentID, 50*time.Millisecond)

	select {
	case res := <-resultCh:
		if res.Err != ErrIntentTimeout {
			t.Errorf("expected ErrIntentTimeout, got %v", res.Err)
		}
		if res.Status != "TIMEOUT" {
			t.Errorf("expected status TIMEOUT, got %s", res.Status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for tracker timeout emission")
	}

	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending intents after timeout, got %d", tracker.PendingCount())
	}
}

func TestCorrelationTracker_Close(t *testing.T) {
	tracker := NewCorrelationTracker(10)
	resultCh := tracker.Register("req_close_test", 5*time.Second)

	_ = tracker.Close()

	select {
	case res := <-resultCh:
		if res.Err != ErrTrackerClosed {
			t.Errorf("expected ErrTrackerClosed, got %v", res.Err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for tracker close notification")
	}
}
