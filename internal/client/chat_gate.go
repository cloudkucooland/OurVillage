package client

import "sync"

// chatTurnGate prevents a busy conversation from causing overlapping or
// back-to-back autonomous response turns.
type chatTurnGate struct {
	mu             sync.Mutex
	inFlight       bool
	hasResponded   bool
	lastResponseAt uint64
}

func (g *chatTurnGate) TryStart(tick, cooldownTicks uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight {
		return false
	}
	if g.hasResponded && tick < g.lastResponseAt+cooldownTicks {
		return false
	}
	g.inFlight = true
	g.hasResponded = true
	g.lastResponseAt = tick
	return true
}

func (g *chatTurnGate) Finish() {
	g.mu.Lock()
	g.inFlight = false
	g.mu.Unlock()
}
