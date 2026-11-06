package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelDisplaysBoundedLogHistoryInSeparateTab(t *testing.T) {
	model := NewModel(nil, nil, nil)
	model.state = logsTab
	model.logViewport.Width = 100
	model.logViewport.Height = 20
	for i := 0; i < maxDisplayedLogLines+10; i++ {
		updated, _ := model.Update(logMsg{line: fmt.Sprintf("server log %d", i)})
		model = updated.(Model)
	}

	if len(model.logLines) != maxDisplayedLogLines {
		t.Fatalf("log line count = %d, want %d", len(model.logLines), maxDisplayedLogLines)
	}
	if model.logLines[0] != "server log 10" || model.logLines[len(model.logLines)-1] != "server log 509" {
		t.Fatalf("unexpected retained log range: %q through %q", model.logLines[0], model.logLines[len(model.logLines)-1])
	}
	view := model.View()
	if !strings.Contains(view, "Server Logs") || !strings.Contains(view, "server log 509") {
		t.Fatalf("logs tab does not render buffered messages: %q", view)
	}
}

func TestModelCyclesThroughLogsTab(t *testing.T) {
	model := NewModel(nil, nil, nil)
	model.state = adminTab
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(Model).state; got != logsTab {
		t.Fatalf("tab after admin = %v, want logs", got)
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(Model).state; got != statusTab {
		t.Fatalf("tab after logs = %v, want status", got)
	}
}
