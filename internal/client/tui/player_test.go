package tui

import (
	"strings"
	"testing"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/spatial"
)

func TestBuildPlayerIntentSupportsDirectWorldActions(t *testing.T) {
	tests := []struct {
		command string
		action  string
	}{
		{"/look", "LOOK"},
		{"/status", "STATUS"},
		{"/move market", "MOVE"},
		{"/m market", "MOVE"},
		{"/chat Hello village", "CHAT"},
		{"/c Hello village", "CHAT"},
		{"/forage firewood", "FORAGE"},
		{"/collect berries 12", "FORAGE"},
		{"/plant field_plot_1 wheat", "PLANT"},
		{"/harvest field_plot_1", "HARVEST"},
		{"/give martha gold 5", "TRANSFER"},
		{"/give martha bread 2", "TRANSFER"},
		{"/drop bread", "DROP"},
		{"/pickup bread 2", "PICKUP"},
		{"/equip axe", "EQUIP"},
		{"/unequip", "UNEQUIP"},
		{"/craft mill_flour", "CRAFT"},
		{"/buy bread 2", "BUY"},
		{"/restock bread 3 2", "RESTOCK"},
		{"/eat bread", "EAT"},
		{"/drink water_flask", "EAT"},
		{"/rest", "REST"},
		{"/cancel", "CANCEL_ACTION"},
	}

	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			intent, err := buildPlayerIntent(test.command)
			if err != nil {
				t.Fatalf("buildPlayerIntent() error = %v", err)
			}
			if intent.Action != test.action {
				t.Errorf("action = %q, want %q", intent.Action, test.action)
			}
		})
	}
}

func TestBuildPlayerIntentRejectsInvalidOrPlainTextInput(t *testing.T) {
	for _, input := range []string{"go to market", "/give martha gold 0", "/craft", "/rest -1"} {
		if _, err := buildPlayerIntent(input); err == nil {
			t.Errorf("buildPlayerIntent(%q) unexpectedly succeeded", input)
		}
	}
}

func TestPlayerInputDoesNotInvokeLLMForPlainText(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewPlayerModel(t.Context(), ctxMgr, nil)

	if cmd := model.handleInput("go to the market"); cmd != nil {
		t.Fatal("plain text unexpectedly created a command")
	}
	if !strings.Contains(model.errMessage, "not sent to an LLM") {
		t.Fatalf("error = %q, want an explicit no-LLM message", model.errMessage)
	}
	if strings.Contains(model.View(), "LLM Engine active") {
		t.Fatal("player TUI should not display LLM status")
	}
}

func TestPlayerDisplaysServerErrorsAsErrors(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewPlayerModel(t.Context(), ctxMgr, nil)

	updated, _ := model.Update(playerResultMsg{
		result: dispatcher.ExecutionResult{Status: "ERROR", ErrorCode: "CHARACTER_BUSY", Message: "Cannot move"},
	})
	model = updated.(PlayerModel)
	if !strings.Contains(model.View(), "CHARACTER_BUSY: Cannot move") {
		t.Fatal("server error was not shown in the error status")
	}
}

func TestPlayerTUIHeaderDisplaysHealthAndHunger(t *testing.T) {
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{AgentID: "elda"})
	model := NewPlayerModel(t.Context(), ctxMgr, nil)
	view := model.View()
	if !strings.Contains(view, "Health:") || !strings.Contains(view, "Hunger:") {
		t.Errorf("Player TUI View missing Health or Hunger in header: %q", view)
	}
}
