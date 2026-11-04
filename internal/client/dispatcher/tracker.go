package dispatcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrIntentTimeout is returned when the server does not respond within the deadline.
	ErrIntentTimeout = errors.New("intent execution timed out")

	// ErrTrackerClosed is returned when the correlation tracker is shut down.
	ErrTrackerClosed = errors.New("correlation tracker is closed")
)

// ExecutionResult represents the outcome received from the server for an intent.
type ExecutionResult struct {
	IntentID   string        `json:"intent_id"`
	Status     string        `json:"status"` // "SUCCESS" or "ERROR"
	ErrorCode  string        `json:"error_code,omitempty"`
	Message    string        `json:"message"`
	Duration   time.Duration `json:"duration"`
	ReceivedAt time.Time     `json:"received_at"`
	Err        error         `json:"error,omitempty"`
}

type pendingIntent struct {
	intentID  string
	startedAt time.Time
	ch        chan ExecutionResult
	timer     *time.Timer
}

// CorrelationTracker matches asynchronously dispatched intent_ids with incoming server direct execution results.
type CorrelationTracker struct {
	mu      sync.Mutex
	closed  bool
	pending map[string]*pendingIntent
	history []ExecutionResult
	maxHist int
}

// NewCorrelationTracker creates a correlation tracker with a bounded history buffer.
func NewCorrelationTracker(historyLimit int) *CorrelationTracker {
	if historyLimit <= 0 {
		historyLimit = 50
	}
	return &CorrelationTracker{
		pending: make(map[string]*pendingIntent),
		history: make([]ExecutionResult, 0, historyLimit),
		maxHist: historyLimit,
	}
}

// Register registers an intent for asynchronous response correlation.
// It returns a channel that emits the ExecutionResult upon server response or timeout.
func (t *CorrelationTracker) Register(intentID string, timeout time.Duration) <-chan ExecutionResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	resultCh := make(chan ExecutionResult, 1)
	if t.closed {
		resultCh <- ExecutionResult{IntentID: intentID, Err: ErrTrackerClosed}
		return resultCh
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	startedAt := time.Now()
	p := &pendingIntent{
		intentID:  intentID,
		startedAt: startedAt,
		ch:        resultCh,
	}

	// Schedule timeout eviction
	p.timer = time.AfterFunc(timeout, func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		if curr, ok := t.pending[intentID]; ok && curr == p {
			delete(t.pending, intentID)
			res := ExecutionResult{
				IntentID:   intentID,
				Status:     "TIMEOUT",
				Message:    fmt.Sprintf("Intent %s timed out after %v without server confirmation", intentID, timeout),
				Duration:   time.Since(startedAt),
				ReceivedAt: time.Now(),
				Err:        ErrIntentTimeout,
			}
			t.recordHistory(res)
			curr.ch <- res
		}
	})

	t.pending[intentID] = p
	return resultCh
}

// Resolve processes an incoming direct message payload from world/agents/{agent_id}/direct,
// matching the intent_id against pending trackers.
func (t *CorrelationTracker) Resolve(raw []byte) (ExecutionResult, bool) {
	var direct struct {
		IntentID  string `json:"intent_id"`
		Status    string `json:"status"`
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
	}

	if err := json.Unmarshal(raw, &direct); err != nil {
		return ExecutionResult{Err: fmt.Errorf("invalid direct json: %w", err)}, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	p, found := t.pending[direct.IntentID]
	if !found {
		// Not a tracked pending intent or already resolved/timed out
		res := ExecutionResult{
			IntentID:   direct.IntentID,
			Status:     direct.Status,
			ErrorCode:  direct.ErrorCode,
			Message:    direct.Message,
			ReceivedAt: time.Now(),
		}
		t.recordHistory(res)
		return res, false
	}

	delete(t.pending, direct.IntentID)
	p.timer.Stop()

	res := ExecutionResult{
		IntentID:   direct.IntentID,
		Status:     direct.Status,
		ErrorCode:  direct.ErrorCode,
		Message:    direct.Message,
		Duration:   time.Since(p.startedAt),
		ReceivedAt: time.Now(),
	}
	t.recordHistory(res)

	p.ch <- res
	return res, true
}

func (t *CorrelationTracker) recordHistory(res ExecutionResult) {
	if len(t.history) >= t.maxHist {
		t.history = t.history[1:]
	}
	t.history = append(t.history, res)
}

// PendingCount returns the number of currently in-flight intents awaiting server acknowledgment.
func (t *CorrelationTracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// History returns a copy of recent execution results in chronological order.
func (t *CorrelationTracker) History() []ExecutionResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	copied := make([]ExecutionResult, len(t.history))
	copy(copied, t.history)
	return copied
}

// Close cancels all pending intent trackers and releases timers.
func (t *CorrelationTracker) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true
	for id, p := range t.pending {
		p.timer.Stop()
		p.ch <- ExecutionResult{IntentID: id, Err: ErrTrackerClosed}
	}
	t.pending = make(map[string]*pendingIntent)
	return nil
}
