package tui

import (
	"fmt"
	"sort"
	"strings"

	"agentmud/internal/client/spatial"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	headerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("63")).
			Padding(0, 1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Background(lipgloss.Color("235")).
				Padding(0, 1)
)

func (m TUIModel) View() string {
	if m.quitting {
		return "Shutting down AgentMUD TUI...\n"
	}

	// 1. Header Information
	name := m.ctxMgr.AgentName()
	locID := m.ctxMgr.CurrentLocation()
	locName := m.ctxMgr.LocationName()
	gold := m.ctxMgr.Gold()
	energy := m.ctxMgr.Energy()
	status := m.ctxMgr.Status()
	timeStr := m.ctxMgr.LastInGameTime()
	weather := m.ctxMgr.LastWeather()

	headerText := fmt.Sprintf("Agent: %s | Location: %s (%s) | Gold: %d | Energy: %d | Status: %s | Time: %s | Weather: %s",
		name, locName, locID, gold, energy, status, timeStr, weather)
	header := headerBoxStyle.Render(titleStyle.Render("AgentMUD Client TUI") + "\n" + headerText)

	// 2. Tabs
	tabs := []string{"Sensory Log", "Inventory", "Relationships", "Self Identity"}
	var renderedTabs []string
	for i, t := range tabs {
		if i == m.tab {
			renderedTabs = append(renderedTabs, tabActiveStyle.Render(t))
		} else {
			renderedTabs = append(renderedTabs, tabInactiveStyle.Render(t))
		}
	}
	tabRow := strings.Join(renderedTabs, "  ")

	// 3. Tab Content View
	var contentBody string
	switch m.tab {
	case 0:
		contentBody = m.viewport.View()
	case 1:
		inv := m.ctxMgr.Inventory()
		if len(inv) == 0 {
			contentBody = "Inventory is empty."
		} else {
			contentBody = "=== INVENTORY ===\n" + strings.Join(inv, "\n")
		}
	case 2:
		known := m.ctxMgr.KnownCharacterIDs()
		if len(known) == 0 {
			contentBody = "No known peers recorded."
		} else {
			// Sort alphabetically by character name
			sort.Slice(known, func(i, j int) bool {
				nameI := m.ctxMgr.GetCharacterName(known[i])
				nameJ := m.ctxMgr.GetCharacterName(known[j])
				return nameI < nameJ
			})

			var peerMems []spatial.PeerMemoryContext
			if m.fetchPeerMemories != nil {
				peerMems = m.fetchPeerMemories()
			}
			memMap := make(map[string][]string)
			for _, pm := range peerMems {
				memMap[pm.PeerID] = pm.Reflections
			}

			var lines []string
			lines = append(lines, "=== KNOWN CHARACTERS & RELATIONSHIPS ===")
			for _, pid := range known {
				name := m.ctxMgr.GetCharacterName(pid)
				lines = append(lines, fmt.Sprintf("• %s (%s)", name, pid))
				refs := memMap[pid]
				if len(refs) == 0 {
					lines = append(lines, "  [No recorded memories or perceptions yet]")
				} else {
					for _, r := range refs {
						lines = append(lines, fmt.Sprintf("  - Reflection: %s", r))
					}
				}
				lines = append(lines, "")
			}
			contentBody = strings.Join(lines, "\n")
		}
	case 3:
		var lines []string
		lines = append(lines, "=== CORE BELIEFS & IDENTITY TRAITS ===")
		if m.identityLoadError != "" {
			lines = append(lines, "Unable to load identity memory: "+m.identityLoadError)
		} else if len(m.identityTraits) == 0 {
			lines = append(lines, "[No identity traits recorded yet]")
		} else {
			for _, trait := range m.identityTraits {
				lines = append(lines, fmt.Sprintf("- %s: %s (salience %d, tick %d)", trait.Key, trait.Value, trait.SalienceScore, trait.UpdatedAtTick))
			}
		}
		lines = append(lines, "", "=== RECENT PRIVATE SELF-REFLECTIONS ===")
		if m.identityLoadError != "" {
			lines = append(lines, "Reflections are unavailable until memory can be loaded.")
		} else if len(m.selfReflections) == 0 {
			lines = append(lines, "[No self-reflections recorded yet]")
		} else {
			for _, reflection := range m.selfReflections {
				lines = append(lines, fmt.Sprintf("- [Tick %d] %s", reflection.Tick, reflection.ReflectionText))
			}
		}
		contentBody = strings.Join(lines, "\n")
	}

	mainPane := paneStyle.Width(m.width - 2).Render(tabRow + "\n\n" + contentBody)

	// 4. Status / Feedback line
	var statusLine string
	if m.errMessage != "" {
		statusLine = errorStyle.Render("ERROR: " + m.errMessage)
		if m.backgroundStatus != "" {
			statusLine += "\n" + infoStyle.Render(m.backgroundStatus)
		}
	} else if m.isThinking {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Render("🧠 LLM Engine active (thinking & generating intent)...")
		if m.backgroundStatus != "" {
			statusLine += "\n" + infoStyle.Render(m.backgroundStatus)
		}
	} else if m.backgroundStatus != "" {
		statusLine = infoStyle.Render(m.backgroundStatus)
	} else if m.infoMessage != "" {
		statusLine = infoStyle.Render("INFO: " + m.infoMessage)
	} else {
		statusLine = "Type /help for slash commands."
	}

	// 5. Input Bar
	inputView := m.textInput.View()

	return fmt.Sprintf("%s\n%s\n%s\n%s", header, mainPane, statusLine, inputView)
}
