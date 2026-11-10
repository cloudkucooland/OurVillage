package tui

import (
	"context"
	"strings"
	"testing"

	"agentmud/internal/client/memory"
	"agentmud/internal/client/spatial"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHeaderFitsTerminalWidth(t *testing.T) {
	status := "Agent: Luke the Farmer | Location: Village Field (village_field) | Gold: 10 | Energy: 48 | Health: 100 | Hunger: 51 | Status: IDLE | Time: 04:00 | Weather: clear"
	for _, width := range []int{80, 120, 160} {
		header := renderHeader("AgentMUD Client TUI", status, width)
		for i, line := range strings.Split(header, "\n") {
			if lineWidth := lipgloss.Width(line); lineWidth > width {
				t.Errorf("header line %d at width %d is %d cells wide", i+1, width, lineWidth)
			}
		}
	}
}

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

func TestSynthesisStatusAppearsInTUI(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)
	statuses := make(chan string, 1)
	statuses <- "Memory synthesis: reflecting on mira — started"
	model.SetSynthesisStatusChannel(statuses)

	updated, _ := model.Update(synthesisStatusMsg(<-statuses))
	model = updated.(TUIModel)
	if !strings.Contains(model.View(), "Memory synthesis: reflecting on mira") {
		t.Fatal("TUI does not display background synthesis status")
	}
}

func TestSelfIdentityTabLoadsAndDisplaysPersistedMemory(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)
	model.SetSelfIdentityProvider(func() ([]memory.IdentityTrait, []memory.SelfReflection, error) {
		return []memory.IdentityTrait{{
				Key: "craftsmanship", Value: "I value patient, durable work.", SalienceScore: 8, UpdatedAtTick: 41,
			}}, []memory.SelfReflection{{
				Tick: 42, ReflectionText: "I felt content repairing the old mill.",
			}}, nil
	})

	var updated tea.Model
	var cmd tea.Cmd
	for i := 0; i < 3; i++ {
		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(TUIModel)
	}
	if model.tab != 3 {
		t.Fatalf("tab after three switches = %d, want self identity", model.tab)
	}
	if cmd == nil {
		t.Fatal("selecting Self Identity did not request persisted data")
	}
	loaded := cmd()
	updated, _ = model.Update(loaded)
	model = updated.(TUIModel)
	view := model.View()
	for _, expected := range []string{"Self Identity", "craftsmanship", "patient, durable work", "Tick 42", "old mill"} {
		if !strings.Contains(view, expected) {
			t.Errorf("Self Identity view missing %q", expected)
		}
	}
}

func TestProjectsTabLoadsAndDisplaysProjects(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)
	model.SetProjectProvider(func() ([]memory.PersonalProject, error) {
		return []memory.PersonalProject{{
			ID:         "restore-mill",
			Title:      "Restore the Old Mill",
			Motivation: "Ensure grinding capacity for the village",
			Status:     "active",
			Progress:   "Gathered timber",
			NextStep:   "Repair the water wheel",
			People:     []string{"mira"},
			Locations:  []string{"mill"},
		}}, nil
	})

	var updated tea.Model
	var cmd tea.Cmd
	for i := 0; i < 4; i++ {
		updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(TUIModel)
	}
	if model.tab != 4 {
		t.Fatalf("tab after four switches = %d, want projects", model.tab)
	}
	if cmd == nil {
		t.Fatal("selecting Projects did not request project data")
	}
	loaded := cmd()
	updated, _ = model.Update(loaded)
	model = updated.(TUIModel)
	view := model.View()
	for _, expected := range []string{"Projects", "Restore the Old Mill", "restore-mill", "Gathered timber", "Repair the water wheel"} {
		if !strings.Contains(view, expected) {
			t.Errorf("Projects view missing %q", expected)
		}
	}
}

func TestDefaultInputTriggersInfluence(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)

	cmd := model.handleInput("Check on the mill")
	if cmd != nil {
		t.Fatal("expected nil cmd when engine is nil")
	}
	if model.infoMessage != "Subconscious influence recorded (no engine active): Check on the mill" {
		t.Errorf("unexpected info message: %q", model.infoMessage)
	}
}

func TestTUIHeaderDisplaysHealthAndHunger(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)
	view := model.View()
	if !strings.Contains(view, "Health:") || !strings.Contains(view, "Hunger:") {
		t.Errorf("TUI View missing Health or Hunger in header: %q", view)
	}
}

func TestControlLRefreshesClientTUI(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewTUIModel(context.Background(), "elda", nil, nil, ctxMgr, nil, nil, nil, nil, nil, nil)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("Ctrl+L did not return a command for screen refresh")
	}
}
