package tui

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	worlddb "ourvillage/internal/db"
	"ourvillage/internal/server/admin"

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
	recipesTab
	itemsTab
	backupTab
	mapTab
)

const tabCount = 9

type backupAction int

const (
	backupIdle backupAction = iota
	backupExportPath
	backupImportPath
	backupImportConfirm
)

const maxDisplayedLogLines = 500

type ServerStateProvider interface {
	GetTickCount() uint64
	GetWeather() string
	GetTemperature() float64
}

type Model struct {
	db            *sql.DB
	admin         *admin.AdminCommandExecutor
	provider      ServerStateProvider
	state         sessionState
	width, height int

	// Status Info
	tickCount   uint64
	weather     string
	temperature float64
	startTime   time.Time

	// Admin Console
	textInput       textinput.Model
	viewport        viewport.Model
	logViewport     viewport.Model
	history         []string
	commandHistory  []string
	historyIndex    int
	draftInput      string
	sensitiveOutput string
	logChannel      <-chan string
	logLines        []string
	tabCycleIndex   int
	tabPrefix       string
	tabCandidates   []string

	// Lists
	locations           []string
	characters          []string
	characterRows       []characterSummary
	selectedCharacter   int
	inspectingCharacter bool
	locationRows        []locationSummary
	selectedLocation    int
	inspectingLocation  bool
	locationEditor      bool
	locationEditing     bool
	locationStatus      string
	recipeRows          []worlddb.Recipe
	selectedRecipe      int
	inspectingRecipe    bool
	recipeEditor        bool
	recipeEditing       bool
	recipeDeletePrompt  bool
	recipeStatus        string
	itemRows            []admin.ItemDefinition
	selectedItem        int
	inspectingItem      bool
	itemEditor          bool
	itemEditing         bool
	itemDeletePrompt    bool
	itemStatus          string
	backupAction        backupAction
	backupPath          string
	backupStatus        string
}

type characterSummary struct {
	id, name, occupation, location, status, controller string
	gold, hunger, energy, health                       int
	online                                             bool
	actionTarget                                       string
	actionEndTick                                      uint64
}

type locationSummary struct {
	id, name, description, owner, access, openTime, closeTime, rules string
	public, indoor                                                   bool
}

