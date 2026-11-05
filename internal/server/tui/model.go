package tui

import (
	"database/sql"
	"fmt"
	"strconv"
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
	logsTab
)

const maxDisplayedLogLines = 500

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
	textInput       textinput.Model
	viewport        viewport.Model
	logViewport     viewport.Model
	history         []string
	sensitiveOutput string
	logChannel      <-chan string
	logLines        []string

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
	logVP := viewport.New(0, 0)
	logVP.SetContent("Server logs will appear here.")

	return Model{
		db:          db,
		admin:       adm,
		provider:    provider,
		state:       statusTab,
		startTime:   time.Now(),
		textInput:   ti,
		viewport:    vp,
		logViewport: logVP,
	}
}

func (m *Model) SetLogChannel(logs <-chan string) {
	m.logChannel = logs
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		m.refreshStats(),
	}
	if m.logChannel != nil {
		cmds = append(cmds, m.listenLogs())
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.state == adminTab {
				m.sensitiveOutput = ""
			}
			m.state = (m.state + 1) % 5
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
		m.logViewport.Width = msg.Width - 4
		m.logViewport.Height = msg.Height - 6

	case tickMsg:
		m.tickCount = msg.tick
		m.weather = msg.weather
		return m, m.refreshStats()

	case logMsg:
		atBottom := m.logViewport.AtBottom()
		m.logLines = append(m.logLines, msg.line)
		if len(m.logLines) > maxDisplayedLogLines {
			m.logLines = append([]string(nil), m.logLines[len(m.logLines)-maxDisplayedLogLines:]...)
		}
		m.logViewport.SetContent(strings.Join(m.logLines, "\n"))
		if atBottom {
			m.logViewport.GotoBottom()
		}
		return m, m.listenLogs()
	}

	if m.state == adminTab {
		m.textInput, cmd = m.textInput.Update(msg)
		m.viewport, _ = m.viewport.Update(msg)
	} else if m.state == logsTab {
		m.logViewport, cmd = m.logViewport.Update(msg)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
	}

	return m, cmd
}

type logMsg struct {
	line string
}

