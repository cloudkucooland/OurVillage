package tui

import (
	"context"
	"testing"

	"agentmud/internal/client/spatial"
)

func TestTUIParamTuning(t *testing.T) {
	ctx := context.Background()
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{
		AgentID: "eldon_millwright",
	})

	prob := 0.85
	freq := 30
	temp := 0.7
	debounce := 2000

	model := NewTUIModel(ctx, "eldon_millwright", nil, nil, ctxMgr, nil, nil, &prob, &freq, &temp, &debounce)

	// Test /set slash command tuning
	model.handleSlashCommand("/set prob 0.5")
	if prob != 0.5 {
		t.Errorf("expected prob to be 0.5, got %f", prob)
	}

	model.handleSlashCommand("/set freq 60")
	if freq != 60 {
		t.Errorf("expected freq to be 60, got %d", freq)
	}

	model.handleSlashCommand("/set temp 0.2")
	if temp != 0.2 {
		t.Errorf("expected temp to be 0.2, got %f", temp)
	}

	model.handleSlashCommand("/set debounce 1000")
	if debounce != 1000 {
		t.Errorf("expected debounce to be 1000, got %d", debounce)
	}
}