var commandSyntaxes = map[string]string{
	"help":             "help",
	"weather":          "weather <clear|cloudy|drought|fog|frost|hail|heatwave|rain|snow|storm|windy>",
	"temp":             "temp <celsius>",
	"temperature":      "temperature <celsius>",
	"set-desc":         "set-desc <loc_id> <description>",
	"set-name":         "set-name <loc_id> <name>",
	"set-owner":        "set-owner <loc_id> <char_id|none>",
	"set-public":       "set-public <loc_id> <public|private>",
	"set-indoor":       "set-indoor <loc_id> <indoor|outdoor>",
	"set-access":       "set-access <loc_id> <ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY>",
	"set-hours":        "set-hours <loc_id> <open_HH:MM> <close_HH:MM>",
	"set-rules":        "set-rules <loc_id> <rules>",
	"add-item":         "add-item <char_id> <item_id> <qty>",
	"drop-item":        "drop-item <char_id> <item_id> <qty>",
	"set-gold":         "set-gold <char_id> <amount>",
	"full-heal":        "full-heal <character_id>",
	"create-item":      "create-item <id> <name> <weight_kg> <description> [--consumable <hunger_restore>] [--container <capacity_kg> <slot_limit>]",
	"set-resource":     "set-resource <loc_id> <resource_id> <current_stock> <max_stock> <replenish_per_day>",
	"set-shop-item":    "set-shop-item <loc_id> <item_id> <price_gold> <stock_quantity>",
	"create-location":  "create-location <id> <name> <description>",
	"connect":          "connect <from_loc> <to_loc> <travel_ticks>",
	"create-character": "create-character <id> <name> <occupation> <location_id> <persona>",
	"move":             "move <character_id> <location_id>",
	"teach-recipe":     "teach-recipe <character_id> <recipe_id>",
	"set-skill":        "set-skill <character_id> <skill_id> <level> <experience>",
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
	if m.state == locationsTab || m.state == mapTab {
		m.refreshLocationRows()
	}
	if m.state == recipesTab {
		m.refreshRecipeRows()
	}
	if m.state == itemsTab {
		m.refreshItemRows()
	}
	if m.state == charactersTab {
		m.refreshCharacterRows()
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// The admin input is a command line, so q must remain typeable there.
			if m.backupAction == backupIdle && !m.recipeEditor && !m.itemEditor && (m.state != adminTab || m.textInput.Value() == "") {
				return m, tea.Quit
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Number shortcuts provide direct navigation when not entering a command.
			if m.backupAction == backupIdle && !m.recipeEditor && !m.itemEditor && (m.state != adminTab || m.textInput.Value() == "") {
				m.state = sessionState(msg.String()[0] - '1')
				if m.state != adminTab && m.state != recipesTab && m.state != itemsTab {
					m.sensitiveOutput = ""
				}
				return m, nil
			}
		case "a":
			if (m.state == locationsTab || m.state == mapTab) && !m.locationEditor {
				m.locationEditor = true
				m.locationEditing = false
				m.locationStatus = ""
				m.textInput.Placeholder = `id "name" "description" owner|none public true|false indoor true|false access open_HH:MM close_HH:MM "rules"|none`
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, nil
			}
			if m.state == itemsTab && !m.itemEditor && !m.itemDeletePrompt {
				m.itemEditor = true
				m.itemEditing = false
				m.itemStatus = ""
				m.textInput.Placeholder = `id "name" weight_kg "description" hunger_restore health_restore consumable container capacity_kg slots perishable shelf_life_ticks spoilage_item|none`
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, nil
			}
			if m.state == recipesTab && !m.recipeEditor && !m.recipeDeletePrompt {
				m.recipeEditor = true
				m.recipeEditing = false
				m.recipeStatus = ""
				m.textInput.Placeholder = `id "name" output_item qty duration energy station|none skill|none min_skill item:qty[,item:qty...]`
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, nil
			}
		case "e":
			if m.state == backupTab && m.backupAction == backupIdle {
				m.backupAction = backupExportPath
				m.textInput.Placeholder = "Directory for CSV backup"
				m.textInput.SetValue("agentmud-backup")
				m.textInput.CursorEnd()
				return m, nil
			}
			if (m.state == locationsTab || m.state == mapTab) && !m.locationEditor && len(m.locationRows) > 0 {
				m.locationEditor = true
				m.locationEditing = true
				m.locationStatus = ""
				m.textInput.Placeholder = `id "name" "description" owner|none public true|false indoor true|false access open_HH:MM close_HH:MM "rules"|none`
				m.textInput.SetValue(locationEditorValue(m.locationRows[m.selectedLocation]))
				m.textInput.CursorEnd()
				m.textInput.Focus()
				return m, nil
			}
			if m.state == itemsTab && !m.itemEditor && len(m.itemRows) > 0 {
				m.itemEditor = true
				m.itemEditing = true
				m.itemStatus = ""
				m.textInput.Placeholder = `id "name" weight_kg "description" hunger_restore health_restore consumable container capacity_kg slots perishable shelf_life_ticks spoilage_item|none`
				m.textInput.SetValue(itemEditorValue(m.itemRows[m.selectedItem]))
				m.textInput.CursorEnd()
				m.textInput.Focus()
				return m, nil
			}
			if m.state == recipesTab && !m.recipeEditor && len(m.recipeRows) > 0 {
				m.recipeEditor = true
				m.recipeEditing = true
				m.recipeStatus = ""
				m.textInput.Placeholder = `id "name" output_item qty duration energy station|none skill|none min_skill item:qty[,item:qty...]`
				m.textInput.SetValue(recipeEditorValue(m.recipeRows[m.selectedRecipe]))
				m.textInput.Focus()
				return m, nil
			}
		case "i":
			if m.state == backupTab && m.backupAction == backupIdle {
				m.backupAction = backupImportPath
				m.textInput.Placeholder = "Directory containing CSV backup"
				m.textInput.SetValue("agentmud-backup")
				m.textInput.CursorEnd()
				return m, nil
			}
		case "d":
			if m.state == itemsTab && !m.itemEditor && len(m.itemRows) > 0 {
				m.itemDeletePrompt = true
				return m, nil
			}
			if m.state == recipesTab && !m.recipeEditor && len(m.recipeRows) > 0 {
				m.recipeDeletePrompt = true
				return m, nil
			}
		case "y", "n":
			if m.state == backupTab && m.backupAction == backupImportConfirm {
				if msg.String() == "y" {
					m = m.restoreCSVBackup()
				}
				m.backupAction = backupIdle
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if m.state == itemsTab && m.itemDeletePrompt {
				if msg.String() == "y" && m.admin != nil && len(m.itemRows) > 0 {
					if err := m.admin.DeleteItem(m.itemRows[m.selectedItem].ID); err != nil {
						m.itemStatus = "Delete failed: " + err.Error()
					} else {
						m.itemStatus = "Item deleted."
						m.refreshItemRows()
					}
				}
				m.itemDeletePrompt = false
				return m, nil
			}
			if m.state == recipesTab && m.recipeDeletePrompt {
				if msg.String() == "y" && m.admin != nil {
					if err := m.admin.DeleteRecipe(m.recipeRows[m.selectedRecipe].ID); err != nil {
						m.recipeStatus = "Delete failed: " + err.Error()
					} else {
						m.recipeStatus = "Recipe deleted."
						m.refreshRecipeRows()
					}
				}
				m.recipeDeletePrompt = false
				return m, nil
			}
		case "ctrl+l":
			return m, tea.ClearScreen
		case "?", "f1":
			if m.state == backupTab && m.backupAction != backupIdle && msg.String() == "?" {
				break
			}
			if m.state == itemsTab && m.itemEditor && msg.String() == "?" {
				break
			}
			if m.state == recipesTab && m.recipeEditor && msg.String() == "?" {
				break
			}
			if (m.state == locationsTab || m.state == mapTab) && m.locationEditor && msg.String() == "?" {
				break
			}
			if msg.String() == "?" && m.state == adminTab && m.textInput.Value() != "" {
				break
			}
			m.state = adminTab
			m.textInput.SetValue("help")
			m.textInput.CursorEnd()
			return m, nil
		case "tab", "shift+tab":
			if (m.state == recipesTab && m.recipeEditor) || (m.state == itemsTab && m.itemEditor) || ((m.state == locationsTab || m.state == mapTab) && m.locationEditor) || (m.state == backupTab && m.backupAction != backupIdle) {
				break
			}
			if msg.String() == "shift+tab" || (m.state == adminTab && m.textInput.Value() == "") {
				if m.state == adminTab {
					m.sensitiveOutput = ""
				}
				if msg.String() == "shift+tab" {
					m.state = (m.state - 1 + tabCount) % tabCount
				} else {
					m.state = (m.state + 1) % tabCount
				}
				return m, nil
			}
			if m.state == adminTab {
				m.handleTabCompletion()
				return m, nil
			}
			m.state = (m.state + 1) % tabCount
			return m, nil
		case "up":
			if m.state == itemsTab && !m.itemEditor && !m.itemDeletePrompt {
				if m.selectedItem > 0 {
					m.selectedItem--
				}
				return m, nil
			}
			if m.state == recipesTab && !m.recipeEditor && !m.recipeDeletePrompt {
				if m.selectedRecipe > 0 {
					m.selectedRecipe--
				}
				return m, nil
			}
			if m.state == locationsTab || m.state == mapTab {
				if m.selectedLocation > 0 {
					m.selectedLocation--
				}
				return m, nil
			}
			if m.state == charactersTab {
				if m.inspectingCharacter {
					m.selectedCharacter--
					if m.selectedCharacter < 0 {
						m.selectedCharacter = 0
					}
				} else if m.selectedCharacter > 0 {
					m.selectedCharacter--
				}
				return m, nil
			}
			if m.state == adminTab {
				if len(m.commandHistory) > 0 {
					if m.historyIndex == len(m.commandHistory) {
						m.draftInput = m.textInput.Value()
					}
					if m.historyIndex > 0 {
						m.historyIndex--
						m.textInput.SetValue(m.commandHistory[m.historyIndex])
						m.textInput.CursorEnd()
					}
				}
				return m, nil
			}
		case "down":
			if m.state == itemsTab && !m.itemEditor && !m.itemDeletePrompt {
				if m.selectedItem+1 < len(m.itemRows) {
					m.selectedItem++
				}
				return m, nil
			}
			if m.state == recipesTab && !m.recipeEditor && !m.recipeDeletePrompt {
				if m.selectedRecipe+1 < len(m.recipeRows) {
					m.selectedRecipe++
				}
				return m, nil
			}
			if m.state == locationsTab || m.state == mapTab {
				if m.selectedLocation+1 < len(m.locationRows) {
					m.selectedLocation++
				}
				return m, nil
			}
			if m.state == charactersTab {
				if m.inspectingCharacter {
					if m.selectedCharacter+1 < len(m.characterRows) {
						m.selectedCharacter++
					}
				} else if m.selectedCharacter+1 < len(m.characterRows) {
					m.selectedCharacter++
				}
				return m, nil
			}
			if m.state == adminTab {
				if m.historyIndex < len(m.commandHistory) {
					m.historyIndex++
					if m.historyIndex == len(m.commandHistory) {
						m.textInput.SetValue(m.draftInput)
					} else {
						m.textInput.SetValue(m.commandHistory[m.historyIndex])
					}
					m.textInput.CursorEnd()
				}
				return m, nil
			}
		case "esc":
			if m.state == backupTab && m.backupAction != backupIdle {
				m.backupAction = backupIdle
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if m.state == itemsTab && m.itemEditor {
				m.itemEditor = false
				m.itemEditing = false
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if m.state == itemsTab && m.itemDeletePrompt {
				m.itemDeletePrompt = false
				return m, nil
			}
			if m.state == itemsTab && m.inspectingItem {
				m.inspectingItem = false
				return m, nil
			}
			if m.state == recipesTab && m.recipeEditor {
				m.recipeEditor = false
				m.recipeEditing = false
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if m.state == recipesTab && m.recipeDeletePrompt {
				m.recipeDeletePrompt = false
				return m, nil
			}
			if m.state == recipesTab && m.inspectingRecipe {
				m.inspectingRecipe = false
				return m, nil
			}
			if (m.state == locationsTab || m.state == mapTab) && m.locationEditor {
				m.locationEditor = false
				m.locationEditing = false
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if (m.state == locationsTab || m.state == mapTab) && m.inspectingLocation {
				m.inspectingLocation = false
				return m, nil
			}
			if m.state == charactersTab && m.inspectingCharacter {
				m.inspectingCharacter = false
				return m, nil
			}
		case "enter":
			if m.state == backupTab && m.backupAction == backupExportPath {
				m.backupPath = strings.TrimSpace(m.textInput.Value())
				if m.backupPath == "" {
					m.backupStatus = "Enter a backup directory path."
					return m, nil
				}
				if err := worlddb.ExportCSV(m.db, m.backupPath); err != nil {
					m.backupStatus = "Export failed: " + err.Error()
				} else {
					m.backupStatus = "CSV backup exported to " + m.backupPath
				}
				m.backupAction = backupIdle
				m.textInput.Placeholder = "Enter admin command..."
				m.textInput.SetValue("")
				return m, nil
			}
			if m.state == backupTab && m.backupAction == backupImportPath {
				m.backupPath = strings.TrimSpace(m.textInput.Value())
				if m.backupPath == "" {
					m.backupStatus = "Enter a backup directory path."
					return m, nil
				}
				m.backupAction = backupImportConfirm
				return m, nil
			}
			if m.state == itemsTab && m.itemEditor {
				m = m.saveItemEditor()
				return m, nil
			}
			if m.state == itemsTab && len(m.itemRows) > 0 {
				m.inspectingItem = !m.inspectingItem
				return m, nil
			}
			if m.state == recipesTab && m.recipeEditor {
				m = m.saveRecipeEditor()
				return m, nil
			}
			if m.state == recipesTab && len(m.recipeRows) > 0 {
				m.inspectingRecipe = !m.inspectingRecipe
				return m, nil
			}
			if (m.state == locationsTab || m.state == mapTab) && m.locationEditor {
				m = m.saveLocationEditor()
				return m, nil
			}
			if (m.state == locationsTab || m.state == mapTab) && len(m.locationRows) > 0 {
				m.inspectingLocation = !m.inspectingLocation
				return m, nil
			}
			if m.state == charactersTab && len(m.characterRows) > 0 {
				m.inspectingCharacter = !m.inspectingCharacter
				return m, nil
			}
			if m.state == adminTab {
				input := m.textInput.Value()
				if input != "" {
					m.commandHistory = append(m.commandHistory, input)
					m.historyIndex = len(m.commandHistory)
					m.draftInput = ""
					m = m.executeCommand(input)
					m.textInput.SetValue("")
					m.tabCandidates = nil
					m.tabPrefix = ""
					m.tabCycleIndex = 0
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
		m.temperature = msg.temperature
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

	if m.state == adminTab || (m.state == recipesTab && m.recipeEditor) || (m.state == itemsTab && m.itemEditor) || ((m.state == locationsTab || m.state == mapTab) && m.locationEditor) || (m.state == backupTab && (m.backupAction == backupExportPath || m.backupAction == backupImportPath)) {
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
	tick        uint64
	weather     string
	temperature float64
}

func (m Model) refreshStats() tea.Cmd {
	return tea.Tick(time.Second*1, func(t time.Time) tea.Msg {
		var tick uint64
		var weather string = "clear"
		var temperature float64 = 20.0
		if m.provider != nil {
			tick = m.provider.GetTickCount()
			weather = m.provider.GetWeather()
			temperature = m.provider.GetTemperature()
		}
		return tickMsg{tick: tick, weather: weather, temperature: temperature}
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
			"  temp <celsius> - set environmental temperature in degrees Celsius\n" +
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
			"  full-heal <character_id> - restore health and hunger to full\n" +
			"  create-item <id> <name> <weight_kg> <description> [--consumable <hunger_restore>] [--container <capacity_kg> <slot_limit>]\n" +
			"  set-resource <loc_id> <resource_id> <current_stock> <max_stock> <replenish_per_day>\n" +
			"  set-shop-item <loc_id> <item_id> <price_gold> <stock_quantity>\n" +
			"  create-location <id> <name> <description> - creates a public, all-day location\n" +
			"  connect <from_loc> <to_loc> <travel_ticks> - adds a bidirectional connection\n" +
			"  create-character <id> <name> <occupation> <location_id> <persona> - creates an agent and generates an MQTT password\n" +
			"  move <character_id> <location_id> - move an idle character to a location\n" +
			"  teach-recipe <character_id> <recipe_id> - teach a recipe to a character\n" +
			"  set-skill <character_id> <skill_id> <level> <experience> - set a character's skill level and total XP\n" +
			"Quote names and descriptions containing spaces; use backslash escapes inside quotes.\n"
	case "weather":
		if len(args) < 1 {
			err = fmt.Errorf("usage: weather <type>")
		} else {
			err = m.admin.SetWeather(args[0])
			output = fmt.Sprintf("Weather set to %s", args[0])
		}
	case "temp", "temperature":
		if len(args) < 1 {
			err = fmt.Errorf("usage: temp <celsius>")
		} else {
			err = m.admin.SetTemperature(args[0])
			if err == nil {
				output = fmt.Sprintf("Temperature set to %s°C", args[0])
			}
		}
	case "set-desc":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-desc <loc_id> <description>")
		} else {
			err = m.admin.SetLocationDescription(args[0], strings.Join(args[1:], " "))
			output = fmt.Sprintf("Description updated for %s", args[0])
		}
	case "set-name":
		if len(args) < 2 {
			err = fmt.Errorf("usage: set-name <loc_id> <name>")
		} else {
			err = m.admin.SetLocationName(args[0], strings.Join(args[1:], " "))
			output = fmt.Sprintf("Name updated for %s", args[0])
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
	case "full-heal":
		if len(args) != 1 {
			err = fmt.Errorf("usage: full-heal <character_id>")
		} else {
			err = m.admin.FullHealCharacter(args[0])
			if err == nil {
				output = fmt.Sprintf("Restored health and hunger for %s", args[0])
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
	case "move":
		if len(args) != 2 {
			err = fmt.Errorf("usage: move <character_id> <location_id>")
		} else {
			err = m.admin.MoveCharacter(args[0], args[1])
			if err == nil {
				output = fmt.Sprintf("Moved %s to %s", args[0], args[1])
			}
		}
	case "teach-recipe":
		if len(args) != 2 {
			err = fmt.Errorf("usage: teach-recipe <character_id> <recipe_id>")
		} else {
			err = m.admin.TeachRecipe(args[0], args[1])
			if err == nil {
				output = fmt.Sprintf("Taught %s to %s", args[1], args[0])
			}
		}
	case "set-skill":
		if len(args) != 4 {
			err = fmt.Errorf("usage: set-skill <character_id> <skill_id> <level> <experience>")
		} else {
			level, levelErr := strconv.Atoi(args[2])
			experience, experienceErr := strconv.Atoi(args[3])
			if levelErr != nil || experienceErr != nil {
				err = fmt.Errorf("level and experience must be integers")
			} else {
				err = m.admin.SetCharacterSkill(args[0], args[1], level, experience)
				if err == nil {
					output = fmt.Sprintf("Set %s's %s skill to level %d (%d total XP)", args[0], args[1], level, experience)
				}
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

	tabs := []string{"1. Status", "2. Locations", "3. Characters", "4. Admin Console", "5. Logs", "6. Recipes", "7. Items", "8. Backup", "9. Map"}
	var tabStrs []string
	for i, t := range tabs {
		if int(m.state) == i {
			tabStrs = append(tabStrs, fmt.Sprintf("[%s]", t))
		} else {
			tabStrs = append(tabStrs, fmt.Sprintf(" %s ", t))
		}
	}
	sb.WriteString(strings.Join(tabStrs, "  ") + "\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")

	switch m.state {
	case statusTab:
		sb.WriteString(fmt.Sprintf("Server Master Tick: %d\n", m.tickCount))
		sb.WriteString(fmt.Sprintf("Current Weather:    %s\n", m.weather))
		sb.WriteString(fmt.Sprintf("Current Temp:       %.1f°C\n", m.temperature))
		sb.WriteString(fmt.Sprintf("Uptime:             %v\n", time.Since(m.startTime).Truncate(time.Second)))
		sb.WriteString(fmt.Sprintf("Recent log lines:    %d (open Logs tab)\n", len(m.logLines)))
		sb.WriteString("\n[Instructions]\n- Tab / Shift+Tab: switch tabs; 1–9: jump directly.\n- ?: open Admin Console help; q: quit; Ctrl+C: quit.\n")

	case locationsTab:
		m.refreshLocationRows()
		m.renderLocations(&sb)

	case charactersTab:
		m.renderCharacters(&sb)

	case recipesTab:
		m.refreshRecipeRows()
		m.renderRecipes(&sb)

	case itemsTab:
		m.refreshItemRows()
		m.renderItems(&sb)

	case backupTab:
		m.renderBackup(&sb)

	case mapTab:
		m.refreshLocationRows()
		m.renderMap(&sb)

	case adminTab:
		sb.WriteString("Admin Console Log & Output:\n")
		sb.WriteString(m.viewport.View() + "\n\n")
		if m.sensitiveOutput != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.sensitiveOutput) + "\n")
		}

		// Dynamic Syntax Helper & Tab Completion hint
		currentInput := m.textInput.Value()
		fields, _ := parseCommandLine(currentInput)
		syntaxHint := "Type 'help' for available commands (Weather: clear|cloudy|drought|fog|frost|hail|heatwave|rain|snow|storm|windy). Press Tab to autocomplete, Up/Down for history."
		if len(fields) > 0 {
			cmdName := fields[0]
			if syntax, ok := commandSyntaxes[cmdName]; ok {
				syntaxHint = "Syntax: " + syntax
			} else {
				syntaxHint = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Unknown command. Type 'help' for available commands.")
			}
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(syntaxHint) + "\n")
		sb.WriteString(m.textInput.View() + "\n")

		if len(m.tabCandidates) > 1 {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(fmt.Sprintf("Matches (%d): %s\n", len(m.tabCandidates), strings.Join(m.tabCandidates, ", "))))
		}

	case logsTab:
		sb.WriteString(fmt.Sprintf("Server Logs (most recent %d lines; scroll with PgUp/PgDn)\n\n", maxDisplayedLogLines))
		sb.WriteString(m.logViewport.View())
	}

	sb.WriteString("\n\nTab/Shift+Tab navigate • 1–8 jump • ? help • q quit")

	return sb.String()
}

func (m *Model) renderCharacters(sb *strings.Builder) {
	m.characterRows = nil
	if m.db == nil {
		sb.WriteString("Character data unavailable: database is not connected.\n")
		return
	}
	rows, err := m.db.Query(`SELECT id, name, occupation, current_location, gold, hunger, energy, health,
		status, is_online, controller_type, COALESCE(action_target, ''), COALESCE(action_end_tick, 0)
		FROM characters ORDER BY name, id`)
	if err != nil {
		sb.WriteString("Could not load characters: " + err.Error() + "\n")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var character characterSummary
		if err := rows.Scan(&character.id, &character.name, &character.occupation, &character.location, &character.gold,
			&character.hunger, &character.energy, &character.health, &character.status,
			&character.online, &character.controller, &character.actionTarget, &character.actionEndTick); err == nil {
			m.characterRows = append(m.characterRows, character)
		}
	}
	if err := rows.Err(); err != nil {
		sb.WriteString("Could not load characters: " + err.Error() + "\n")
		return
	}
	if len(m.characterRows) == 0 {
		sb.WriteString("No characters found.\n")
		return
	}
	if m.selectedCharacter >= len(m.characterRows) {
		m.selectedCharacter = len(m.characterRows) - 1
	}
	if m.selectedCharacter < 0 {
		m.selectedCharacter = 0
	}

	if !m.inspectingCharacter {
		sb.WriteString("Characters — use ↑/↓ to select, Enter to inspect\n\n")
		for i, character := range m.characterRows {
			marker := "  "
			if i == m.selectedCharacter {
				marker = "› "
			}
			presence := "offline"
			if character.online {
				presence = "online"
			}
			line := fmt.Sprintf("%s%-28s %-16s @ %-20s %s", marker, character.name, character.status, character.location, presence)
			if i == m.selectedCharacter {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		return
	}

	character := m.characterRows[m.selectedCharacter]
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(character.name) + "  (" + character.id + ")\n\n")
	sb.WriteString(fmt.Sprintf("Occupation: %s\nLocation:   %s\nStatus:     %s\nPresence:   %s\nController: %s\n",
		character.occupation, character.location, character.status, onlineLabel(character.online), character.controller))
	sb.WriteString(fmt.Sprintf("Health: %d   Hunger: %d   Energy: %d   Gold: %d\n", character.health, character.hunger, character.energy, character.gold))
	if character.actionTarget != "" {
		sb.WriteString(fmt.Sprintf("Action target: %s   Action ends at tick: %d\n", character.actionTarget, character.actionEndTick))
	}

	sb.WriteString("\nInventory\n---------\n")
	inventory, err := m.db.Query(`SELECT i.name, inv.item_id, inv.quantity FROM inventories inv
		LEFT JOIN items i ON i.id = inv.item_id WHERE inv.character_id = ? AND inv.quantity > 0 ORDER BY i.name, inv.item_id`, character.id)
	if err != nil {
		sb.WriteString("Could not load inventory: " + err.Error() + "\n")
	} else {
		count := 0
		for inventory.Next() {
			var name, id string
			var quantity int
			if inventory.Scan(&name, &id, &quantity) == nil {
				if name == "" {
					name = id
				}
				sb.WriteString(fmt.Sprintf("• %s (%s) × %d\n", name, id, quantity))
				count++
			}
		}
		if inventory.Err() != nil {
			sb.WriteString("Could not finish loading inventory: " + inventory.Err().Error() + "\n")
		} else if count == 0 {
			sb.WriteString("(empty)\n")
		}
		inventory.Close()
	}

	sb.WriteString("\nSkills\n------\n")
	writeCharacterPairs(sb, m.db, `SELECT skill_id, printf('Level %d · %d XP', level, experience)
		FROM character_skills WHERE character_id = ? ORDER BY skill_id`, character.id, "(none recorded)")
	sb.WriteString("\nKnown recipes\n-------------\n")
	writeCharacterPairs(sb, m.db, `SELECT COALESCE(r.name, cr.recipe_id), cr.recipe_id
		FROM character_known_recipes cr LEFT JOIN recipes r ON r.id = cr.recipe_id
		WHERE cr.character_id = ? ORDER BY r.name, cr.recipe_id`, character.id, "(none)")
	sb.WriteString("\n↑/↓ select another character • Esc return to list")
}

func (m *Model) renderMap(sb *strings.Builder) {
	if m.db == nil {
		sb.WriteString("Map data unavailable: database is not connected.\n")
		return
	}
	if len(m.locationRows) == 0 {
		sb.WriteString("No locations found in the village.\n")
		return
	}
	if m.selectedLocation >= len(m.locationRows) {
		m.selectedLocation = len(m.locationRows) - 1
	}
	if m.selectedLocation < 0 {
		m.selectedLocation = 0
	}
	if m.locationEditor {
		mode := "Add location"
		if m.locationEditing {
			mode = "Edit location"
		}
		sb.WriteString(mode + "\n\nFields: id, quoted name, quoted description, owner|none, public true|false, indoor true|false, access ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY, open time HH:MM, close time HH:MM, quoted rules|none\n")
		sb.WriteString("Example: tavern \"The Tavern\" \"A warm cozy tavern.\" none true false ALL 08:00 02:00 \"Quiet after closing.\"\nEsc cancels.\n\n")
		sb.WriteString(m.textInput.View() + "\n")
		if m.locationStatus != "" {
			sb.WriteString(m.locationStatus + "\n")
		}
		return
	}

	if !m.inspectingLocation {
		sb.WriteString("Village Spatial Map & Topology — use ↑/↓ to select, Enter to inspect connections & occupants, a add, e edit\n\n")
		for i, loc := range m.locationRows {
			marker := "  "
			if i == m.selectedLocation {
				marker = "› "
			}
			var charCount int
			_ = m.db.QueryRow("SELECT COUNT(*) FROM characters WHERE current_location = ?", loc.id).Scan(&charCount)

			kind := "Outdoor"
			if loc.indoor {
				kind = "Indoor"
			}
			line := fmt.Sprintf("%s%-26s [%-7s] — Occupants: %d", marker, loc.name, kind, charCount)
			if i == m.selectedLocation {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		return
	}

	loc := m.locationRows[m.selectedLocation]
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(loc.name) + "  (" + loc.id + ")\n\n")
	kind, visibility := "Outdoor", "Private"
	if loc.indoor {
		kind = "Indoor"
	}
	if loc.public {
		visibility = "Public"
	}
	sb.WriteString(fmt.Sprintf("Type: %s   Property: %s   Owner: %s\n", kind, visibility, displayOr(loc.owner, "None")))
	if loc.description != "" {
		sb.WriteString("\nDescription\n-----------\n" + loc.description + "\n")
	}

	sb.WriteString("\nCharacters at this location:\n")
	rows, err := m.db.Query("SELECT id, name, occupation, status, is_online FROM characters WHERE current_location = ? ORDER BY name", loc.id)
	if err == nil {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id, name, occupation, status string
			var online bool
			if rows.Scan(&id, &name, &occupation, &status, &online) == nil {
				onlineStr := "offline"
				if online {
					onlineStr = "online"
				}
				sb.WriteString(fmt.Sprintf("  • %s (%s) — %s [%s, status: %s]\n", name, id, occupation, onlineStr, displayOr(status, "idle")))
				count++
			}
		}
		if count == 0 {
			sb.WriteString("  (none)\n")
		}
	}

	sb.WriteString("\nConnected Neighbor Locations (Travel Routes):\n")
	connRows, err := m.db.Query(`SELECT c.to_location_id, COALESCE(l.name, c.to_location_id), c.travel_ticks 
		FROM location_connections c LEFT JOIN locations l ON l.id = c.to_location_id 
		WHERE c.from_location_id = ? ORDER BY c.travel_ticks, l.name`, loc.id)
	if err == nil {
		defer connRows.Close()
		cCount := 0
		for connRows.Next() {
			var toID, toName string
			var ticks int
			if connRows.Scan(&toID, &toName, &ticks) == nil {
				sb.WriteString(fmt.Sprintf("  → %s (%s) — %d ticks travel\n", toName, toID, ticks))
				cCount++
			}
		}
		if cCount == 0 {
			sb.WriteString("  (none connected)\n")
		}
	}

	sb.WriteString("\n↑/↓ select another location • Esc return to map overview")
}

func (m *Model) renderLocations(sb *strings.Builder) {
	if m.db == nil {
		sb.WriteString("Location data unavailable: database is not connected.\n")
		return
	}
	if len(m.locationRows) == 0 {
		var count int
		if err := m.db.QueryRow("SELECT COUNT(*) FROM locations").Scan(&count); err != nil {
			sb.WriteString("Could not load locations: " + err.Error() + "\n")
		} else {
			sb.WriteString("No locations found.\n")
		}
		return
	}
	if m.selectedLocation >= len(m.locationRows) {
		m.selectedLocation = len(m.locationRows) - 1
	}
	if m.selectedLocation < 0 {
		m.selectedLocation = 0
	}
	if m.locationEditor {
		mode := "Add location"
		if m.locationEditing {
			mode = "Edit location"
		}
		sb.WriteString(mode + "\n\nFields: id, quoted name, quoted description, owner|none, public true|false, indoor true|false, access ALL|FOOT_AND_WHEELBARROW|FOOT_ONLY, open time HH:MM, close time HH:MM, quoted rules|none\n")
		sb.WriteString("Example: tavern \"The Tavern\" \"A warm cozy tavern.\" none true false ALL 08:00 02:00 \"Quiet after closing.\"\nEsc cancels.\n\n")
		sb.WriteString(m.textInput.View() + "\n")
		if m.locationStatus != "" {
			sb.WriteString(m.locationStatus + "\n")
		}
		return
	}
	if !m.inspectingLocation {
		sb.WriteString("Locations — ↑/↓ select, Enter inspect, a add, e edit\n\n")
		for i, location := range m.locationRows {
			marker := "  "
			if i == m.selectedLocation {
				marker = "› "
			}
			kind, visibility := "Outdoor", "Private"
			if location.indoor {
				kind = "Indoor"
			}
			if location.public {
				visibility = "Public"
			}
			line := fmt.Sprintf("%s%-28s %-8s %-8s Owner: %s", marker, location.name, kind, visibility, displayOr(location.owner, "None"))
			if i == m.selectedLocation {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		return
	}

	location := m.locationRows[m.selectedLocation]
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(location.name) + "  (" + location.id + ")\n\n")
	kind, visibility := "Outdoor", "Private"
	if location.indoor {
		kind = "Indoor"
	}
	if location.public {
		visibility = "Public"
	}
	hours := "Not specified"
	if location.openTime != "" || location.closeTime != "" {
		hours = displayOr(location.openTime, "?") + "–" + displayOr(location.closeTime, "?")
	}
	sb.WriteString(fmt.Sprintf("Type: %s   Property: %s   Owner: %s\nAccess: %s   Hours: %s\n", kind, visibility,
		displayOr(location.owner, "None"), location.access, hours))
	sb.WriteString("\nDescription\n-----------\n" + displayOr(location.description, "(none)") + "\n")
	if location.rules != "" {
		sb.WriteString("\nRules\n-----\n" + location.rules + "\n")
	}

	sb.WriteString("\nConnections\n-----------\n")
	writeLocationRows(sb, m.db, `SELECT c.to_location_id, COALESCE(l.name, c.to_location_id), c.travel_ticks FROM location_connections c
		LEFT JOIN locations l ON l.id = c.to_location_id WHERE c.from_location_id = ? ORDER BY l.name, c.to_location_id`, location.id,
		func(id, name string, ticks int) string {
			return fmt.Sprintf("• %s (%s) — %d ticks", name, id, ticks)
		}, "(none)")
	sb.WriteString("\nResources\n---------\n")
	writeLocationRows(sb, m.db, `SELECT resource_id, '', current_stock FROM location_resources WHERE location_id = ? ORDER BY resource_id`, location.id,
		func(resource, _ string, stock int) string {
			return fmt.Sprintf("• %s — %d in stock", resource, stock)
		}, "(none)")
	sb.WriteString("\nShop listings\n-------------\n")
	writeLocationRows(sb, m.db, `SELECT si.item_id, COALESCE(i.name, si.item_id), si.stock_quantity FROM location_shop_items si
		LEFT JOIN items i ON i.id = si.item_id WHERE si.location_id = ? ORDER BY i.name, si.item_id`, location.id,
		func(id, name string, stock int) string {
			return fmt.Sprintf("• %s (%s) — stock %d", name, id, stock)
		}, "(none)")
	sb.WriteString("\nWorkstations\n------------\n")
	writeLocationRows(sb, m.db, `SELECT name, crafting_domain, 0 FROM workstations WHERE location_id = ? ORDER BY name, id`, location.id,
		func(name, domain string, _ int) string { return fmt.Sprintf("• %s — %s", name, domain) }, "(none)")
	sb.WriteString("\nGround items\n------------\n")
	writeLocationRows(sb, m.db, `SELECT li.item_id, COALESCE(i.name, li.item_id), li.quantity FROM location_items li
		LEFT JOIN items i ON i.id = li.item_id WHERE li.location_id = ? ORDER BY i.name, li.item_id`, location.id,
		func(id, name string, quantity int) string {
			return fmt.Sprintf("• %s (%s) × %d", name, id, quantity)
		}, "(none)")
	sb.WriteString("\nFarm plots\n----------\n")
	writeLocationRows(sb, m.db, `SELECT fp.id, COALESCE(fc.name, 'Unplanted'), fp.mature_tick FROM farm_plots fp
		LEFT JOIN farm_crops fc ON fc.id = fp.crop_id WHERE fp.location_id = ? ORDER BY fp.id`, location.id,
		func(id, crop string, mature int) string {
			if crop == "Unplanted" {
				return fmt.Sprintf("• %s — %s", id, crop)
			}
			return fmt.Sprintf("• %s — %s, matures at tick %d", id, crop, mature)
		}, "(none)")
	sb.WriteString("\n↑/↓ select another location • Esc return to list • e edit")
}

func writeLocationRows(sb *strings.Builder, database *sql.DB, query, locationID string, format func(string, string, int) string, emptyText string) {
	rows, err := database.Query(query, locationID)
	if err != nil {
		sb.WriteString("Could not load data: " + err.Error() + "\n")
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var first, second string
		var number int
		if rows.Scan(&first, &second, &number) == nil {
			sb.WriteString(format(first, second, number) + "\n")
			count++
		}
	}
	if err := rows.Err(); err != nil {
		sb.WriteString("Could not finish loading data: " + err.Error() + "\n")
	} else if count == 0 {
		sb.WriteString(emptyText + "\n")
	}
}

func displayOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (m *Model) refreshRecipeRows() {
	if m.db == nil {
		m.recipeRows = nil
		return
	}
	rows, err := m.db.Query("SELECT id FROM recipes ORDER BY name, id")
	if err != nil {
		m.recipeRows = nil
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	m.recipeRows = nil
	for _, id := range ids {
		if recipe, err := worlddb.GetRecipe(m.db, id); err == nil {
			m.recipeRows = append(m.recipeRows, *recipe)
		}
	}
}

func (m *Model) renderRecipes(sb *strings.Builder) {
	if m.db == nil {
		sb.WriteString("Recipe data unavailable: database is not connected.\n")
		return
	}
	if m.selectedRecipe >= len(m.recipeRows) {
		m.selectedRecipe = len(m.recipeRows) - 1
	}
	if m.selectedRecipe < 0 {
		m.selectedRecipe = 0
	}
	if m.recipeDeletePrompt && len(m.recipeRows) > 0 {
		sb.WriteString(fmt.Sprintf("Delete recipe %q? This also removes it from characters' known recipes. Press y to delete, n or Esc to cancel.\n", m.recipeRows[m.selectedRecipe].Name))
		return
	}
	if m.recipeEditor {
		mode := "Add recipe"
		if m.recipeEditing {
			mode = "Edit recipe"
		}
		sb.WriteString(mode + "\n\nFields: id, quoted name, output item id, output qty, duration ticks, energy cost, station|none, skill|none, min skill, ingredients item:qty,...\n")
		sb.WriteString("Example: mill_flour \"Mill Flour\" flour 1 5 2 millstones milling 1 wheat:2\nEsc cancels.\n\n")
		sb.WriteString(m.textInput.View() + "\n")
		if m.recipeStatus != "" {
			sb.WriteString(m.recipeStatus + "\n")
		}
		return
	}
	if m.recipeStatus != "" {
		sb.WriteString(m.recipeStatus + "\n\n")
	}
	if len(m.recipeRows) == 0 {
		sb.WriteString("No recipes found. Press a to add one.\n")
		return
	}
	if !m.inspectingRecipe {
		sb.WriteString("Recipes — ↑/↓ select, Enter inspect, a add, e edit, d delete\n\n")
		for i, recipe := range m.recipeRows {
			marker := "  "
			if i == m.selectedRecipe {
				marker = "› "
			}
			line := fmt.Sprintf("%s%-32s → %dx %s", marker, recipe.Name, recipe.OutputQuantity, recipe.OutputItemID)
			if i == m.selectedRecipe {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		return
	}
	recipe := m.recipeRows[m.selectedRecipe]
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(recipe.Name) + "  (" + recipe.ID + ")\n\n")
	sb.WriteString(fmt.Sprintf("Output: %d × %s\nDuration: %d ticks   Energy cost: %d\nStation: %s\n",
		recipe.OutputQuantity, recipe.OutputItemID, recipe.DurationTicks, recipe.EnergyCost,
		displayOr(recipe.RequiredStation, "Anywhere")))
	if recipe.RequiredSkillID == "" {
		sb.WriteString("Skill: None\n")
	} else {
		sb.WriteString(fmt.Sprintf("Skill: %s (level %d minimum)\n", recipe.RequiredSkillID, recipe.MinSkillLevel))
	}
	sb.WriteString("\nIngredients\n-----------\n")
	if len(recipe.Ingredients) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, ingredient := range recipe.Ingredients {
		sb.WriteString(fmt.Sprintf("• %s × %d\n", ingredient.ItemID, ingredient.Quantity))
	}
	sb.WriteString("\n↑/↓ select another recipe • Esc return to list • e edit • d delete")
}

func (m *Model) refreshItemRows() {
	if m.db == nil {
		m.itemRows = nil
		return
	}
	rows, err := m.db.Query(`SELECT id, name, COALESCE(description, ''), weight_kg, is_consumable,
		hunger_restore, health_restore, is_container, capacity_kg, slot_limit, is_perishable,
		shelf_life_ticks, COALESCE(spoilage_item_id, '') FROM items ORDER BY name, id`)
	if err != nil {
		m.itemRows = nil
		return
	}
	defer rows.Close()
	m.itemRows = nil
	for rows.Next() {
		var item admin.ItemDefinition
		if rows.Scan(&item.ID, &item.Name, &item.Description, &item.WeightKg, &item.IsConsumable,
			&item.HungerRestore, &item.HealthRestore, &item.IsContainer, &item.CapacityKg,
			&item.SlotLimit, &item.IsPerishable, &item.ShelfLifeTicks, &item.SpoilageItemID) == nil {
			m.itemRows = append(m.itemRows, item)
		}
	}
}

func (m *Model) renderItems(sb *strings.Builder) {
	if m.db == nil {
		sb.WriteString("Item data unavailable: database is not connected.\n")
		return
	}
	if m.selectedItem >= len(m.itemRows) {
		m.selectedItem = len(m.itemRows) - 1
	}
	if m.selectedItem < 0 {
		m.selectedItem = 0
	}
	if m.itemDeletePrompt && len(m.itemRows) > 0 {
		sb.WriteString(fmt.Sprintf("Delete item %q? Deletion is blocked while it is used by inventory, recipes, shops, or farm crops. Press y to delete, n or Esc to cancel.\n", m.itemRows[m.selectedItem].Name))
		return
	}
	if m.itemEditor {
		mode := "Add item"
		if m.itemEditing {
			mode = "Edit item"
		}
		sb.WriteString(mode + "\n\nFields: id, quoted name, weight kg, quoted description, hunger restore, health restore, consumable true|false, container true|false, capacity kg, slots, perishable true|false, shelf life ticks, spoilage item id|none\n")
		sb.WriteString("Example: bread \"Fresh Bread\" 0.3 \"Restores hunger\" 25 0 true false 0 0 true 96 rotting_waste\nEsc cancels.\n\n")
		sb.WriteString(m.textInput.View() + "\n")
		if m.itemStatus != "" {
			sb.WriteString(m.itemStatus + "\n")
		}
		return
	}
	if m.itemStatus != "" {
		sb.WriteString(m.itemStatus + "\n\n")
	}
	if len(m.itemRows) == 0 {
		sb.WriteString("No items found. Press a to add one.\n")
		return
	}
	if !m.inspectingItem {
		sb.WriteString("Items — ↑/↓ select, Enter inspect, a add, e edit, d delete\n\n")
		for i, item := range m.itemRows {
			marker := "  "
			if i == m.selectedItem {
				marker = "› "
			}
			line := fmt.Sprintf("%s%-30s (%s)  %.2f kg", marker, item.Name, item.ID, item.WeightKg)
			if i == m.selectedItem {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		return
	}
	item := m.itemRows[m.selectedItem]
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(item.Name) + "  (" + item.ID + ")\n\n")
	sb.WriteString(fmt.Sprintf("Description: %s\nWeight: %.2f kg\nConsumable: %t (hunger +%d, health +%d)\nContainer: %t (%.2f kg capacity, %d slots)\n",
		displayOr(item.Description, "(none)"), item.WeightKg, item.IsConsumable, item.HungerRestore,
		item.HealthRestore, item.IsContainer, item.CapacityKg, item.SlotLimit))
	sb.WriteString(fmt.Sprintf("Perishable: %t", item.IsPerishable))
	if item.IsPerishable {
		sb.WriteString(fmt.Sprintf(" (shelf life %d ticks, spoils into %s)", item.ShelfLifeTicks, displayOr(item.SpoilageItemID, "unspecified")))
	}
	sb.WriteString("\n\n↑/↓ select another item • Esc return to list • e edit • d delete")
}

func (m *Model) renderBackup(sb *strings.Builder) {
	sb.WriteString("CSV world-state backup\n\n")
	sb.WriteString("Exports one CSV file per database table to a directory. The backup includes character account credentials; keep it private.\n")
	sb.WriteString("Import replaces the current persisted world state and should be followed by a server restart to synchronize live sessions.\n\n")
	switch m.backupAction {
	case backupExportPath:
		sb.WriteString("Export directory path, then press Enter:\n")
		sb.WriteString(m.textInput.View() + "\n")
	case backupImportPath:
		sb.WriteString("Import directory path, then press Enter:\n")
		sb.WriteString(m.textInput.View() + "\n")
	case backupImportConfirm:
		sb.WriteString(fmt.Sprintf("Import from %s? This replaces all persisted world tables. Press y to import, n or Esc to cancel.\n", m.backupPath))
	default:
		sb.WriteString("Press e to export a backup or i to import one. Default directory: agentmud-backup\n")
	}
	if m.backupStatus != "" {
		sb.WriteString("\n" + m.backupStatus + "\n")
	}
}

func (m Model) restoreCSVBackup() Model {
	if err := worlddb.ImportCSV(m.db, m.backupPath); err != nil {
		m.backupStatus = "Import failed; current database was left unchanged: " + err.Error()
		return m
	}
	tick, err := worlddb.GetServerTick(m.db)
	if err != nil {
		m.backupStatus = "Import completed, but could not read restored server tick: " + err.Error()
		return m
	}
	m.tickCount = tick
	if setter, ok := m.provider.(interface{ SetTickCount(uint64) }); ok {
		setter.SetTickCount(tick)
	}
	m.backupStatus = fmt.Sprintf("CSV backup restored from %s at tick %d. Restart the server to synchronize active sessions.", m.backupPath, tick)
	return m
}

func itemEditorValue(item admin.ItemDefinition) string {
	return fmt.Sprintf("%s %s %.4g %s %d %d %t %t %.4g %d %t %d %s",
		item.ID, strconv.Quote(item.Name), item.WeightKg, strconv.Quote(item.Description), item.HungerRestore,
		item.HealthRestore, item.IsConsumable, item.IsContainer, item.CapacityKg, item.SlotLimit,
		item.IsPerishable, item.ShelfLifeTicks, displayOr(item.SpoilageItemID, "none"))
}

func (m Model) saveItemEditor() Model {
	parts, err := parseCommandLine(m.textInput.Value())
	if err != nil {
		m.itemStatus = "Invalid item: " + err.Error()
		return m
	}
	if len(parts) != 13 {
		m.itemStatus = "Enter all 13 fields. Quote names and descriptions that contain spaces."
		return m
	}
	item := admin.ItemDefinition{ID: parts[0], Name: parts[1], Description: parts[3]}
	if m.itemEditing && m.selectedItem < len(m.itemRows) && item.ID != m.itemRows[m.selectedItem].ID {
		m.itemStatus = "Item ids cannot be changed while editing; create a new item instead."
		return m
	}
	if item.WeightKg, err = strconv.ParseFloat(parts[2], 64); err != nil {
		m.itemStatus = "Weight must be a number."
		return m
	}
	intFields := []struct {
		index  int
		target *int
	}{{4, &item.HungerRestore}, {5, &item.HealthRestore}, {9, &item.SlotLimit}, {11, &item.ShelfLifeTicks}}
	for _, field := range intFields {
		if *field.target, err = strconv.Atoi(parts[field.index]); err != nil {
			m.itemStatus = fmt.Sprintf("Invalid integer %q.", parts[field.index])
			return m
		}
	}
	if item.IsConsumable, err = strconv.ParseBool(parts[6]); err != nil {
		m.itemStatus = "Consumable must be true or false."
		return m
	}
	if item.IsContainer, err = strconv.ParseBool(parts[7]); err != nil {
		m.itemStatus = "Container must be true or false."
		return m
	}
	if item.CapacityKg, err = strconv.ParseFloat(parts[8], 64); err != nil {
		m.itemStatus = "Container capacity must be a number."
		return m
	}
	if item.IsPerishable, err = strconv.ParseBool(parts[10]); err != nil {
		m.itemStatus = "Perishable must be true or false."
		return m
	}
	if !strings.EqualFold(parts[12], "none") {
		item.SpoilageItemID = parts[12]
	}
	if m.admin == nil {
		m.itemStatus = "Item editor unavailable: admin executor is not connected."
		return m
	}
	if m.itemEditing {
		err = m.admin.UpdateItem(item)
	} else {
		err = m.admin.CreateItem(item)
	}
	if err != nil {
		m.itemStatus = "Save failed: " + err.Error()
		return m
	}
	if m.itemEditing {
		m.itemStatus = "Item updated."
	} else {
		m.itemStatus = "Item added."
	}
	m.itemEditor = false
	m.itemEditing = false
	m.textInput.Placeholder = "Enter admin command..."
	m.textInput.SetValue("")
	m.refreshItemRows()
	return m
}

func recipeEditorValue(recipe worlddb.Recipe) string {
	station, skill := displayOr(recipe.RequiredStation, "none"), displayOr(recipe.RequiredSkillID, "none")
	var ingredients []string
	for _, ingredient := range recipe.Ingredients {
		ingredients = append(ingredients, fmt.Sprintf("%s:%d", ingredient.ItemID, ingredient.Quantity))
	}
	return fmt.Sprintf("%s %s %s %d %d %d %s %s %d %s", recipe.ID, strconv.Quote(recipe.Name), recipe.OutputItemID,
		recipe.OutputQuantity, recipe.DurationTicks, recipe.EnergyCost, station, skill, recipe.MinSkillLevel, strings.Join(ingredients, ","))
}

func locationEditorValue(location locationSummary) string {
	owner, rules := displayOr(location.owner, "none"), displayOr(location.rules, "none")
	return fmt.Sprintf("%s %s %s %s %t %t %s %s %s %s", location.id,
		strconv.Quote(location.name), strconv.Quote(location.description), owner,
		location.public, location.indoor, location.access, location.openTime,
		location.closeTime, strconv.Quote(rules))
}

func (m Model) saveLocationEditor() Model {
	parts, err := parseCommandLine(m.textInput.Value())
	if err != nil {
		m.locationStatus = "Invalid location: " + err.Error()
		return m
	}
	if len(parts) != 10 {
		m.locationStatus = "Enter all 10 fields. Quote the name, description, and rules; use none for owner or rules."
		return m
	}
	location := admin.LocationDefinition{ID: parts[0], Name: parts[1], Description: parts[2], AccessTier: parts[6], OpenTime: parts[7], CloseTime: parts[8]}
	if !strings.EqualFold(parts[3], "none") {
		location.OwnerID = parts[3]
	}
	if location.IsPublic, err = strconv.ParseBool(parts[4]); err != nil {
		m.locationStatus = "Public must be true or false."
		return m
	}
	if location.IsIndoor, err = strconv.ParseBool(parts[5]); err != nil {
		m.locationStatus = "Indoor must be true or false."
		return m
	}
	if !strings.EqualFold(parts[9], "none") {
		location.Rules = parts[9]
	}
	if m.locationEditing && m.selectedLocation < len(m.locationRows) && location.ID != m.locationRows[m.selectedLocation].id {
		m.locationStatus = "Location ids cannot be changed while editing; create a new location instead."
		return m
	}
	if m.admin == nil {
		m.locationStatus = "Location editor unavailable: admin executor is not connected."
		return m
	}
	if m.locationEditing {
		err = m.admin.UpdateLocation(location)
	} else {
		err = m.admin.CreateLocationObj(location)
	}
	if err != nil {
		m.locationStatus = "Save failed: " + err.Error()
		return m
	}
	if m.locationEditing {
		m.locationStatus = "Location updated."
	} else {
		m.locationStatus = "Location added."
	}
	m.locationEditor = false
	m.locationEditing = false
	m.textInput.Placeholder = "Enter admin command..."
	m.textInput.SetValue("")
	m.refreshLocationRows()
	return m
}

func (m Model) saveRecipeEditor() Model {
	parts, err := parseCommandLine(m.textInput.Value())
	if err != nil {
		m.recipeStatus = "Invalid recipe: " + err.Error()
		return m
	}
	if len(parts) != 10 {
		m.recipeStatus = "Enter all 10 fields. Ingredients use item_id:quantity,item_id:quantity."
		return m
	}
	recipe := admin.RecipeDefinition{ID: parts[0], Name: parts[1], OutputItemID: parts[2]}
	if m.recipeEditing && m.selectedRecipe < len(m.recipeRows) && recipe.ID != m.recipeRows[m.selectedRecipe].ID {
		m.recipeStatus = "Recipe ids cannot be changed while editing; create a new recipe instead."
		return m
	}
	values := []*int{&recipe.OutputQuantity, &recipe.DurationTicks, &recipe.EnergyCost, &recipe.MinSkillLevel}
	for i, index := range []int{3, 4, 5, 8} {
		*values[i], err = strconv.Atoi(parts[index])
		if err != nil {
			m.recipeStatus = "Invalid numeric field: " + err.Error()
			return m
		}
	}
	if !strings.EqualFold(parts[6], "none") {
		recipe.RequiredStation = parts[6]
	}
	if !strings.EqualFold(parts[7], "none") {
		recipe.RequiredSkillID = parts[7]
	}
	for _, part := range strings.Split(parts[9], ",") {
		fields := strings.Split(part, ":")
		if len(fields) != 2 || strings.TrimSpace(fields[0]) == "" {
			m.recipeStatus = fmt.Sprintf("Invalid ingredient %q; expected item_id:quantity.", part)
			return m
		}
		quantity, err := strconv.Atoi(fields[1])
		if err != nil {
			m.recipeStatus = fmt.Sprintf("Invalid quantity in ingredient %q.", part)
			return m
		}
		recipe.Ingredients = append(recipe.Ingredients, worlddb.RecipeIngredient{ItemID: strings.TrimSpace(fields[0]), Quantity: quantity})
	}
	if m.admin == nil {
		m.recipeStatus = "Recipe editor unavailable: admin executor is not connected."
		return m
	}
	if m.recipeEditing {
		err = m.admin.UpdateRecipe(recipe)
	} else {
		err = m.admin.CreateRecipe(recipe)
	}
	if err != nil {
		m.recipeStatus = "Save failed: " + err.Error()
		return m
	}
	if m.recipeEditing {
		m.recipeStatus = "Recipe updated."
	} else {
		m.recipeStatus = "Recipe added."
	}
	m.recipeEditor = false
	m.recipeEditing = false
	m.textInput.Placeholder = "Enter admin command..."
	m.textInput.SetValue("")
	m.refreshRecipeRows()
	return m
}

func (m *Model) refreshLocationRows() {
	if m.db == nil {
		m.locationRows = nil
		return
	}
	rows, err := m.db.Query(`SELECT id, name, description, COALESCE(owner_id, ''), is_public_property,
		is_indoor, COALESCE(vehicle_access_tier, 'ALL'), COALESCE(open_time, ''),
		COALESCE(close_time, ''), COALESCE(rules_prompt, '') FROM locations ORDER BY name, id`)
	if err != nil {
		m.locationRows = nil
		return
	}
	defer rows.Close()
	m.locationRows = nil
	for rows.Next() {
		var location locationSummary
		if rows.Scan(&location.id, &location.name, &location.description, &location.owner,
			&location.public, &location.indoor, &location.access, &location.openTime,
			&location.closeTime, &location.rules) == nil {
			m.locationRows = append(m.locationRows, location)
		}
	}
}

func (m *Model) refreshCharacterRows() {
	if m.db == nil {
		m.characterRows = nil
		return
	}
	rows, err := m.db.Query(`SELECT id, name, occupation, current_location, gold, hunger, energy, health,
		status, is_online, controller_type, COALESCE(action_target, ''), COALESCE(action_end_tick, 0)
		FROM characters ORDER BY name, id`)
	if err != nil {
		m.characterRows = nil
		return
	}
	defer rows.Close()
	m.characterRows = nil
	for rows.Next() {
		var character characterSummary
		if rows.Scan(&character.id, &character.name, &character.occupation, &character.location, &character.gold,
			&character.hunger, &character.energy, &character.health, &character.status,
			&character.online, &character.controller, &character.actionTarget, &character.actionEndTick) == nil {
			m.characterRows = append(m.characterRows, character)
		}
	}
}

func onlineLabel(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

func writeCharacterPairs(sb *strings.Builder, database *sql.DB, query, characterID, emptyText string) {
	rows, err := database.Query(query, characterID)
	if err != nil {
		sb.WriteString("Could not load data: " + err.Error() + "\n")
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var first, second string
		if rows.Scan(&first, &second) == nil {
			sb.WriteString(fmt.Sprintf("• %s: %s\n", first, second))
			count++
		}
	}
	if err := rows.Err(); err != nil {
		sb.WriteString("Could not finish loading data: " + err.Error() + "\n")
	} else if count == 0 {
		sb.WriteString(emptyText + "\n")
	}
}

func (m *Model) queryDBValues(query string) []string {
	if m.db == nil {
		return nil
	}
	rows, err := m.db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var vals []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			vals = append(vals, id)
		}
	}
	return vals
}

func (m *Model) fetchCandidatesForArg(cmdName string, argIndex int) []string {
	cmdName = strings.ToLower(cmdName)
	switch cmdName {
	case "weather":
		if argIndex == 0 {
			return []string{"clear", "cloudy", "drought", "fog", "frost", "hail", "heatwave", "rain", "snow", "storm", "windy"}
		}
	case "set-public":
		if argIndex == 1 {
			return []string{"public", "private"}
		}
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "set-indoor":
		if argIndex == 1 {
			return []string{"indoor", "outdoor"}
		}
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "set-access":
		if argIndex == 1 {
			return []string{"ALL", "FOOT_AND_WHEELBARROW", "FOOT_ONLY"}
		}
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "set-owner":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
		if argIndex == 1 {
			chars := m.queryDBValues("SELECT id FROM characters")
			return append([]string{"none"}, chars...)
		}
	case "add-item", "drop-item":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM characters")
		}
		if argIndex == 1 {
			return m.queryDBValues("SELECT id FROM items")
		}
	case "set-gold", "full-heal":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM characters")
		}
	case "set-desc", "set-name", "set-hours", "set-rules", "set-resource":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
		if cmdName == "set-resource" && argIndex == 1 {
			return []string{"wood", "stone", "water", "iron", "clay", "berries", "fish", "herbs"}
		}
	case "set-shop-item":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM locations")
		}
		if argIndex == 1 {
			return m.queryDBValues("SELECT id FROM items")
		}
	case "connect":
		if argIndex == 0 || argIndex == 1 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "create-character":
		if argIndex == 3 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "move":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM characters")
		}
		if argIndex == 1 {
			return m.queryDBValues("SELECT id FROM locations")
		}
	case "teach-recipe":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM characters")
		}
		if argIndex == 1 {
			return m.queryDBValues("SELECT id FROM recipes")
		}
	case "set-skill":
		if argIndex == 0 {
			return m.queryDBValues("SELECT id FROM characters")
		}
		if argIndex == 1 {
			return []string{"crafting", "foraging", "herbalism", "woodcutting"}
		}
	}
	return nil
}

func (m *Model) handleTabCompletion() {
	val := m.textInput.Value()
	parts := strings.Split(val, " ")
	currentPrefix := ""
	argIndex := len(parts) - 1
	if strings.HasSuffix(val, " ") {
		argIndex = len(parts)
		currentPrefix = ""
	} else if len(parts) > 0 {
		currentPrefix = parts[len(parts)-1]
	}

	var candidates []string
	if argIndex <= 0 {
		for cmd := range commandSyntaxes {
			candidates = append(candidates, cmd)
		}
	} else {
		cmdName := parts[0]
		candidates = m.fetchCandidatesForArg(cmdName, argIndex-1)
	}

	var filtered []string
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c), strings.ToLower(currentPrefix)) {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) == 0 {
		return
	}

	if m.tabPrefix != currentPrefix || len(m.tabCandidates) != len(filtered) {
		m.tabCandidates = filtered
		m.tabPrefix = currentPrefix
		m.tabCycleIndex = 0
	}

	chosen := m.tabCandidates[m.tabCycleIndex%len(m.tabCandidates)]
	m.tabCycleIndex++

	if strings.HasSuffix(val, " ") {
		parts = append(parts, chosen)
	} else {
		parts[len(parts)-1] = chosen
	}
	m.textInput.SetValue(strings.Join(parts, " "))
	m.textInput.CursorEnd()
}
