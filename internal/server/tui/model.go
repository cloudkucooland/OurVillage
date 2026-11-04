package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentmud/internal/server/admin"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sessionState int

const (
	statusTab sessionState = iota
	locationsTab
	charactersTab
	adminTab
)

type ServerStateProvider interface {
	GetTickCount() uint64
	GetWeather() string
}

type Model struct {
	db            *sql.DB
	admin         *admin.AdminCommandExecutor
	provider      ServerStateProvider
	state         sessionState
	width, height int

	// Status Info
	tickCount uint64
	weather   string
	startTime time.Time

	// Admin Console
	textInput textinput.Model
	viewport  viewport.Model
	history   []string

	// Lists
	locations  []string
	characters []string
}

func NewModel(db *sql.DB, adm *admin.AdminCommandExecutor, provider ServerStateProvider) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter admin command..."
	ti.Focus()

	vp := viewport.New(0, 0)
	vp.SetContent("Welcome to AgentMUD Server Admin Console.\nType 'help' for available commands.")

	return Model{
		db:        db,
		admin:     adm,
		provider:  provider,
		state:     statusTab,
		startTime: time.Now(),
		textInput: ti,
		viewport:  vp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.refreshStats(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.state = (m.state + 1) % 4
			return m, nil
		case "1":
			m.state = statusTab
			return m, nil
		case "2":
			m.state = locationsTab
			return m, nil
		case "3":
			m.state = charactersTab
			return m, nil
		case "4":
			m.state = adminTab
			return m, nil
		case "enter":
			if m.state == adminTab {
				input := m.textInput.Value()
				if input != "" {
					m = m.executeCommand(input)
					m.textInput.SetValue("")
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = msg.Height - 10

	case tickMsg:
		m.tickCount = msg.tick
		m.weather = msg.weather
		return m, m.refreshStats()
	}

	if m.state == adminTab {
		m.textInput, cmd = m.textInput.Update(msg)
		m.viewport, _ = m.viewport.Update(msg)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
	}

	return m, cmd
}

type tickMsg struct {
	tick    uint64
	weather string
}

func (m Model) refreshStats() tea.Cmd {
	return tea.Tick(time.Second*1, func(t time.Time) tea.Msg {
		var tick uint64
		var weather string = "clear"
		if m.provider != nil {
			tick = m.provider.GetTickCount()
			weather = m.provider.GetWeather()
		}
		return tickMsg{tick: tick, weather: weather}
	})
}

func (m Model) executeCommand(cmdStr string) Model {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return m
	}

	command := parts[0]
	args := parts[1:]

	var err error
	output := ""

	switch command {
	case "help":
		output = "Available commands:\n" +
			"  weather <type> - set world weather (clear, rain, snow, storm)\n" +
			"  set-desc <loc_id> <desc> - set location description\n" +
			"  set-owner <loc_id> <char_id> - set location owner\n" +
			"  add-item <char_id> <item_id> <qty> - give item to character\n" +
			"  drop-item <char_id> <item_id> <qty> - remove item from character\n" +
			"  set-gold <char_id> <amount> - set character gold\n"
	case "weather":
		if len(args) < 1 {
			err = fmt.Errorf("usage: weather <type>")
		} else {
			err = m.admin.SetWeather(args[0])
			output = fmt.Sprintf("Weather set to %s", args[0])
		}
	case "set-desc":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-desc <loc_id> <description>")
		} else {
			err = m.admin.SetLocationDescription(args[0], strings.Join(args[1:], " "))
			output = fmt.Sprintf("Description updated for %s", args[0])
		}
	case "set-owner":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-owner <loc_id> <char_id>")
		} else {
			err = m.admin.SetLocationOwner(args[0], args[1])
			output = fmt.Sprintf("Owner of %s set to %s", args[0], args[1])
		}
	case "add-item":
		if len(args) < 3 {
			err = fmt.Errorf("usage: add-item <char_id> <item_id> <qty>")
		} else {
			var qty int
			fmt.Sscanf(args[2], "%d", &qty)
			err = m.admin.AddItemToCharacter(args[0], args[1], qty)
			output = fmt.Sprintf("Added %d x %s to %s", qty, args[1], args[0])
		}
	case "drop-item":
		if len(args) < 3 {
			err = fmt.Errorf("usage: drop-item <char_id> <item_id> <qty>")
		} else {
			var qty int
			fmt.Sscanf(args[2], "%d", &qty)
			err = m.admin.RemoveItemFromCharacter(args[0], args[1], qty)
			output = fmt.Sprintf("Removed %d x %s from %s", qty, args[1], args[0])
		}
	case "set-gold":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-gold <char_id> <amount>")
		} else {
			var amount int
			fmt.Sscanf(args[1], "%d", &amount)
			err = m.admin.SetCharacterGold(args[0], amount)
			output = fmt.Sprintf("Gold for %s set to %d", args[0], amount)
		}
	default:
		err = fmt.Errorf("unknown command: %s", command)
	}

	if err != nil {
		m.history = append(m.history, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("> %s\nError: %v", cmdStr, err)))
	} else {
		m.history = append(m.history, fmt.Sprintf("> %s\n%s", cmdStr, output))
	}

	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
	return m
}