func (m Model) listenLogs() tea.Cmd {
	if m.logChannel == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-m.logChannel
		if !ok {
			return nil
		}
		return logMsg{line: line}
	}
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
	m.sensitiveOutput = ""
	parts, parseErr := parseCommandLine(cmdStr)
	if parseErr != nil {
		m.history = append(m.history, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("> %s\nError: %v", cmdStr, parseErr)))
		m.viewport.SetContent(strings.Join(m.history, "\n"))
		m.viewport.GotoBottom()
		return m
	}
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
			"  weather <type> - set world weather (clear, cloudy, drought, fog, frost, hail, heatwave, rain, snow, storm, windy)\n" +
			"  set-desc <loc_id> <desc> - set location description\n" +
			"  set-owner <loc_id> <char_id|none> - set or clear location owner\n" +
			"  set-public <loc_id> <public|private> - set property visibility\n" +
			"  set-indoor <loc_id> <indoor|outdoor> - set interior status\n" +
			"  set-access <loc_id> <ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY> - set vehicle access\n" +
			"  set-hours <loc_id> <open_HH:MM> <close_HH:MM> - set opening hours (supports overnight)\n" +
			"  set-rules <loc_id> <rules> - set location rules prompt\n" +
			"  add-item <char_id> <item_id> <qty> - give item to character\n" +
			"  drop-item <char_id> <item_id> <qty> - remove item from character\n" +
			"  set-gold <char_id> <amount> - set character gold\n" +
			"  create-item <id> <name> <weight_kg> <description> [--consumable <hunger_restore>] [--container <capacity_kg> <slot_limit>]\n" +
			"  set-resource <loc_id> <resource_id> <current_stock> <max_stock> <replenish_per_day>\n" +
			"  set-shop-item <loc_id> <item_id> <price_gold> <stock_quantity>\n" +
			"  create-location <id> <name> <description> - creates a public, all-day location\n" +
			"  connect <from_loc> <to_loc> <travel_ticks> - adds a bidirectional connection\n" +
			"  create-character <id> <name> <occupation> <location_id> <persona> - creates an agent and generates an MQTT password\n" +
			"Quote names and descriptions containing spaces; use backslash escapes inside quotes.\n"
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
			err = fmt.Errorf("usage: set-owner <loc_id> <char_id|none>")
		} else {
			ownerID := args[1]
			if strings.EqualFold(ownerID, "none") {
				ownerID = ""
			}
			err = m.admin.SetLocationOwner(args[0], ownerID)
			if ownerID == "" {
				output = fmt.Sprintf("Cleared owner of %s", args[0])
			} else {
				output = fmt.Sprintf("Owner of %s set to %s", args[0], ownerID)
			}
		}
	case "set-public":
		if len(args) != 2 {
			err = fmt.Errorf("usage: set-public <loc_id> <public|private>")
		} else {
			var isPublic bool
			switch strings.ToLower(args[1]) {
			case "public":
				isPublic = true
			case "private":
			default:
				err = fmt.Errorf("visibility must be public or private")
			}
			if err == nil {
				err = m.admin.SetLocationPublic(args[0], isPublic)
				output = fmt.Sprintf("Set %s visibility to %s", args[0], strings.ToLower(args[1]))
			}
		}
	case "set-indoor":
		if len(args) != 2 {
			err = fmt.Errorf("usage: set-indoor <loc_id> <indoor|outdoor>")
		} else {
			var isIndoor bool
			switch strings.ToLower(args[1]) {
			case "indoor":
				isIndoor = true
			case "outdoor":
			default:
				err = fmt.Errorf("location type must be indoor or outdoor")
			}
			if err == nil {
				err = m.admin.SetLocationIndoor(args[0], isIndoor)
				output = fmt.Sprintf("Set %s to %s", args[0], strings.ToLower(args[1]))
			}
		}
	case "set-access":
		if len(args) != 2 {
			err = fmt.Errorf("usage: set-access <loc_id> <ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY>")
		} else {
			err = m.admin.SetLocationAccess(args[0], args[1])
			output = fmt.Sprintf("Set vehicle access tier for %s to %s", args[0], strings.ToUpper(args[1]))
		}
	case "set-hours":
		if len(args) != 3 {
			err = fmt.Errorf("usage: set-hours <loc_id> <open_HH:MM> <close_HH:MM>")
		} else {
			err = m.admin.SetLocationHours(args[0], args[1], args[2])
			output = fmt.Sprintf("Set opening hours for %s to %s-%s", args[0], args[1], args[2])
		}
	case "set-rules":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-rules <loc_id> <rules>")
		} else {
			err = m.admin.SetLocationRules(args[0], strings.Join(args[1:], " "))
			output = fmt.Sprintf("Updated rules for %s", args[0])
		}
	case "add-item":
		if len(args) < 3 {
			err = fmt.Errorf("usage: add-item <char_id> <item_id> <qty>")
		} else {
			qty, parseErr := strconv.Atoi(args[2])
			if parseErr != nil {
				err = fmt.Errorf("quantity must be an integer")
			} else {
				err = m.admin.AddItemToCharacter(args[0], args[1], qty)
				output = fmt.Sprintf("Added %d x %s to %s", qty, args[1], args[0])
			}
		}
	case "drop-item":
		if len(args) < 3 {
			err = fmt.Errorf("usage: drop-item <char_id> <item_id> <qty>")
		} else {
			qty, parseErr := strconv.Atoi(args[2])
			if parseErr != nil {
				err = fmt.Errorf("quantity must be an integer")
			} else {
				err = m.admin.RemoveItemFromCharacter(args[0], args[1], qty)
				output = fmt.Sprintf("Removed %d x %s from %s", qty, args[1], args[0])
			}
		}
	case "set-gold":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-gold <char_id> <amount>")
		} else {
			amount, parseErr := strconv.Atoi(args[1])
			if parseErr != nil {
				err = fmt.Errorf("amount must be an integer")
			} else {
				err = m.admin.SetCharacterGold(args[0], amount)
				output = fmt.Sprintf("Gold for %s set to %d", args[0], amount)
			}
		}
	case "create-item":
		if len(args) < 4 {
			err = fmt.Errorf("usage: create-item <id> <name> <weight_kg> <description> [--consumable <hunger_restore>] [--container <capacity_kg> <slot_limit>]")
		} else {
			item := admin.ItemDefinition{
				ID:          args[0],
				Name:        args[1],
				Description: args[3],
			}
			item.WeightKg, err = strconv.ParseFloat(args[2], 64)
			for i := 4; err == nil && i < len(args); {
				switch args[i] {
				case "--consumable":
					if item.IsConsumable || i+1 >= len(args) {
						err = fmt.Errorf("usage: --consumable <hunger_restore>")
						continue
					}
					item.IsConsumable = true
					item.HungerRestore, err = strconv.Atoi(args[i+1])
					if err != nil {
						err = fmt.Errorf("hunger_restore must be an integer")
						continue
					}
					i += 2
				case "--container":
					if item.IsContainer || i+2 >= len(args) {
						err = fmt.Errorf("usage: --container <capacity_kg> <slot_limit>")
						continue
					}
					item.IsContainer = true
					item.CapacityKg, err = strconv.ParseFloat(args[i+1], 64)
					if err != nil {
						err = fmt.Errorf("capacity_kg must be a number")
						continue
					}
					item.SlotLimit, err = strconv.Atoi(args[i+2])
					if err != nil {
						err = fmt.Errorf("slot_limit must be an integer")
						continue
					}
					i += 3
				default:
					err = fmt.Errorf("unknown create-item option %q", args[i])
				}
			}
			if err == nil {
				err = m.admin.CreateItem(item)
			}
			if err == nil {
				output = fmt.Sprintf("Created item %s", item.ID)
			}
		}
	case "set-resource":
		if len(args) != 5 {
			err = fmt.Errorf("usage: set-resource <loc_id> <resource_id> <current_stock> <max_stock> <replenish_per_day>")
		} else {
			currentStock, currentErr := strconv.Atoi(args[2])
			maxStock, maxErr := strconv.Atoi(args[3])
			replenishRate, replenishErr := strconv.Atoi(args[4])
			switch {
			case currentErr != nil || maxErr != nil || replenishErr != nil:
				err = fmt.Errorf("stock and replenish values must be integers")
			default:
				err = m.admin.SetLocationResource(args[0], args[1], currentStock, maxStock, replenishRate)
				output = fmt.Sprintf("Set %s at %s: stock %d/%d, replenish %d per day", args[1], args[0], currentStock, maxStock, replenishRate)
			}
		}
	case "set-shop-item":
		if len(args) != 4 {
			err = fmt.Errorf("usage: set-shop-item <loc_id> <item_id> <price_gold> <stock_quantity>")
		} else {
			priceGold, priceErr := strconv.Atoi(args[2])
			stockQuantity, stockErr := strconv.Atoi(args[3])
			if priceErr != nil || stockErr != nil {
				err = fmt.Errorf("price and stock quantity must be integers")
			} else {
				err = m.admin.SetShopItem(args[0], args[1], priceGold, stockQuantity)
				output = fmt.Sprintf("Set shop listing %s at %s: %d gold, stock %d", args[1], args[0], priceGold, stockQuantity)
			}
		}
	case "create-location":
		if len(args) != 3 {
			err = fmt.Errorf("usage: create-location <id> <name> <description>")
		} else {
			err = m.admin.CreateLocation(args[0], args[1], args[2])
			output = fmt.Sprintf("Created public, all-day location %s; connect it to reachable locations with connect", args[0])
		}
	case "connect":
		if len(args) != 3 {
			err = fmt.Errorf("usage: connect <from_loc> <to_loc> <travel_ticks>")
		} else {
			ticks, parseErr := strconv.Atoi(args[2])
			if parseErr != nil {
				err = fmt.Errorf("travel_ticks must be an integer")
			} else {
				err = m.admin.ConnectLocations(args[0], args[1], ticks)
				output = fmt.Sprintf("Connected %s and %s in both directions (%d ticks)", args[0], args[1], ticks)
			}
		}
	case "create-character":
		if len(args) != 5 {
			err = fmt.Errorf("usage: create-character <id> <name> <occupation> <location_id> <persona>")
		} else {
			var password string
			password, err = m.admin.CreateCharacter(args[0], args[1], args[2], args[3], args[4])
			if err == nil {
				output = fmt.Sprintf("Created character %s. Generated MQTT password is shown once below.", args[0])
				m.sensitiveOutput = "MQTT password (copy now): " + password
			}
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

func parseCommandLine(input string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote byte
	started, escaped := false, false

	for i := 0; i < len(input); i++ {
		ch := input[i]
		if escaped {
			token.WriteByte(ch)
			started, escaped = true, false
			continue
		}
		if ch == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				token.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			started = true
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if started {
				args = append(args, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteByte(ch)
		started = true
	}

	if escaped {
		return nil, fmt.Errorf("command ends with an incomplete escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed quote in command")
	}
	if started {
		args = append(args, token.String())
	}
	return args, nil
}

func (m Model) View() string {
	var sb strings.Builder

	tabs := []string{"1. Status", "2. Locations", "3. Characters", "4. Admin Console", "5. Logs"}
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
		sb.WriteString(fmt.Sprintf("Recent log lines:    %d (open Logs tab)\n", len(m.logLines)))
		sb.WriteString("\n[Instructions]\n- Press Tab to switch tabs.\n- Switch to the Admin Console tab to run system administration commands.\n")

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
		if m.sensitiveOutput != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.sensitiveOutput) + "\n")
		}
		sb.WriteString(m.textInput.View() + "\n")

		// Autocomplete suggestions popup
		allCommands := []string{
			"help",
			"weather <type> (clear|cloudy|drought|fog|frost|hail|heatwave|rain|snow|storm|windy)",
			"set-desc <loc_id> <desc>",
			"set-owner <loc_id> <char_id|none>",
			"set-public <loc_id> <public|private>",
			"set-indoor <loc_id> <indoor|outdoor>",
			"set-access <loc_id> <ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY>",
			"set-hours <loc_id> <open_HH:MM> <close_HH:MM>",
			"set-rules <loc_id> <rules>",
			"add-item <char_id> <item_id> <qty>",
			"drop-item <char_id> <item_id> <qty>",
			"set-gold <char_id> <amount>",
			"create-item <id> <name> <weight_kg> <description> [--consumable <hunger_restore>] [--container <capacity_kg> <slot_limit>]",
			"set-resource <loc_id> <resource_id> <current_stock> <max_stock> <replenish_per_day>",
			"set-shop-item <loc_id> <item_id> <price_gold> <stock_quantity>",
			"create-location <id> <name> <description>",
			"connect <from_loc> <to_loc> <travel_ticks>",
			"create-character <id> <name> <occupation> <location_id> <persona>",
		}
		query := strings.TrimSpace(m.textInput.Value())
		if fields := strings.Fields(query); len(fields) > 0 {
			query = fields[0]
		}
		var matched []string
		for _, cmd := range allCommands {
			cmdName := strings.Fields(cmd)[0]
			if query == "" || strings.HasPrefix(cmdName, query) {
				matched = append(matched, cmd)
			}
		}
		if len(matched) > 0 {
			sb.WriteString("\n[Available Commands Matching Prefix]\n")
			for _, mcmd := range matched {
				sb.WriteString(fmt.Sprintf("  > %s\n", mcmd))
			}
		}

	case logsTab:
		sb.WriteString(fmt.Sprintf("Server Logs (most recent %d lines; scroll with PgUp/PgDn)\n\n", maxDisplayedLogLines))
		sb.WriteString(m.logViewport.View())
	}

	return sb.String()
}
