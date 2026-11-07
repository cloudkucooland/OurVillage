package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/spatial"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PlayerModel struct {
	ctx         context.Context
	ctxMgr      *spatial.ContextManager
	dispatcher  *dispatcher.IntentDispatcher
	textInput   textinput.Model
	viewport    viewport.Model
	tab         int
	width       int
	errMessage  string
	infoMessage string
	busy        bool
	quitting    bool
}

type playerTickMsg time.Time
type playerResultMsg struct {
	result dispatcher.ExecutionResult
	err    error
}

func NewPlayerModel(ctx context.Context, ctxMgr *spatial.ContextManager, disp *dispatcher.IntentDispatcher) PlayerModel {
	ti := textinput.New()
	ti.Placeholder = "Type /help or a command such as /look, /move, /craft..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 80

	vp := viewport.New(80, 20)
	vp.SetContent("Connecting to OurVillage...")

	return PlayerModel{
		ctx:        ctx,
		ctxMgr:     ctxMgr,
		dispatcher: disp,
		textInput:  ti,
		viewport:   vp,
	}
}

func (m PlayerModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return playerTickMsg(t) }),
	)
}

func (m PlayerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyTab:
			m.tab = (m.tab + 1) % 3
			return m, nil
		case tea.KeyEnter:
			input := strings.TrimSpace(m.textInput.Value())
			m.textInput.Reset()
			if input != "" {
				return m, m.handleInput(input)
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.viewport.Width = msg.Width - 6
		m.viewport.Height = msg.Height - 12
		if m.viewport.Height < 5 {
			m.viewport.Height = 5
		}
		m.textInput.Width = msg.Width - 6
	case playerTickMsg:
		logs := m.ctxMgr.FormatSensoryLogs()
		m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width).Render(strings.Join(logs, "\n")))
		m.viewport.GotoBottom()
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return playerTickMsg(t) })
	case playerResultMsg:
		m.busy = false
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		if !strings.EqualFold(msg.result.Status, "SUCCESS") {
			m.errMessage = msg.result.Message
			if msg.result.ErrorCode != "" {
				m.errMessage = msg.result.ErrorCode + ": " + m.errMessage
			}
			return m, nil
		}
		m.infoMessage = msg.result.Message
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	m.viewport, _ = m.viewport.Update(msg)
	return m, cmd
}

func (m PlayerModel) View() string {
	if m.quitting {
		return "Leaving OurVillage...\n"
	}

	headerText := fmt.Sprintf(
		"Character: %s | Location: %s (%s) | Gold: %d | Energy: %d | Health: %d | Hunger: %d | Status: %s | Time: %s | Weather: %s",
		m.ctxMgr.AgentName(), m.ctxMgr.LocationName(), m.ctxMgr.CurrentLocation(),
		m.ctxMgr.Gold(), m.ctxMgr.Energy(), m.ctxMgr.Health(), m.ctxMgr.Hunger(), m.ctxMgr.Status(),
		m.ctxMgr.LastInGameTime(), m.ctxMgr.LastWeather(),
	)
	header := headerBoxStyle.Render(titleStyle.Render("OurVillage Player TUI") + "\n" + headerText)

	tabs := []string{"Sensory Log", "Inventory", "Commands"}
	renderedTabs := make([]string, 0, len(tabs))
	for i, tab := range tabs {
		if i == m.tab {
			renderedTabs = append(renderedTabs, tabActiveStyle.Render(tab))
		} else {
			renderedTabs = append(renderedTabs, tabInactiveStyle.Render(tab))
		}
	}

	var content string
	switch m.tab {
	case 0:
		content = m.viewport.View()
	case 1:
		inventory := m.ctxMgr.Inventory()
		if len(inventory) == 0 {
			content = "Inventory is empty."
		} else {
			content = "=== INVENTORY ===\n" + strings.Join(inventory, "\n")
		}
	case 2:
		content = playerHelpText
	}
	mainPane := paneStyle.Width(m.width - 2).Render(strings.Join(renderedTabs, "  ") + "\n\n" + content)

	status := "Type /help for commands. Plain text is not sent or interpreted."
	if m.errMessage != "" {
		status = errorStyle.Render("ERROR: " + m.errMessage)
	} else if m.busy {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Render("Waiting for world response...")
	} else if m.infoMessage != "" {
		status = infoStyle.Render("INFO: " + m.infoMessage)
	}
	return fmt.Sprintf("%s\n%s\n%s\n%s", header, mainPane, status, m.textInput.View())
}

