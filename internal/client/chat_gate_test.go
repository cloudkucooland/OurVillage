package client

import "testing"

func TestChatTurnGateSuppressesConcurrentAndBackToBackReplies(t *testing.T) {
	var gate chatTurnGate
	if !gate.TryStart(10, 2) {
		t.Fatal("first response was unexpectedly suppressed")
	}
	if gate.TryStart(10, 2) {
		t.Fatal("concurrent response was allowed")
	}
	gate.Finish()
	if gate.TryStart(11, 2) {
		t.Fatal("response inside cooldown was allowed")
	}
	if !gate.TryStart(12, 2) {
		t.Fatal("response after cooldown was suppressed")
	}
	gate.Finish()
}