func (m Model) View() string {
	var sb strings.Builder

	tabs := []string{"1. Status", "2. Locations", "3. Characters", "4. Admin Console"}
	var tabStrs []string
	for i, t := range tabs {
		if int(m.state) == i {
			tabStrs = append(tabStrs, fmt.Sprintf("[%s]", t))
		} else {
			tabStrs = append(tabStrs, fmt.Sprintf(" %s ", t))
		}
	}
	sb.WriteString(strings.Join(tabStrs, "  ") + "\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n\n")

	switch m.state {
	case statusTab:
		sb.WriteString(fmt.Sprintf("Server Master Tick: %d\n", m.tickCount))
		sb.WriteString(fmt.Sprintf("Current Weather:    %s\n", m.weather))
		sb.WriteString(fmt.Sprintf("Uptime:             %v\n", time.Since(m.startTime).Truncate(time.Second)))
		sb.WriteString("\n[Instructions]\n- Press 1-4 or Tab to switch tabs.\n- Switch to Tab 4 (Admin Console) to run system administration commands.\n")

	case locationsTab:
		sb.WriteString("Registered Locations:\n\n")
		rows, err := m.db.Query("SELECT id, name, is_indoor, COALESCE(owner_id, 'None') FROM locations")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name string
				var isIndoor bool
				var owner string
				if err := rows.Scan(&id, &name, &isIndoor, &owner); err == nil {
					indoorStr := "Outdoor"
					if isIndoor {
						indoorStr = "Indoor"
					}
					sb.WriteString(fmt.Sprintf("• [%s] %s (%s) — Owner: %s\n", id, name, indoorStr, owner))
				}
			}
		}

	case charactersTab:
		sb.WriteString("Registered Characters:\n\n")
		rows, err := m.db.Query("SELECT id, name, current_location, gold, status, is_online FROM characters")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name, loc, status string
				var gold int
				var online bool
				if err := rows.Scan(&id, &name, &loc, &gold, &status, &online); err == nil {
					onlineStr := "offline"
					if online {
						onlineStr = "ONLINE"
					}
					sb.WriteString(fmt.Sprintf("• %s (%s) @ %s | Gold: %d | Status: %s [%s]\n", name, id, loc, gold, status, onlineStr))
				}
			}
		}

	case adminTab:
		sb.WriteString("Admin Console Log & Output:\n")
		sb.WriteString(m.viewport.View() + "\n\n")
		sb.WriteString(m.textInput.View() + "\n")
		sb.WriteString("\nCommands: weather <type>, set-desc <loc_id> <desc>, set-owner <loc_id> <char_id>, add-item <char> <item> <qty>, drop-item <char> <item> <qty>, set-gold <char> <amount>\n")
	}

	return sb.String()
}