func (m *PlayerModel) handleInput(input string) tea.Cmd {
	m.errMessage = ""
	m.infoMessage = ""
	if m.busy {
		m.errMessage = "wait for the current action to finish before sending another command"
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(input), "/help") {
		m.tab = 2
		return nil
	}
	intent, err := buildPlayerIntent(input)
	if err != nil {
		m.errMessage = err.Error()
		return nil
	}
	if m.dispatcher == nil {
		m.errMessage = "The player is not connected to the world."
		return nil
	}
	m.busy = true
	ctx := m.ctx
	disp := m.dispatcher
	return func() tea.Msg {
		result, err := disp.Dispatch(ctx, intent, 10*time.Second)
		return playerResultMsg{result: result, err: err}
	}
}

const playerHelpText = `=== PLAYER COMMANDS ===
/look, /status                  Refresh character and location
/move <location_id> (/m)        Move to a connected location
/chat <message> (/c)            Send public chat
/forage <resource> [ticks]      Gather a resource (default: 60 ticks)
/collect <resource> [ticks]     Alias for /forage
/plant <plot_id> <crop_id>       Plant a seed in an empty communal plot
/harvest <plot_id>               Harvest a mature communal plot
/give <recipient> gold <amount> Give gold to a character
/give <recipient> <item> <qty>  Give items to a character
/drop <item> [qty]              Drop items
/pickup <item> [qty]            Pick up ground items
/equip <item>                   Equip an item
/unequip                        Unequip the current item
/craft <recipe_id>              Craft a recipe available here
/buy <item> [qty]               Buy a shop item
/restock <item> <qty> <price>   Restock a shop
/eat <item> [qty]               Eat an item
/drink <item> [qty]             Alias for /eat
/rest [ticks]                   Rest (default: 30 ticks)
/cancel                         Cancel the current action
/help                           Show this command list
Tab                             Switch tabs; Ctrl+C / Esc quits`

