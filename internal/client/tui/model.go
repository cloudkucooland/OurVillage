package tui

import (
	"context"
	"strings"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/memory"
	"agentmud/internal/client/model"
	"agentmud/internal/client/routine"
	"agentmud/internal/client/spatial"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type TUIModel struct {
	ctx        context.Context
	cfg        interface{} // holds client config reference or parameter references
	agentID    string
	mqttClient mqtt.Client
	tracker    *dispatcher.CorrelationTracker
	ctxMgr     *spatial.ContextManager
	dispatcher *dispatcher.IntentDispatcher

	engine            model.InferenceEngine
	fetchPeerMemories func() []spatial.PeerMemoryContext

	textInput              textinput.Model
	viewport               viewport.Model
	errMessage             string
	infoMessage            string
	backgroundStatus       string
	isThinking             bool
	synthesisStatus        <-chan string
	selfIdentityProvider   func() ([]memory.IdentityTrait, []memory.SelfReflection, error)
	identityTraits         []memory.IdentityTrait
	selfReflections        []memory.SelfReflection
	identityLoadError      string
	projectProvider        func() ([]memory.PersonalProject, error)
	personalProjects       []memory.PersonalProject
	projectLoadError       string
	locationMemoryProvider func() ([]memory.LocationMemory, error)
	locationMemories       []memory.LocationMemory
	locationLoadError      string
	travelIntentionSaver   func(routine.Intention) error
	tab                    int // Sensory Log, Inventory, Relationships, Self Identity, Projects, Locations
	width                  int
	height                 int
	quitting               bool

	// Config references for /set tuning
	chatProbability *float64
	reflectionFreq  *int
	temperature     *float64
	debounceMS      *int
}

type TickMsg time.Time
type infoMsg string
type errMsg struct{ err error }
type synthesisStatusMsg string
type selfIdentityMsg struct {
	traits      []memory.IdentityTrait
	reflections []memory.SelfReflection
	err         error
}

type projectsMsg struct {
	projects []memory.PersonalProject
	err      error
}

type locationMemoryMsg struct {
	memories []memory.LocationMemory
	err      error
}

func (e errMsg) Error() string { return e.err.Error() }

func (m *TUIModel) SetSynthesisStatusChannel(statuses <-chan string) {
	m.synthesisStatus = statuses
}

func (m *TUIModel) SetSelfIdentityProvider(provider func() ([]memory.IdentityTrait, []memory.SelfReflection, error)) {
	m.selfIdentityProvider = provider
}

func (m *TUIModel) SetProjectProvider(provider func() ([]memory.PersonalProject, error)) {
	m.projectProvider = provider
}

func (m *TUIModel) SetLocationMemoryProvider(provider func() ([]memory.LocationMemory, error)) {
	m.locationMemoryProvider = provider
}

func (m *TUIModel) SetTravelIntentionSaver(saver func(routine.Intention) error) {
	m.travelIntentionSaver = saver
}

func NewTUIModel(ctx context.Context, agentID string, client mqtt.Client, tracker *dispatcher.CorrelationTracker, ctxMgr *spatial.ContextManager, engine model.InferenceEngine, fetchMemories func() []spatial.PeerMemoryContext, chatProb *float64, refFreq *int, temp *float64, debounce *int) TUIModel {
	ti := textinput.New()
	ti.Placeholder = "Type influence hint or /help, /chat <msg>, /look, /move <loc>, /set..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 80

	vp := viewport.New(80, 20)
	vp.SetContent("Initializing AgentMUD TUI...")

	disp := dispatcher.NewIntentDispatcher(agentID, client, tracker)

	return TUIModel{
		ctx:               ctx,
		agentID:           agentID,
		mqttClient:        client,
		tracker:           tracker,
		ctxMgr:            ctxMgr,
		dispatcher:        disp,
		engine:            engine,
		fetchPeerMemories: fetchMemories,
		textInput:         ti,
		viewport:          vp,
		chatProbability:   chatProb,
		reflectionFreq:    refFreq,
		temperature:       temp,
		debounceMS:        debounce,
	}
}

func (m TUIModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return TickMsg(t)
		}),
	}
	if m.synthesisStatus != nil {
		cmds = append(cmds, m.listenSynthesisStatus())
	}
	return tea.Batch(cmds...)
}

