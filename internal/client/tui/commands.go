package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/model"
	"agentmud/internal/client/spatial"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *TUIModel) handleSlashCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help":
		helpText := "=== AgentMUD TUI Help ===\n" +
			"  /look (/l)            - Refresh perception (LOOK intent)\n" +
			"  /move <loc> (/m)      - Move to location ID (e.g. /move tavern)\n" +
			"  /locations (/locs)    - List all village locations\n" +
			"  /chat <msg> (/c)      - Send public chat message\n" +
			"  /suggest <hint> (/s)  - Inject guidance and trigger LLM agent cognition\n" +
			"  /set <param> <val>    - Tune parameters (probability, freq, temp, debounce)\n" +
			"  /help                 - Show this help message\n" +
			"  Tab                   - Switch tabs (Sensory Log -> Inventory -> Relationships)\n" +
			"  Ctrl+C / Esc          - Quit"
		m.ctxMgr.IngestSystem(helpText)
		return nil

	case "/locations", "/locs":
		locList := "=== Village Locations ===\n" +
			"  - town_square: Town Square (Paved heart of the village)\n" +
			"  - market: Village Market (Open-air merchant stalls)\n" +
			"  - tavern: The Boar's Tusk Tavern (Social hub & lodging)\n" +
			"  - town_mill: The Town Mill (Water-driven grain mill)\n" +
			"  - blacksmith: Forge & Anvil Blacksmith (Tools & metalwork)\n" +
			"  - bakery: Hearth & Crust Bakery (Bread & pastries)\n" +
			"  - church: St. Jude's Church (Stone sanctuary)\n" +
			"  - school: Village Schoolhouse (Youth education)\n" +
			"  - town_hall: Town Hall (Administrative & Clerk office)\n" +
			"  - lumberjack_lodge: Pine Ridge Timber Lodge (Firewood & timber)\n" +
			"  - herbalist: Rosemary's Apothecary (Herbs & remedies)\n" +
			"  - deep_forest: Deep Forest (Foraging timber & firewood)\n" +
			"  - prairie: Open Prairie (Gathering wild herbs & berries)\n" +
			"  - riverbank: Riverbank (Fishing & water access)\n" +
			"  - mine: Iron Mountain Mine (Coal & iron ore deposits)"
		m.ctxMgr.IngestSystem(locList)
		return nil

	case "/look", "/l":
		return func() (msg tea.Msg) {
			defer func() {
				if r := recover(); r != nil {
					msg = errMsg{err: fmt.Errorf("panic recovered in /look: %v", r)}
				}
			}()
			lookIntent := dispatcher.Intent{
				IntentID: fmt.Sprintf("req_tui_look_%d", time.Now().UnixNano()),
				Action:   "LOOK",
				Payload:  json.RawMessage(`{}`),
			}
			res, err := m.dispatcher.Dispatch(m.ctx, lookIntent, 5*time.Second)
			if err != nil {
				return errMsg{err: fmt.Errorf("look failed: %v", err)}
			}
			return infoMsg(fmt.Sprintf("Look refreshed: %s", res.Message))
		}

	case "/move", "/m":
		if len(args) == 0 {
			m.errMessage = "Usage: /move <location_id>"
			return nil
		}
		targetLoc := args[0]
		return func() (msg tea.Msg) {
			defer func() {
				if r := recover(); r != nil {
					msg = errMsg{err: fmt.Errorf("panic recovered in /move: %v", r)}
				}
			}()
			payload, _ := json.Marshal(map[string]string{"target_location": targetLoc})
			moveIntent := dispatcher.Intent{
				IntentID: fmt.Sprintf("req_tui_move_%d", time.Now().UnixNano()),
				Action:   "MOVE",
				Payload:  payload,
			}
			res, err := m.dispatcher.Dispatch(m.ctx, moveIntent, 5*time.Second)
			if err != nil {
				return errMsg{err: fmt.Errorf("move failed: %v", err)}
			}
			return infoMsg(fmt.Sprintf("Moved: %s", res.Message))
		}

	case "/chat", "/c":
		if len(args) == 0 {
			m.errMessage = "Usage: /chat <message>"
			return nil
		}
		msg := strings.Join(args, " ")
		return func() (retMsg tea.Msg) {
			defer func() {
				if r := recover(); r != nil {
					retMsg = errMsg{err: fmt.Errorf("panic recovered in /chat: %v", r)}
				}
			}()
			payload, _ := json.Marshal(map[string]string{
				"agent_id": m.agentID,
				"message":  msg,
			})
			chatIntent := dispatcher.Intent{
				IntentID: fmt.Sprintf("req_tui_chat_%d", time.Now().UnixNano()),
				Action:   "CHAT",
				Payload:  payload,
			}
			res, err := m.dispatcher.Dispatch(m.ctx, chatIntent, 5*time.Second)
			if err != nil {
				return errMsg{err: fmt.Errorf("chat failed: %v", err)}
			}
			return infoMsg(fmt.Sprintf("Chat sent: %s", res.Message))
		}

	case "/suggest", "/s":
		if len(args) == 0 {
			m.errMessage = "Usage: /suggest <guidance hint>"
			return nil
		}
		hint := strings.Join(args, " ")
		m.ctxMgr.IngestSystem(fmt.Sprintf("[Human Suggestion] %s", hint))
		m.isThinking = true
		return func() (retMsg tea.Msg) {
			defer func() {
				if r := recover(); r != nil {
					retMsg = errMsg{err: fmt.Errorf("panic recovered in /suggest: %v", r)}
				}
			}()
			if m.engine == nil {
				msg := fmt.Sprintf("Suggestion recorded (no engine active): %s", hint)
				m.ctxMgr.IngestSystem(msg)
				return infoMsg(msg)
			}
			var memories []spatial.PeerMemoryContext
			if m.fetchPeerMemories != nil {
				memories = m.fetchPeerMemories()
			}
			prompt := m.ctxMgr.BuildPrompt(memories)
			temp := 0.7
			if m.temperature != nil {
				temp = *m.temperature
			}
			genOpts := model.GenerationOptions{
				Temperature: float32(temp),
			}
			intent, err := dispatcher.GenerateIntentWithRetry(m.ctx, m.engine, prompt, genOpts, 2)
			if err != nil {
				return errMsg{err: fmt.Errorf("cognition generation failed: %v", err)}
			}
			res, err := m.dispatcher.Dispatch(m.ctx, intent, 10*time.Second)
			if err != nil {
				return errMsg{err: fmt.Errorf("intent dispatch failed: %v", err)}
			}
			msg := fmt.Sprintf("Suggestion processed -> Action: %s (Thought: %s) [%s]", intent.Action, intent.Thought, res.Message)
			m.ctxMgr.IngestSystem(msg)
			return infoMsg(msg)
		}

	case "/set":
		if len(args) < 2 {
			m.errMessage = "Usage: /set <probability|freq|temp|debounce> <value>"
			return nil
		}
		param := strings.ToLower(args[0])
		valStr := args[1]

		switch param {
		case "probability", "prob":
			f, err := strconv.ParseFloat(valStr, 64)
			if err != nil || f < 0.0 || f > 1.0 {
				m.errMessage = "Invalid probability value (must be between 0.0 and 1.0)"
				return nil
			}
			if m.chatProbability != nil {
				*m.chatProbability = f
			}
			m.infoMessage = fmt.Sprintf("Chat response probability set to %.2f", f)
		case "freq":
			i, err := strconv.Atoi(valStr)
			if err != nil || i <= 0 {
				m.errMessage = "Invalid reflection frequency ticks (must be > 0)"
				return nil
			}
			if m.reflectionFreq != nil {
				*m.reflectionFreq = i
			}
			m.infoMessage = fmt.Sprintf("Reflection frequency set to %d ticks", i)
		case "temp", "temperature":
			f, err := strconv.ParseFloat(valStr, 64)
			if err != nil || f < 0.0 {
				m.errMessage = "Invalid temperature value"
				return nil
			}
			if m.temperature != nil {
				*m.temperature = f
			}
			m.infoMessage = fmt.Sprintf("Model temperature set to %.2f", f)
		case "debounce":
			i, err := strconv.Atoi(valStr)
			if err != nil || i < 0 {
				m.errMessage = "Invalid debounce milliseconds"
				return nil
			}
			if m.debounceMS != nil {
				*m.debounceMS = i
			}
			m.infoMessage = fmt.Sprintf("Tick debounce MS set to %d ms", i)
		default:
			m.errMessage = fmt.Sprintf("Unknown parameter '%s'. Supported: probability, freq, temp, debounce", param)
		}
		return nil

	default:
		m.errMessage = fmt.Sprintf("Unknown command '%s'. Type /help for assistance.", cmd)
		return nil
	}
}