func buildPlayerIntent(input string) (dispatcher.Intent, error) {
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		return dispatcher.Intent{}, fmt.Errorf("commands must start with /; plain text is not sent to an LLM")
	}
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return dispatcher.Intent{}, fmt.Errorf("enter a slash command; type /help for help")
	}
	command := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := fields[1:]
	action := ""
	payload := map[string]any{}
	usage := ""
	require := func(min, max int, form string) error {
		if len(args) < min || len(args) > max {
			return fmt.Errorf("usage: %s", form)
		}
		return nil
	}
	parsePositive := func(value, label string) (int, error) {
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%s must be a positive integer", label)
		}
		return n, nil
	}
	itemAndQuantity := func(maxArgs int) (string, int, error) {
		if err := require(1, maxArgs, "/"+command+" <item_id> [quantity]"); err != nil {
			return "", 0, err
		}
		quantity := 1
		if len(args) > 1 {
			parsed, err := parsePositive(args[1], "quantity")
			if err != nil {
				return "", 0, err
			}
			quantity = parsed
		}
		return args[0], quantity, nil
	}

	switch command {
	case "look", "status":
		if err := require(0, 0, "/"+command); err != nil {
			return dispatcher.Intent{}, err
		}
		action = strings.ToUpper(command)
	case "move", "m":
		usage = "/move <location_id>"
		if err := require(1, 1, usage); err != nil {
			return dispatcher.Intent{}, err
		}
		action, payload["target_location"] = "MOVE", args[0]
	case "chat", "c":
		if err := require(1, len(args), "/chat <message>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "CHAT"
		payload = map[string]any{"message": strings.Join(args, " ")}
	case "forage", "collect":
		if err := require(1, 2, "/"+command+" <resource_id> [ticks]"); err != nil {
			return dispatcher.Intent{}, err
		}
		duration := 60
		if len(args) == 2 {
			parsed, err := parsePositive(args[1], "duration")
			if err != nil {
				return dispatcher.Intent{}, err
			}
			duration = parsed
		}
		action = "FORAGE"
		payload = map[string]any{"target_resource": args[0], "duration_ticks": duration}
	case "plant":
		if err := require(2, 2, "/plant <plot_id> <crop_id>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "PLANT"
		payload = map[string]any{"plot_id": args[0], "crop_id": args[1]}
	case "harvest":
		if err := require(1, 1, "/harvest <plot_id>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "HARVEST"
		payload = map[string]any{"plot_id": args[0]}
	case "give", "transfer":
		if err := require(3, 3, "/"+command+" <recipient_id> gold <amount> | <recipient_id> <item_id> <quantity>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "TRANSFER"
		payload = map[string]any{"recipient_id": args[0]}
		if strings.EqualFold(args[1], "gold") {
			amount, err := parsePositive(args[2], "amount")
			if err != nil {
				return dispatcher.Intent{}, err
			}
			payload["amount"] = amount
		} else {
			quantity, err := parsePositive(args[2], "quantity")
			if err != nil {
				return dispatcher.Intent{}, err
			}
			payload["item_id"], payload["quantity"] = args[1], quantity
		}
	case "drop", "pickup", "eat", "drink":
		item, quantity, err := itemAndQuantity(2)
		if err != nil {
			return dispatcher.Intent{}, err
		}
		action = strings.ToUpper(command)
		if action == "DRINK" {
			action = "EAT"
		}
		payload = map[string]any{"item_id": item, "quantity": quantity}
	case "equip":
		if err := require(1, 1, "/equip <item_id>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action, payload = "EQUIP", map[string]any{"item_id": args[0]}
	case "unequip":
		if err := require(0, 0, "/unequip"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "UNEQUIP"
	case "craft":
		if err := require(1, 1, "/craft <recipe_id>"); err != nil {
			return dispatcher.Intent{}, err
		}
		action, payload = "CRAFT", map[string]any{"recipe_id": args[0]}
	case "buy":
		item, quantity, err := itemAndQuantity(2)
		if err != nil {
			return dispatcher.Intent{}, err
		}
		action, payload = "BUY", map[string]any{"item_id": item, "quantity": quantity}
	case "restock":
		if err := require(3, 3, "/restock <item_id> <quantity> <price_gold>"); err != nil {
			return dispatcher.Intent{}, err
		}
		quantity, err := parsePositive(args[1], "quantity")
		if err != nil {
			return dispatcher.Intent{}, err
		}
		price, err := strconv.Atoi(args[2])
		if err != nil || price < 0 {
			return dispatcher.Intent{}, fmt.Errorf("price_gold must be a non-negative integer")
		}
		action, payload = "RESTOCK", map[string]any{"item_id": args[0], "quantity": quantity, "price_gold": price}
	case "rest":
		if err := require(0, 1, "/rest [ticks]"); err != nil {
			return dispatcher.Intent{}, err
		}
		duration := 30
		if len(args) == 1 {
			parsed, err := parsePositive(args[0], "duration")
			if err != nil {
				return dispatcher.Intent{}, err
			}
			duration = parsed
		}
		action, payload = "REST", map[string]any{"duration_ticks": duration}
	case "cancel", "cancel-action":
		if err := require(0, 0, "/cancel"); err != nil {
			return dispatcher.Intent{}, err
		}
		action = "CANCEL_ACTION"
	default:
		return dispatcher.Intent{}, fmt.Errorf("unknown command %q; type /help for available commands", fields[0])
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return dispatcher.Intent{}, fmt.Errorf("encode %s command: %w", action, err)
	}
	return dispatcher.ValidateIntent(dispatcher.Intent{
		IntentID: dispatcher.GenerateIntentID(),
		Action:   action,
		Payload:  payloadBytes,
	})
}