func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyCtrlL:
			return m, tea.ClearScreen
		case tea.KeyTab:
			m.tab = (m.tab + 1) % 6
			if m.tab == 3 {
				return m, m.loadSelfIdentity()
			} else if m.tab == 4 {
				return m, m.loadProjects()
			} else if m.tab == 5 {
				return m, m.loadLocationMemories()
			}
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
		m.height = msg.Height

		// Adjust viewport boundaries accounting for borders and headers
		m.viewport.Width = msg.Width - 6
		m.viewport.Height = msg.Height - 12
		if m.viewport.Height < 5 {
			m.viewport.Height = 5
		}
		m.textInput.Width = msg.Width - 6

	case TickMsg:
		// Periodic refresh of viewport content from sensory ring buffer
		logs := m.ctxMgr.FormatSensoryLogs()
		content := strings.Join(logs, "\n")
		wrapped := lipgloss.NewStyle().Width(m.viewport.Width).Render(content)
		m.viewport.SetContent(wrapped)
		m.viewport.GotoBottom()
		return m, tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
			return TickMsg(t)
		})

	case infoMsg:
		m.isThinking = false
		m.infoMessage = string(msg)
		return m, nil

	case errMsg:
		m.isThinking = false
		m.errMessage = msg.Error()
		return m, nil

	case synthesisStatusMsg:
		m.backgroundStatus = string(msg)
		if m.tab == 3 && strings.Contains(m.backgroundStatus, "reflecting on identity — complete") {
			return m, tea.Batch(m.listenSynthesisStatus(), m.loadSelfIdentity())
		}
		if m.tab == 4 && strings.Contains(m.backgroundStatus, "updating personal projects — complete") {
			return m, tea.Batch(m.listenSynthesisStatus(), m.loadProjects())
		}
		return m, m.listenSynthesisStatus()

	case selfIdentityMsg:
		m.identityLoadError = ""
		if msg.err != nil {
			m.identityLoadError = msg.err.Error()
		} else {
			m.identityTraits = msg.traits
			m.selfReflections = msg.reflections
		}
		return m, nil

	case projectsMsg:
		m.projectLoadError = ""
		if msg.err != nil {
			m.projectLoadError = msg.err.Error()
		} else {
			m.personalProjects = msg.projects
		}
		return m, nil

	case locationMemoryMsg:
		m.locationLoadError = ""
		if msg.err != nil {
			m.locationLoadError = msg.err.Error()
		} else {
			m.locationMemories = msg.memories
		}
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	m.viewport, _ = m.viewport.Update(msg)
	return m, cmd
}

func (m TUIModel) loadSelfIdentity() tea.Cmd {
	if m.selfIdentityProvider == nil {
		return nil
	}
	return func() tea.Msg {
		traits, reflections, err := m.selfIdentityProvider()
		return selfIdentityMsg{traits: traits, reflections: reflections, err: err}
	}
}

func (m TUIModel) loadProjects() tea.Cmd {
	if m.projectProvider == nil {
		return nil
	}
	return func() tea.Msg {
		projects, err := m.projectProvider()
		return projectsMsg{projects: projects, err: err}
	}
}

func (m TUIModel) loadLocationMemories() tea.Cmd {
	if m.locationMemoryProvider == nil {
		return nil
	}
	return func() tea.Msg {
		memories, err := m.locationMemoryProvider()
		return locationMemoryMsg{memories: memories, err: err}
	}
}

func (m TUIModel) listenSynthesisStatus() tea.Cmd {
	if m.synthesisStatus == nil {
		return nil
	}
	return func() tea.Msg {
		status, ok := <-m.synthesisStatus
		if !ok {
			return nil
		}
		return synthesisStatusMsg(status)
	}
}

func (m *TUIModel) handleInput(input string) tea.Cmd {
	m.errMessage = ""
	m.infoMessage = ""

	if strings.HasPrefix(input, "/") {
		return m.handleSlashCommand(input)
	}

	// Default normal text input treated as subconscious influence hint
	return m.influenceCmd(input)
}
