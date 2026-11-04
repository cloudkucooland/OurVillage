package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// IntentDispatcher coordinates intent validation, MQTT publishing, and asynchronous response correlation.
type IntentDispatcher struct {
	agentID string
	client  mqtt.Client
	tracker *CorrelationTracker
}

// NewIntentDispatcher creates a new IntentDispatcher.
func NewIntentDispatcher(agentID string, client mqtt.Client, tracker *CorrelationTracker) *IntentDispatcher {
	if tracker == nil {
		tracker = NewCorrelationTracker(50)
	}
	return &IntentDispatcher{
		agentID: agentID,
		client:  client,
		tracker: tracker,
	}
}

// Tracker returns the underlying CorrelationTracker.
func (d *IntentDispatcher) Tracker() *CorrelationTracker {
	return d.tracker
}

// HandleDirectResponse forwards direct server responses from world/agents/{agent_id}/direct to the tracker.
func (d *IntentDispatcher) HandleDirectResponse(payload []byte) (ExecutionResult, bool) {
	return d.tracker.Resolve(payload)
}

// Dispatch validates, tracks, and publishes an intent over MQTT, returning the correlated server response.
func (d *IntentDispatcher) Dispatch(ctx context.Context, intent Intent, timeout time.Duration) (ExecutionResult, error) {
	validIntent, err := ValidateIntent(intent)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("intent validation failed: %w", err)
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	resultCh := d.tracker.Register(validIntent.IntentID, timeout)

	intentBytes, err := json.Marshal(validIntent)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to marshal intent: %w", err)
	}

	topic := fmt.Sprintf("world/agents/%s/intent", d.agentID)
	token := d.client.Publish(topic, 0, false, intentBytes)
	token.Wait()
	if token.Error() != nil {
		return ExecutionResult{}, fmt.Errorf("failed to publish intent to %s: %w", topic, token.Error())
	}

	select {
	case <-ctx.Done():
		return ExecutionResult{IntentID: validIntent.IntentID, Err: ctx.Err()}, ctx.Err()
	case res := <-resultCh:
		if res.Err != nil {
			return res, res.Err
		}
		return res, nil
	}
}
