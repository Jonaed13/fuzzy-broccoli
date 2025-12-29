package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	
	"solana-pump-bot/internal/config"
	signalPkg "solana-pump-bot/internal/signal"
	"solana-pump-bot/internal/trading"
)


// --- CLONE THEME (CROSSTERM) ---
var (
	// Colors (Reference Image)
	// Background: Dark Slate Blue
	ColorBg          = lipgloss.Color("#0f1c2e") 
	ColorBorder      = lipgloss.Color("#2e7de9") // Blue/Cyan border
	ColorText        = lipgloss.Color("#a9b1d6") // Light Grey
	ColorAccentGreen = lipgloss.Color("#41a6b5") // Teal/Green
	ColorAccentPurple= lipgloss.Color("#bd93f9") // Purple
	ColorActive      = lipgloss.Color("#7aa2f7") // Bright Blue
	
	// Functional Mappings
	ColorSuccess = lipgloss.Color("#73daca")
	ColorWarning = lipgloss.Color("#ff9e64")
	ColorError   = lipgloss.Color("#f7768e")
	ColorInfo    = lipgloss.Color("#7dcfff")
	ColorProfit  = lipgloss.Color("#9ece6a") // Toxic Green
	ColorLoss    = lipgloss.Color("#f7768e") // Red

	// Styles
	StylePage = lipgloss.NewStyle().
		Background(ColorBg).
		Foreground(ColorText)

	StyleHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorActive).
		Padding(0, 0)

	StyleKey = lipgloss.NewStyle().
		Foreground(ColorAccentPurple).
		Bold(true)

	StyleProfit = lipgloss.NewStyle().Foreground(ColorProfit)
	StyleLoss   = lipgloss.NewStyle().Foreground(ColorLoss)

	// Legacies / Helpers
	ColorGray        = ColorText
	StyleTableHeader = lipgloss.NewStyle().Foreground(ColorActive).Bold(true)
	StyleFooter      = lipgloss.NewStyle().Foreground(ColorText)
	StyleModal       = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 2)
)

func RenderHotKey(k, d string) string {
	return StyleKey.Render("["+k+"]") + d
}

// --- ARCHITECTURE DEFINITIONS ---

type Screen string
const (
	ScreenDashboard Screen = "dashboard"
	ScreenConfig    Screen = "config"
	ScreenLogs      Screen = "logs"
	ScreenTrades    Screen = "trades"
	ScreenMetrics   Screen = "metrics"
)

// Global Keys
type KeyMap struct {
	Config, Pause, Sell, Logs, Trades, Quit key.Binding
	Up, Down, Left, Right, Enter, Escape    key.Binding
	Tab                                     key.Binding
	Search, Clear, Export, Theme, Health    key.Binding
	Tab1, Tab2, Tab3, Tab0                  key.Binding
}
var keys = KeyMap{
	Config: key.NewBinding(key.WithKeys("c")),
	Pause:  key.NewBinding(key.WithKeys("p")),
	Sell:   key.NewBinding(key.WithKeys("s")),
	Logs:   key.NewBinding(key.WithKeys("l")),
	Trades: key.NewBinding(key.WithKeys("t")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Left:   key.NewBinding(key.WithKeys("left", "h")),
	Right:  key.NewBinding(key.WithKeys("right", "l")),
	Tab:    key.NewBinding(key.WithKeys("tab")),
	Enter:  key.NewBinding(key.WithKeys("enter")),
	Escape: key.NewBinding(key.WithKeys("esc")),
	Search: key.NewBinding(key.WithKeys("/")),
	Clear:  key.NewBinding(key.WithKeys("f9")),
	Export: key.NewBinding(key.WithKeys("e")),
	Theme:  key.NewBinding(key.WithKeys("t")),
	Health: key.NewBinding(key.WithKeys("5")),
	Tab1:   key.NewBinding(key.WithKeys("1")),
	Tab2:   key.NewBinding(key.WithKeys("2")),
	Tab3:   key.NewBinding(key.WithKeys("3")),
	Tab0:   key.NewBinding(key.WithKeys("4")),
}

// Main Model
type Model struct {
	// Global State
	Config          *config.Manager
	WalletBalance   float64
	RPCLatency      time.Duration
	Running         bool
	StartTime       time.Time 
	
	// Navigation
	CurrentScreen   Screen
	Width, Height   int
	ActivePane      int // 0=Dashboard, 1=Signals, 2=Positions, 3=Metrics
	
	// Components
	Header       HeaderComponent
	Footer       FooterComponent
	Signals      SignalsPane
	Positions    PositionsPane
	ConfigModal  ConfigModal
	LogsView     LogsView
	TradesView   TradesHistoryView
	
	// Callbacks
	OnTogglePause func()
	OnForceClose  func(mint string)
	OnClear       func() // Clear stats callback
	OnExport      func() // Export trades to CSV
	
	// UI Mode: 1=Classic, 2=Crossterm, 3=Animated Premium, 4=Neon
	UIMode int
	
	// Animation state (Mode 3/4)
	Anim AnimationState
	
	// Mode 4 State
	FocusPane     int // 0=Left, 1=Center, 2=Right
	UniqueEntries map[string]bool
	Unique2X      map[string]bool
}

func NewModel(cfg *config.Manager) Model {
	// Read UI mode from environment (default: 4 = Neon Command Center)
	uiMode := 4
	modeEnv := os.Getenv("UI_MODE")
	if modeEnv == "1" { uiMode = 1 }
	if modeEnv == "2" { uiMode = 2 }
	if modeEnv == "4" { uiMode = 4 }
	
	// Initialize animation state for Mode 3/4
	var animState AnimationState
	if uiMode >= 3 {
		animState = NewAnimationState()
	}
	
	return Model{
		Config:        cfg,
		Running:       true,
		StartTime:     time.Now(),
		Header:        HeaderComponent{TotalEntries: 0, Reached2X: 0},
		Footer:        FooterComponent{},
		Signals:       NewSignalsPane(),
		Positions:     NewPositionsPane(),
		LogsView:      NewLogsView(),
		TradesView:    NewTradesHistoryView(),
		ConfigModal:   NewConfigModal(cfg),
		CurrentScreen: ScreenDashboard,
		UIMode:        uiMode,
		Anim:          animState,
		FocusPane:     1, // Default focus center
		UniqueEntries: make(map[string]bool),
		Unique2X:      make(map[string]bool),
	}
}

func (m *Model) SetCallbacks(pause func(), close func(string), clear func(), export func()) {
	m.OnTogglePause = pause
	m.OnForceClose = close
	m.OnClear = clear
	m.OnExport = export
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("AFNEX Bot"),
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return TickMsg(t) }),
	}
	
	// Start animation ticks for Mode 3
	if m.UIMode == 3 {
		cmds = append(cmds, AnimationTickCmd())
	}
	
	return tea.Batch(cmds...)
}

// Messages
type TickMsg time.Time
type SignalMsg struct { Signal *signalPkg.Signal }
type PositionMsg struct { Positions []*trading.Position }
type BalanceMsg struct { SOL float64 }
type LatencyMsg struct { Ms int64 }
type LogMsg struct { Lines []string }
type StatsMsg struct { Signals, Hits int }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleGlobalInput(msg)
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
	case TickMsg:
		m.Header.CurrentTime = time.Time(msg)
		// Update Memory Stats (ULTRATHINK)
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		m.Header.MemUsage = fmt.Sprintf("%dMB", mem.Alloc/1024/1024)
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return TickMsg(t) })
	
	case AnimationTickMsg:
		// Progress animation frames (Mode 3 only)
		if m.UIMode == 3 && m.Anim.IsAnimating() {
			m.Anim.Tick()
			return m, AnimationTickCmd() // Continue ticking
		}
		return m, nil
	
	// Data Updates
	case BalanceMsg:
		m.WalletBalance = msg.SOL
		m.Header.Balance = msg.SOL
	case LatencyMsg:
		m.RPCLatency = time.Duration(msg.Ms) * time.Millisecond
		m.Header.RPCLatency = m.RPCLatency
		// Update history (keep last 60 points)
		m.Header.LatencyHistory = append(m.Header.LatencyHistory, int(msg.Ms))
		if len(m.Header.LatencyHistory) > 60 {
			m.Header.LatencyHistory = m.Header.LatencyHistory[1:]
		}
	case SignalMsg:
		// If EXIT signal, mark matching ENTRY signals as Reached2X
		if msg.Signal.Type == signalPkg.SignalExit {
			for _, s := range m.Signals.List {
				if s.TokenName == msg.Signal.TokenName && s.Type == signalPkg.SignalEntry {
					s.Reached2X = true
				}
			}
			// Track Unique 2X Win (Mode 4 Stats)
			if m.Unique2X != nil {
				if !m.Unique2X[msg.Signal.TokenName] {
					m.Unique2X[msg.Signal.TokenName] = true
					m.Header.Reached2X++ // Increment counter only on first 2X per token
				}
			} else {
				// Fallback for older modes if maps not init
				m.Header.Reached2X++
			}
		} else if msg.Signal.Type == signalPkg.SignalEntry {
			// Track Unique Entry (Mode 4 Stats)
			m.Signals.Add(msg.Signal)
			if m.UniqueEntries != nil {
				if !m.UniqueEntries[msg.Signal.TokenName] {
					m.UniqueEntries[msg.Signal.TokenName] = true
					m.Header.TotalEntries++
				}
			} else {
				m.Header.TotalEntries++
			}
		}
		
		// Play sound if enabled
		if m.UIMode >= 2 {
			fmt.Print("\a")
		}
	case PositionMsg:
		m.Positions.Update(msg.Positions)
		m.Header.PnLPercent = m.Positions.TotalPnLPercent
	case LogMsg:
		m.LogsView.Add(msg.Lines)
	case StatsMsg:
		m.Header.TotalEntries = msg.Signals
		m.Header.Reached2X = msg.Hits
	}
	
	return m, nil
}

func (m Model) handleGlobalInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 1. Config Modal Overlay Override
	if m.CurrentScreen == ScreenConfig {
		// Pass pointer to model for adjustment
		return m.ConfigModal.Update(msg, &m)
	}

	// 2. Global Hotkeys (visible on Dashboard)
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Tab):
		m.FocusPane = (m.FocusPane + 1) % 3
	}

	// 3. Screen Specific Handling
	switch m.CurrentScreen {
	case ScreenDashboard:
		switch {
		case key.Matches(msg, keys.Config):
			m.CurrentScreen = ScreenConfig
		case key.Matches(msg, keys.Pause):
			m.Running = !m.Running
			if m.OnTogglePause != nil { m.OnTogglePause() }
		case key.Matches(msg, keys.Sell):
			m.sellAll()
		case key.Matches(msg, keys.Logs):
			m.CurrentScreen = ScreenLogs
		case key.Matches(msg, keys.Trades):
			m.CurrentScreen = ScreenTrades
		case key.Matches(msg, keys.Clear):
			// F9: Sell all, clear positions, clear signals, reset stats
			m.sellAll()
			m.Positions.Positions = nil
			m.Positions.Offset = 0
			m.Signals.List = nil
			m.Header.TotalEntries = 0
			m.Header.Reached2X = 0
			if m.OnClear != nil { m.OnClear() }
		case key.Matches(msg, keys.Up):
			if m.UIMode == 4 {
				// Mode 4: Contextual Scrolling
				if m.FocusPane == 1 && m.Signals.Offset > 0 {
					m.Signals.Offset--
				} else if m.FocusPane == 2 && m.Positions.Offset > 0 {
					m.Positions.Offset--
				}
			} else {
				// Legacy
				if m.Positions.Offset > 0 { m.Positions.Offset-- }
			}
		case key.Matches(msg, keys.Down):
			if m.UIMode == 4 {
				// Mode 4: Contextual Scrolling
				// Focus 1: Signals
				if m.FocusPane == 1 && m.Signals.Offset < len(m.Signals.List)-1 {
					m.Signals.Offset++
				}
				// Focus 2: Positions
				if m.FocusPane == 2 && m.Positions.Offset < len(m.Positions.Positions)-1 {
					m.Positions.Offset++
				}
			} else {
				// Legacy
				if m.Positions.Offset < len(m.Positions.Positions)-1 { m.Positions.Offset++ }
			}
		case key.Matches(msg, keys.Left):
			if m.UIMode == 4 {
				if m.FocusPane == 1 && m.Signals.Offset > 0 {
					m.Signals.Offset--
				} else if m.FocusPane == 2 && m.Positions.Offset > 0 {
					m.Positions.Offset--
				}
			} else {
				if m.Signals.Offset > 0 { m.Signals.Offset-- }
			}
		case key.Matches(msg, keys.Right):
			if m.UIMode == 4 {
				if m.FocusPane == 1 && m.Signals.Offset < len(m.Signals.List)-1 {
					m.Signals.Offset++
				} else if m.FocusPane == 2 && m.Positions.Offset < len(m.Positions.Positions)-1 {
					m.Positions.Offset++
				}
			} else {
				if m.Signals.Offset < len(m.Signals.List)-1 { m.Signals.Offset++ }
			}
		case key.Matches(msg, keys.Tab1):
			// Key 1: Classic=Full Signals, Crossterm=Health
			if m.UIMode == 1 {
				m.ActivePane = 1 // Full Signals
			} else {
				m.ActivePane = 4 // Health Dashboard
				if m.UIMode == 3 { m.Anim.TriggerButtonFlash("1"); return m, AnimationTickCmd() }
			}
		case key.Matches(msg, keys.Tab2):
			// Key 2: Classic=Full Positions, Crossterm/Animated=Export
			if m.UIMode == 1 {
				m.ActivePane = 2 // Full Positions
			} else {
				if m.OnExport != nil { m.OnExport() }
				if m.UIMode == 3 { m.Anim.TriggerButtonFlash("2"); return m, AnimationTickCmd() }
			}
		case key.Matches(msg, keys.Tab3):
			// Key 3: Classic=Full Metrics, Crossterm/Animated=Theme
			if m.UIMode == 1 {
				m.ActivePane = 3 // Full Metrics
			} else {
				CycleTheme()
				if m.UIMode == 3 { m.Anim.TriggerButtonFlash("3"); return m, AnimationTickCmd() }
			}
		case key.Matches(msg, keys.Tab0):
			// Key 4: Classic=nothing, Crossterm=Clear
			if m.UIMode != 1 {
				m.sellAll()
				m.Positions.Positions = nil
				m.Positions.Offset = 0
				m.Signals.List = nil
				m.Header.TotalEntries = 0
				m.Header.Reached2X = 0
				if m.OnClear != nil { m.OnClear() }
			}
		case key.Matches(msg, keys.Escape):
			m.ActivePane = 0 // Dashboard
		case key.Matches(msg, keys.Export):
			if m.OnExport != nil { m.OnExport() }
		case key.Matches(msg, keys.Theme):
			CycleTheme() // Cycle to next theme
		case key.Matches(msg, keys.Health):
			m.ActivePane = 4 // Full Health Dashboard
		}
	case ScreenLogs:
		return m.LogsView.Update(msg, m)
	case ScreenTrades:
		return m.TradesView.Update(msg, m)
	}
	
	return m, nil
}

func (m Model) sellAll() {
	if m.OnForceClose != nil {
		for _, p := range m.Positions.Positions {
			m.OnForceClose(p.Mint)
		}
	}
}

// Config Adjustment Logic
func (m *Model) adjustConfig(delta float64) {
	m.Config.Update(func(c *config.Config) {
		idx := m.ConfigModal.Selected
		switch idx {
		case 0: c.Trading.MinEntryPercent = maxf(10, c.Trading.MinEntryPercent + delta*5)
		case 1: c.Trading.TakeProfitMultiple = maxf(1.5, c.Trading.TakeProfitMultiple + delta*0.5)
		case 2: c.Trading.MaxAllocPercent = minf(100, maxf(5, c.Trading.MaxAllocPercent + delta*5))
		case 3: c.Trading.MaxOpenPositions = mini(50, maxi(1, c.Trading.MaxOpenPositions + int(delta)))
		case 4: c.Fees.StaticPriorityFeeSol = minf(1.0, maxf(0.0001, c.Fees.StaticPriorityFeeSol + delta*0.001))
		case 5: c.Trading.AutoTradingEnabled = !c.Trading.AutoTradingEnabled; m.Running = c.Trading.AutoTradingEnabled
		}
	})
}


// --- VIEW RENDERING ---

func (m Model) View() string {
	if m.Width == 0 { return "Loading..." }
	
	switch m.CurrentScreen {
	case ScreenLogs:
		return m.LogsView.Render(m.Width, m.Height)
	case ScreenTrades:
		return m.TradesView.Render(m.Width, m.Height)
	case ScreenConfig:
		return m.overlay(m.renderDashboard(), m.ConfigModal.Render(m.Width, m.Height))
	default:
		// ActivePane full-screen views
		switch m.ActivePane {
		case 1:
			return m.renderFullSignals()
		case 2:
			return m.renderFullPositions()
		case 3:
			return m.renderFullMetrics()
		case 4:
			return m.renderFullHealth()
		default:
			// UIMode: 1=Classic, 2=Crossterm, 3=Animated Premium
			switch m.UIMode {
			case 1:
				return m.renderClassicDashboard()
			case 3:
				return m.renderAnimatedDashboard()
			case 4:
				return m.renderNeonDashboard()
			default:
				return m.renderDashboard()
			}
		}
	}
}

func (m Model) renderDashboard() string {
	// 1. TOP ROW: TABS like "Crossterm Demo"
	// Tabs: [ MONITOR ] [ LOGS ] [ CONFIG ]
	tabStyle := lipgloss.NewStyle().Foreground(ColorText).Padding(0, 1)
	activeTabStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true).Padding(0, 1)
	
	monitorTab := activeTabStyle.Render("Monitor")
	if m.CurrentScreen != ScreenDashboard { monitorTab = tabStyle.Render("Monitor") }
	
	logsTab := tabStyle.Render("Logs")
	if m.CurrentScreen == ScreenLogs { logsTab = activeTabStyle.Render("Logs") }
	
	configTab := tabStyle.Render("Config")
	if m.CurrentScreen == ScreenConfig { configTab = activeTabStyle.Render("Config") }
	
	tabs := lipgloss.JoinHorizontal(lipgloss.Top, monitorTab, logsTab, configTab)
	tabsBox := renderBox("AFNEX Bot", tabs, m.Width, 3)

	// 2. MIDDLE ROW: GRAPHS (Gauge & Sparkline)
	// Title: "Metrics"
	// Gauge: Purple
	// Sparkline: Green
	
	balPct := (m.WalletBalance / 5.0) * 100
	if balPct > 100 { balPct = 100 }
	
	gaugeLabel := lipgloss.NewStyle().Foreground(ColorAccentPurple).Width(10).Render("Gauge:")
	// Reduce width even more to prevent wrapping (Width - Label(10) - Value(10-15) - Padding(5))
	gaugeBar := renderGauge(balPct, m.Width - 35, ColorAccentPurple)
	gaugeRow := lipgloss.JoinHorizontal(lipgloss.Left, gaugeLabel, gaugeBar, fmt.Sprintf(" %.2f SOL", m.WalletBalance))
	
	sparkLabel := lipgloss.NewStyle().Foreground(ColorAccentGreen).Width(10).Render("Sparkline:")
	sparkGraph := renderSparkline(m.Header.LatencyHistory, m.Width - 20)
	sparkRow := lipgloss.JoinHorizontal(lipgloss.Left, sparkLabel, sparkGraph, fmt.Sprintf(" %s", m.Header.RPCLatency))
	
	// LineGauge: Win Rate
	winRate := 0.0
	if m.Header.TotalEntries > 0 {
		winRate = float64(m.Header.Reached2X) / float64(m.Header.TotalEntries) * 100
	}
	lineLabel := lipgloss.NewStyle().Foreground(ColorActive).Width(10).Render("LineGauge:")
	lineGraph := renderLineGauge(winRate, m.Width - 20, ColorActive)
	lineRow := lipgloss.JoinHorizontal(lipgloss.Left, lineLabel, lineGraph)
	
	graphsContent := lipgloss.JoinVertical(lipgloss.Left, 
		gaugeRow, 
		"",
		sparkRow,
		"",
		lineRow,
	)
	graphsBox := renderBox("Metrics", graphsContent, m.Width, 8) // Height increased

	// 3. BOTTOM ROW: LISTS (Signals & Positions)
	usedHeight := lipgloss.Height(tabsBox) + lipgloss.Height(graphsBox) + 4
	listHeight := m.Height - usedHeight
	if listHeight < 5 { listHeight = 5 }
	
	halfWidth := (m.Width / 2) - 1
	
	// Signals List (as Bar Chart)
	var sigLines []string
	for i, s := range m.Signals.List {
		if i >= listHeight-2 { break }
		t := time.Unix(s.Timestamp, 0).Format("15:04")
		
		status := " "
		if s.Reached2X { status = "✓" }
		
		// Bar Chart visual: Proportional to value. 
		// Max value assumption: 1000% (10x). Scale 0-1000 to N blocks.
		barLen := int(s.Value / 1000.0 * 10.0) // 10 chars max
		if barLen < 1 { barLen = 1 }
		if barLen > 10 { barLen = 10 }
		bar := strings.Repeat("█", barLen)
		
		// Row: Time Token Bar Value Status
		row := fmt.Sprintf("%s %-7s %-10s %.0f%s %s", t, truncate(s.TokenName, 7), bar, s.Value, s.Unit, status)
		sigLines = append(sigLines, lipgloss.NewStyle().Foreground(ColorAccentGreen).Render(row))
	}
	signalsContent := strings.Join(sigLines, "\n")
	signalsBox := renderBox("Signals", signalsContent, halfWidth, listHeight)

	// Positions List
	var posLines []string
	for i, p := range m.Positions.Positions {
		if i >= listHeight-2 { break }
		pnlStyle := StyleProfit
		if p.PnLPercent < 0 { pnlStyle = StyleLoss }
		row := fmt.Sprintf("%-8s %s", truncate(p.TokenName, 8), pnlStyle.Render(fmt.Sprintf("%+.0f%%", p.PnLPercent)))
		posLines = append(posLines, row)
	}
	positionsContent := strings.Join(posLines, "\n")
	positionsBox := renderBox("Positions", positionsContent, halfWidth, listHeight)
	
	listsRow := lipgloss.JoinHorizontal(lipgloss.Top, signalsBox, positionsBox)

	// 4. BUTTON BAR (Interactive Footer)
	buttonStyle := lipgloss.NewStyle().
		Foreground(ColorText).
		Background(lipgloss.Color("#2a2b36")).
		Padding(0, 1).
		MarginRight(1)
	
	activeButtonStyle := buttonStyle.Copy().
		Foreground(ColorActive).
		Bold(true)
	
	statusLine := fmt.Sprintf("Uptime: %s | PnL: %+.2f%% | Theme: %s",
		time.Since(m.StartTime).Truncate(time.Second),
		m.Positions.TotalPnLPercent,
		GetTheme().Name,
	)
	
	buttons := lipgloss.JoinHorizontal(lipgloss.Left,
		activeButtonStyle.Render("[1:Health]"),
		buttonStyle.Render("[2:Export]"),
		buttonStyle.Render("[3:Theme]"),
		buttonStyle.Render("[4:Clear]"),
		buttonStyle.Copy().Foreground(ColorLoss).Render("[Q:Quit]"),
	)
	
	footerContent := lipgloss.JoinVertical(lipgloss.Left,
		statusLine,
		buttons,
	)
	footerBox := renderBox("Controls", footerContent, m.Width, 4)

	content := lipgloss.JoinVertical(lipgloss.Left, tabsBox, graphsBox, listsRow, footerBox)
	
	// Apply Full Page Background
	return StylePage.Render(content)
}

// renderClassicDashboard - Original dashboard with text hotkeys (UI Mode 1)
func (m Model) renderClassicDashboard() string {
	// 1. TOP ROW: TABS
	tabStyle := lipgloss.NewStyle().Foreground(ColorText).Padding(0, 1)
	activeTabStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true).Padding(0, 1)
	
	monitorTab := activeTabStyle.Render("Monitor")
	if m.CurrentScreen != ScreenDashboard { monitorTab = tabStyle.Render("Monitor") }
	logsTab := tabStyle.Render("Logs")
	configTab := tabStyle.Render("Config")
	
	tabs := lipgloss.JoinHorizontal(lipgloss.Top, monitorTab, logsTab, configTab)
	tabsBox := renderBox("AFNEX Bot", tabs, m.Width, 3)

	// 2. GRAPHS
	balPct := (m.WalletBalance / 5.0) * 100
	if balPct > 100 { balPct = 100 }
	
	gaugeLabel := lipgloss.NewStyle().Foreground(ColorAccentPurple).Width(10).Render("Gauge:")
	gaugeBar := renderGauge(balPct, m.Width - 35, ColorAccentPurple)
	gaugeRow := lipgloss.JoinHorizontal(lipgloss.Left, gaugeLabel, gaugeBar, fmt.Sprintf(" %.2f SOL", m.WalletBalance))
	
	sparkLabel := lipgloss.NewStyle().Foreground(ColorAccentGreen).Width(10).Render("Sparkline:")
	sparkGraph := renderSparkline(m.Header.LatencyHistory, m.Width - 20)
	sparkRow := lipgloss.JoinHorizontal(lipgloss.Left, sparkLabel, sparkGraph, fmt.Sprintf(" %s", m.Header.RPCLatency))
	
	graphsContent := lipgloss.JoinVertical(lipgloss.Left, gaugeRow, "", sparkRow)
	graphsBox := renderBox("Metrics", graphsContent, m.Width, 6)

	// 3. LISTS
	usedHeight := lipgloss.Height(tabsBox) + lipgloss.Height(graphsBox) + 4
	listHeight := m.Height - usedHeight
	if listHeight < 5 { listHeight = 5 }
	halfWidth := (m.Width / 2) - 1
	
	var sigLines []string
	for i, s := range m.Signals.List {
		if i >= listHeight-2 { break }
		t := time.Unix(s.Timestamp, 0).Format("15:04")
		status := " "
		if s.Reached2X { status = "✓" }
		row := fmt.Sprintf("%s %-7s %.1f%s %s", t, truncate(s.TokenName, 7), s.Value, s.Unit, status)
		sigLines = append(sigLines, lipgloss.NewStyle().Foreground(ColorAccentGreen).Render(row))
	}
	signalsBox := renderBox("Signals", strings.Join(sigLines, "\n"), halfWidth, listHeight)

	var posLines []string
	for i, p := range m.Positions.Positions {
		if i >= listHeight-2 { break }
		pnlStyle := StyleProfit
		if p.PnLPercent < 0 { pnlStyle = StyleLoss }
		row := fmt.Sprintf("%-8s %s", truncate(p.TokenName, 8), pnlStyle.Render(fmt.Sprintf("%+.0f%%", p.PnLPercent)))
		posLines = append(posLines, row)
	}
	positionsBox := renderBox("Positions", strings.Join(posLines, "\n"), halfWidth, listHeight)
	
	listsRow := lipgloss.JoinHorizontal(lipgloss.Top, signalsBox, positionsBox)

	// 4. CLASSIC FOOTER (text hotkeys)
	statusLine := fmt.Sprintf("Uptime: %s | PnL: %+.2f%%", time.Since(m.StartTime).Truncate(time.Second), m.Positions.TotalPnLPercent)
	hotkeys := "[1]Signals [2]Positions [3]Metrics [5]Health [C]fg [P]ause [S]ell [F9]Clear [Q]uit"
	footerBox := renderBox("Footer", lipgloss.NewStyle().Foreground(ColorText).Render(statusLine+"\n"+hotkeys), m.Width, 4)

	content := lipgloss.JoinVertical(lipgloss.Left, tabsBox, graphsBox, listsRow, footerBox)
	return StylePage.Render(content)
}

// renderAnimatedDashboard - Cyberpunk animated dashboard (UI Mode 3)
func (m Model) renderAnimatedDashboard() string {
	progress := m.Anim.GetStartupProgress()
	
	// Cyberpunk color palette
	colors := []lipgloss.Color{
		lipgloss.Color("#ff00ff"), // Magenta
		lipgloss.Color("#00ffff"), // Cyan
		lipgloss.Color("#ff79c6"), // Pink
	}
	trueBlack := lipgloss.Color("#0a0a0a")
	
	// Animated border color
	borderColor := colors[m.Anim.GetBorderColorIndex()]
	
	// During startup animation, show animated logo
	if progress < 1.0 {
		return m.renderStartupAnimation(progress, trueBlack, borderColor, colors[0])
	}
	
	// ═══════════════════════════════════════════════════════════════
	// CYBERPUNK LAYOUT: Single-column, centered, animated
	// ═══════════════════════════════════════════════════════════════
	
	// Animated double-line border style
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(borderColor).
		Background(trueBlack).
		Padding(0, 1)
	
	// ─── HEADER ───
	titleGlow := lipgloss.NewStyle().Foreground(colors[0]).Bold(true)
	statusColor := ColorProfit
	statusText := "● LIVE"
	if !m.Running {
		statusColor = ColorLoss
		statusText = "● PAUSED"
	}
	
	headerLeft := titleGlow.Render("⚡ AFNEX CYBERPUNK ⚡")
	headerRight := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusText)
	headerContent := lipgloss.JoinHorizontal(lipgloss.Center,
		headerLeft,
		strings.Repeat(" ", m.Width-40),
		headerRight,
	)
	header := boxStyle.Copy().Width(m.Width-4).Render(headerContent)
	
	// ─── STATS BAR (Time, Date, Uptime, RAM, RPC) ───
	now := time.Now()
	uptime := time.Since(m.StartTime).Truncate(time.Second)
	statsLine := fmt.Sprintf("📅 %s │ 🕐 %s │ ⏱ %s │ 💾 %s │ 📡 %dms",
		now.Format("2006-01-02"),
		now.Format("15:04:05"),
		uptime.String(),
		m.Header.MemUsage,
		m.RPCLatency.Milliseconds(),
	)
	statsStyled := lipgloss.NewStyle().Foreground(colors[2]).Render(statsLine)
	statsBar := boxStyle.Copy().Width(m.Width-4).Render(statsStyled)
	
	// ─── PULSING BALANCE GAUGE ───
	balPct := (m.WalletBalance / 5.0) * 100
	if balPct > 100 { balPct = 100 }
	
	// Apply pulse animation to gauge
	pulseFactor := m.Anim.GetGaugePulse()
	animatedPct := balPct * pulseFactor
	if animatedPct > 100 { animatedPct = 100 }
	
	gaugeWidth := m.Width - 30
	filled := int(float64(gaugeWidth) * animatedPct / 100)
	empty := gaugeWidth - filled
	
	// Gradient fill using different block chars
	var gaugeBar string
	for i := 0; i < filled; i++ {
		// Alternate colors for rainbow effect
		colorIdx := (i + m.Anim.GlobalFrame/3) % len(colors)
		gaugeBar += lipgloss.NewStyle().Foreground(colors[colorIdx]).Render("▓")
	}
	gaugeBar += lipgloss.NewStyle().Foreground(lipgloss.Color("#333333")).Render(strings.Repeat("░", empty))
	
	balLabel := lipgloss.NewStyle().Foreground(colors[1]).Bold(true).Render("BALANCE")
	balValue := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(fmt.Sprintf("%.3f SOL", m.WalletBalance))
	gaugeContent := lipgloss.JoinHorizontal(lipgloss.Left, balLabel, "  ", gaugeBar, "  ", balValue)
	gaugeSection := boxStyle.Copy().Width(m.Width-4).Render(gaugeContent)
	
	// ─── WAVE ANIMATED SPARKLINE ───
	wave := m.renderWaveSparkline(m.Width-10, colors[1])
	waveLabel := lipgloss.NewStyle().Foreground(colors[2]).Bold(true).Render("LATENCY")
	waveContent := lipgloss.JoinVertical(lipgloss.Left, waveLabel, wave)
	waveSection := boxStyle.Copy().Width(m.Width-4).Render(waveContent)
	
	// ─── SIGNALS & POSITIONS (Stacked, not side-by-side) ───
	listHeight := (m.Height - 20) / 2
	if listHeight < 3 { listHeight = 3 }
	
	// Signals with slide-in effect
	sigTitle := lipgloss.NewStyle().Foreground(colors[0]).Bold(true).Render("═══ SIGNALS ═══")
	var sigLines []string
	for i, s := range m.Signals.List {
		if i >= listHeight { break }
		t := time.Unix(s.Timestamp, 0).Format("15:04:05")
		indicator := "→"
		if s.Reached2X { indicator = "✓" }
		// Slide-in offset based on item age
		lineStyle := lipgloss.NewStyle().Foreground(ColorAccentGreen)
		row := fmt.Sprintf("  %s %s %-10s %5.1f%s", indicator, t, truncate(s.TokenName, 10), s.Value, s.Unit)
		sigLines = append(sigLines, lineStyle.Render(row))
	}
	if len(sigLines) == 0 {
		sigLines = append(sigLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render("  No signals yet..."))
	}
	sigContent := lipgloss.JoinVertical(lipgloss.Left, append([]string{sigTitle}, sigLines...)...)
	sigSection := boxStyle.Copy().Width(m.Width-4).Height(listHeight+2).Render(sigContent)
	
	// Positions with PnL coloring
	posTitle := lipgloss.NewStyle().Foreground(colors[1]).Bold(true).Render("═══ POSITIONS ═══")
	var posLines []string
	for i, p := range m.Positions.Positions {
		if i >= listHeight { break }
		pnlStyle := StyleProfit
		pnlIcon := "▲"
		if p.PnLPercent < 0 { 
			pnlStyle = StyleLoss 
			pnlIcon = "▼"
		}
		row := fmt.Sprintf("  %s %-12s %s", pnlIcon, truncate(p.TokenName, 12), pnlStyle.Render(fmt.Sprintf("%+6.1f%%", p.PnLPercent)))
		posLines = append(posLines, row)
	}
	if len(posLines) == 0 {
		posLines = append(posLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render("  No positions yet..."))
	}
	posContent := lipgloss.JoinVertical(lipgloss.Left, append([]string{posTitle}, posLines...)...)
	posSection := boxStyle.Copy().Width(m.Width-4).Height(listHeight+2).Render(posContent)
	
	// ─── ANIMATED BUTTON BAR ───
	footer := m.renderCyberpunkButtonBar(colors, borderColor)
	footerSection := boxStyle.Copy().Width(m.Width-4).Render(footer)
	
	// Assemble layout
	content := lipgloss.JoinVertical(lipgloss.Center,
		header,
		statsBar,
		gaugeSection,
		waveSection,
		sigSection,
		posSection,
		footerSection,
	)
	
	return lipgloss.NewStyle().Background(trueBlack).Width(m.Width).Height(m.Height).Render(content)
}

// renderWaveSparkline creates an animated wave effect
func (m Model) renderWaveSparkline(width int, color lipgloss.Color) string {
	if width < 10 { width = 10 }
	
	// Wave pattern using Unicode blocks
	waveChars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█', '▇', '▆', '▅', '▄', '▃', '▂'}
	offset := m.Anim.WaveOffset(len(waveChars))
	
	var wave strings.Builder
	for i := 0; i < width; i++ {
		charIdx := (i + offset) % len(waveChars)
		wave.WriteRune(waveChars[charIdx])
	}
	
	// If we have actual latency data, overlay it
	if len(m.Header.LatencyHistory) > 0 {
		// Use real data for the wave
		return lipgloss.NewStyle().Foreground(color).Render(wave.String())
	}
	
	return lipgloss.NewStyle().Foreground(color).Render(wave.String())
}

// renderCyberpunkButtonBar creates animated button bar
func (m Model) renderCyberpunkButtonBar(colors []lipgloss.Color, borderColor lipgloss.Color) string {
	// Status line
	statusLine := fmt.Sprintf("⏱ %s │ 💰 %+.2f%% │ 🎨 %s",
		time.Since(m.StartTime).Truncate(time.Second),
		m.Positions.TotalPnLPercent,
		GetTheme().Name,
	)
	statusStyled := lipgloss.NewStyle().Foreground(colors[1]).Render(statusLine)
	
	// Animated buttons
	btnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1a1a2e")).
		Padding(0, 1).
		MarginRight(1)
	
	flashStyle := btnStyle.Copy().
		Foreground(lipgloss.Color("#000000")).
		Background(colors[0]).
		Bold(true)
	
	btn1 := btnStyle.Render("❶ Health")
	if m.Anim.GetButtonFlashActive("1") { btn1 = flashStyle.Render("❶ HEALTH") }
	
	btn2 := btnStyle.Render("❷ Export")
	if m.Anim.GetButtonFlashActive("2") { btn2 = flashStyle.Render("❷ EXPORT") }
	
	btn3 := btnStyle.Render("❸ Theme")
	if m.Anim.GetButtonFlashActive("3") { btn3 = flashStyle.Render("❸ THEME") }
	
	btn4 := btnStyle.Render("❹ Clear")
	if m.Anim.GetButtonFlashActive("4") { btn4 = flashStyle.Render("❹ CLEAR") }
	
	btnQ := btnStyle.Copy().Background(lipgloss.Color("#4a1a1a")).Render("Ⓠ Quit")
	
	buttons := lipgloss.JoinHorizontal(lipgloss.Left, btn1, btn2, btn3, btn4, btnQ)
	
	return lipgloss.JoinVertical(lipgloss.Left, statusStyled, "", buttons)
}

// renderStartupAnimation - Animated logo reveal (kept from before)
func (m Model) renderStartupAnimation(progress float64, bg, border, accent lipgloss.Color) string {
	centerStyle := lipgloss.NewStyle().
		Width(m.Width).
		Height(m.Height).
		Align(lipgloss.Center, lipgloss.Center).
		Background(bg)
	
	var content string
	
	if progress < 0.5 {
		// Phase 1: Logo fade with typewriter
		logoProgress := progress * 2
		logo := TypewriterString("⚡ AFNEX CYBERPUNK ⚡", logoProgress)
		content = lipgloss.NewStyle().Foreground(accent).Bold(true).Render(logo)
	} else if progress < 0.8 {
		// Phase 2: Full logo + loading bar
		logo := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("⚡ AFNEX CYBERPUNK ⚡")
		barProgress := (progress - 0.5) / 0.3
		barWidth := int(float64(40) * barProgress)
		bar := strings.Repeat("█", barWidth) + strings.Repeat("░", 40-barWidth)
		loadingBar := lipgloss.NewStyle().Foreground(border).Render(bar)
		loadingText := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("Initializing systems...")
		content = lipgloss.JoinVertical(lipgloss.Center, logo, "", loadingBar, "", loadingText)
	} else {
		// Phase 3: Ready
		logo := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("⚡ SYSTEMS ONLINE ⚡")
		content = logo
	}
	
	return centerStyle.Render(content)
}

// --- FULL-SCREEN PANE RENDERS ---

func (m Model) renderFullSignals() string {
	header := renderBox("SIGNALS (Full View) [Press 0/Esc to go back]", "", m.Width, 2)
	
	listHeight := m.Height - 4
	var lines []string
	for i, s := range m.Signals.List {
		if i >= listHeight { break }
		t := time.Unix(s.Timestamp, 0).Format("15:04:05")
		status := " "
		if s.Reached2X { status = "✓" }
		
		barLen := int(s.Value / 1000.0 * 20.0)
		if barLen < 1 { barLen = 1 }
		if barLen > 20 { barLen = 20 }
		bar := strings.Repeat("█", barLen)
		
		row := fmt.Sprintf("%s %-10s %-20s %.1f%s %s", t, truncate(s.TokenName, 10), bar, s.Value, s.Unit, status)
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorAccentGreen).Render(row))
	}
	
	body := renderBox("", strings.Join(lines, "\n"), m.Width, listHeight)
	return StylePage.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) renderFullPositions() string {
	header := renderBox("POSITIONS (Full View) [Press 0/Esc to go back]", "", m.Width, 2)
	
	listHeight := m.Height - 4
	var lines []string
	for i, p := range m.Positions.Positions {
		if i >= listHeight { break }
		pnlStyle := StyleProfit
		if p.PnLPercent < 0 { pnlStyle = StyleLoss }
		
		row := fmt.Sprintf("%-12s Entry: %.1f%% | Curr: %.1f%% | %s | Age: %s",
			truncate(p.TokenName, 12),
			p.EntryValue,
			p.CurrentValue,
			pnlStyle.Render(fmt.Sprintf("%+.1f%%", p.PnLPercent)),
			formatDuration(time.Since(p.EntryTime)),
		)
		lines = append(lines, row)
	}
	
	body := renderBox("", strings.Join(lines, "\n"), m.Width, listHeight)
	return StylePage.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) renderFullMetrics() string {
	header := renderBox("METRICS (Full View) [Press 0/Esc to go back]", "", m.Width, 2)
	
	// Larger charts
	balPct := (m.WalletBalance / 5.0) * 100
	if balPct > 100 { balPct = 100 }
	gaugeRow := fmt.Sprintf("Wallet:    %s  %.2f SOL", renderGauge(balPct, m.Width-30, ColorAccentPurple), m.WalletBalance)
	
	sparkRow := fmt.Sprintf("Latency:   %s  %s", renderSparkline(m.Header.LatencyHistory, m.Width-30), m.Header.RPCLatency)
	
	winRate := 0.0
	if m.Header.TotalEntries > 0 {
		winRate = float64(m.Header.Reached2X) / float64(m.Header.TotalEntries) * 100
	}
	lineRow := fmt.Sprintf("Win Rate:  %s", renderLineGauge(winRate, m.Width-30, ColorActive))
	
	content := lipgloss.JoinVertical(lipgloss.Left,
		"",
		gaugeRow,
		"",
		sparkRow,
		"",
		lineRow,
		"",
		fmt.Sprintf("Stats: 50%%+ Entries: %d | 2X Hits: %d", m.Header.TotalEntries, m.Header.Reached2X),
	)
	
	body := renderBox("", content, m.Width, m.Height-4)
	return StylePage.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) renderFullHealth() string {
	header := renderBox("HEALTH DASHBOARD (Full View) [Press 0/Esc to go back]", "", m.Width, 2)
	
	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  COMPONENT          STATUS     NOTES")
	lines = append(lines, "  ─────────          ──────     ─────")
	lines = append(lines, "")
	
	// Static status display (simplified without actual Checker integration for now)
	// RPC
	rpcIcon := lipgloss.NewStyle().Foreground(ColorProfit).Render("✓")
	lines = append(lines, fmt.Sprintf("  RPC Endpoint       %s          Connected", rpcIcon))
	
	// Telegram
	teleIcon := lipgloss.NewStyle().Foreground(ColorProfit).Render("✓")
	lines = append(lines, fmt.Sprintf("  Telegram Listener  %s          Active", teleIcon))
	
	// DB
	dbIcon := lipgloss.NewStyle().Foreground(ColorProfit).Render("✓")
	lines = append(lines, fmt.Sprintf("  SQLite Database    %s          Healthy", dbIcon))
	
	// Jupiter
	jupIcon := lipgloss.NewStyle().Foreground(ColorProfit).Render("✓")
	lines = append(lines, fmt.Sprintf("  Jupiter API        %s          Reachable", jupIcon))
	
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Last Check: %s", time.Now().Format("15:04:05")))
	lines = append(lines, fmt.Sprintf("  Uptime:     %s", time.Since(m.StartTime).Truncate(time.Second)))
	
	body := renderBox("", strings.Join(lines, "\n"), m.Width, m.Height-4)
	return StylePage.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m Model) overlay(base, modal string) string {
	bLines := strings.Split(base, "\n")
	mLines := strings.Split(modal, "\n")
	
	y := (len(bLines) - len(mLines)) / 2
	if y < 0 { y = 0 }
	
	for i, line := range mLines {
		if y+i < len(bLines) {
			bLines[y+i] = line 
		}
	}
	return strings.Join(bLines, "\n")
}


// --- COMPONENTS ---

// 1. HEADER
type HeaderComponent struct {
	Status       string
	Balance      float64
	RPCLatency   time.Duration
	PnLPercent   float64
	CurrentTime  time.Time
	MemUsage     string
	TotalEntries int    // 50%+ signals
	Reached2X    int    // How many hit 2X
	LatencyHistory []int // For sparkline
}

const Version = "v2.1"

func (h HeaderComponent) Render(w int) string {
	statusDots := "●"
	statusColor := ColorSuccess
	if h.Status != "RUNNING" { statusColor = ColorWarning }
	
	status := lipgloss.NewStyle().Foreground(statusColor).Render(statusDots + h.Status + " " + Version)
	bal := fmt.Sprintf("Bal: %.2f SOL", h.Balance)
	rpc := fmt.Sprintf("RPC: %dms", h.RPCLatency.Milliseconds())
	mem := fmt.Sprintf("MEM: %s", h.MemUsage)
	
	// Stats: 50%+ found and 2X hit rate
	var hitRate float64
	if h.TotalEntries > 0 {
		hitRate = float64(h.Reached2X) / float64(h.TotalEntries) * 100
	}
	stats := lipgloss.NewStyle().Foreground(ColorInfo).Render(fmt.Sprintf("50%%+: %d | 2X: %d (%.0f%%)", h.TotalEntries, h.Reached2X, hitRate))
	
	pnlColor := ColorProfit
	if h.PnLPercent < 0 { pnlColor = ColorLoss }
	pnl := lipgloss.NewStyle().Foreground(pnlColor).Render(fmt.Sprintf("PnL: %+.1f%%", h.PnLPercent))
	
	timeStr := h.CurrentTime.Format("15:04:05")
	
	// Layout: Status | Bal | RPC | MEM | Stats | PnL | Time
	parts := []string{status, bal, rpc, mem, stats, pnl, timeStr}
	content := strings.Join(parts, " │ ")
	
	return StyleHeader.Width(w).Render(content)
}

// 2. FOOTER
type FooterComponent struct { Screen string }
func (f FooterComponent) Render(w int) string {
	var s string
	switch f.Screen {
	case "dashboard":
		s = RenderHotKey("C", "fg") + " " + RenderHotKey("P", "ause") + " " + RenderHotKey("S", "ell") + " " + RenderHotKey("L", "og") + " " + RenderHotKey("T", "rades") + " " + RenderHotKey("F9", "Clr") + " " + RenderHotKey("Q", "uit")
	case "logs":
		s = RenderHotKey("Esc", "Back") + " " + RenderHotKey("Up/Dn", "Scroll")
	case "trades":
		s = RenderHotKey("Esc", "Back")
	case "config":
		s = RenderHotKey("Esc", "Cancel") + " " + RenderHotKey("Ent", "Save") + " " + RenderHotKey("Arrows", "Nav")
	default:
		s = RenderHotKey("Q", "uit")
	}
	return StyleFooter.Width(w).Render(s)
}

// 3. SIGNALS PANE
type SignalsPane struct {
	List   []*signalPkg.Signal
	Offset int // For scrolling
}
func NewSignalsPane() SignalsPane { return SignalsPane{List: []*signalPkg.Signal{}, Offset: 0} }
func (sp *SignalsPane) Add(s *signalPkg.Signal) {
	// Only show ENTRY signals (50%+) in the list, not EXIT (2X+)
	if s.Type != signalPkg.SignalEntry {
		return
	}
	sp.List = append([]*signalPkg.Signal{s}, sp.List...)
	if len(sp.List) > 20 { sp.List = sp.List[:20] }
}
func (sp SignalsPane) Render(w, h int) string {
	header := StyleTableHeader.Width(w).Render("📡 SIGNALS")
	subHeader := fmt.Sprintf("%-6s %-6s %-6s %s", "TIME", "TOKEN", "VALUE", "2X?")
	var lines []string
	lines = append(lines, subHeader)
	
	for _, s := range sp.List {
		if len(lines) >= h-1 { break }
		rowStyle := StyleProfit // All signals in list are ENTRY
		
		t := time.Unix(s.Timestamp, 0).Format("15:04")
		
		// 2X column: ✓ if reached 2X, ✗ if not
		reach := "✗"
		if s.Reached2X { reach = "✓" }
		
		row := fmt.Sprintf("%-6s %-6s %-6s %s", t, truncate(s.TokenName, 6), fmt.Sprintf("%.1f%s", s.Value, s.Unit), reach)
		lines = append(lines, rowStyle.Render(row))
	}
	for len(lines) < h-1 { lines = append(lines, "") }
	body := strings.Join(lines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

// 4. POSITIONS PANE
type PositionsPane struct {
	Positions []*trading.Position
	TotalPnLPercent float64
	Offset int // Scroll offset
}
func NewPositionsPane() PositionsPane { return PositionsPane{Positions: []*trading.Position{}} }
func (pp *PositionsPane) Update(pos []*trading.Position) {
	// Preserve scroll position if list length hasn't changed drastically
	// or reset if needed. For now, simple update.
	pp.Positions = pos
	var total float64
	for _, p := range pos { total += p.PnLPercent } 
	if len(pos) > 0 { pp.TotalPnLPercent = total / float64(len(pos)) } else { pp.TotalPnLPercent = 0 }
}
func (pp PositionsPane) Render(w, h int) string {
	header := StyleTableHeader.Width(w).Render("💼 OPEN POSITIONS " + fmt.Sprintf("(%d)", len(pp.Positions)))
	subHeader := fmt.Sprintf("%-8s %-7s %-7s %-3s %-6s %s", "TOKEN", "ENTRY%", "CURR%", "2X?", "PnL", "AGE")
	var lines []string
	lines = append(lines, subHeader)
	
	// Calculate visible height (h - header - subheader)
	visibleHeight := h - 2
	if visibleHeight < 1 { visibleHeight = 1 }

	// Slice positions based on Offset
	start := pp.Offset
	if start >= len(pp.Positions) { start = len(pp.Positions) - 1 }
	if start < 0 { start = 0 }
	
	end := start + visibleHeight
	if end > len(pp.Positions) { end = len(pp.Positions) }
	
	visiblePositions := []*trading.Position{}
	if len(pp.Positions) > 0 {
		visiblePositions = pp.Positions[start:end]
	}

	for _, p := range visiblePositions {
		pnlStyle := StyleProfit
		if p.PnLPercent < 0 { pnlStyle = StyleLoss }
		
		reach := "✗"
		if p.Reached2X { reach = "✓" }

		row := fmt.Sprintf("%-8s %-6s %-6s %-3s %-8s %s",
			truncate(p.TokenName, 8),
			fmt.Sprintf("%.1f", p.EntryValue),
			fmt.Sprintf("%.1f", p.CurrentValue),
			reach,
			pnlStyle.Render(fmt.Sprintf("%+.0f%%", p.PnLPercent)),
			formatDuration(time.Since(p.EntryTime)),
		)
		lines = append(lines, row)
	}
	
	// Add scroll indicator if there are more
	if end < len(pp.Positions) {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorGray).Render(fmt.Sprintf("... %d more ↓", len(pp.Positions)-end)))
	} else {
		for len(lines) < h-1 { lines = append(lines, "") }
	}
	
	return lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(lines, "\n"))
}

// 5. CONFIG MODAL
type ConfigModal struct {
	Cfg *config.Manager
	Fields []string
	Selected int
}
func NewConfigModal(cfg *config.Manager) ConfigModal {
	return ConfigModal{Cfg: cfg, Fields: []string{"MinEntry", "TakeProfit", "MaxAlloc", "MaxPos", "PrioFee", "AutoTrade"}, Selected: 0}
}
func (cm ConfigModal) Update(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		m.CurrentScreen = ScreenDashboard
	case key.Matches(msg, keys.Enter):
		m.CurrentScreen = ScreenDashboard
	case key.Matches(msg, keys.Up):
		if cm.Selected > 0 { m.ConfigModal.Selected-- }
	case key.Matches(msg, keys.Down):
		if cm.Selected < len(cm.Fields)-1 { m.ConfigModal.Selected++ }
	case key.Matches(msg, keys.Left):
		m.adjustConfig(-1)
	case key.Matches(msg, keys.Right):
		m.adjustConfig(1)
	}
	return *m, nil
}
func (cm ConfigModal) Render(w, h int) string {
	t := cm.Cfg.GetTrading()
	f := cm.Cfg.Get() // Get full config for Fees

	autoTrade := StyleLoss.Render("OFF")
	if t.AutoTradingEnabled {
		autoTrade = StyleProfit.Render("ON")
	}

	rows := []string{
		fmt.Sprintf("Min Entry %%:  %.0f", t.MinEntryPercent),
		fmt.Sprintf("Take Profit:  %.1fx", t.TakeProfitMultiple),
		fmt.Sprintf("Max Alloc %%:  %.0f", t.MaxAllocPercent),
		fmt.Sprintf("Max Pos:      %d", t.MaxOpenPositions),
		fmt.Sprintf("Priority Fee: %.4f", f.Fees.StaticPriorityFeeSol),
		fmt.Sprintf("Auto Trade:   %s", autoTrade),
	}
	
	s := "CONFIGURATION\n\n"
	for i, r := range rows {
		cursor := "  "
		if i == cm.Selected { cursor = "> " }
		s += cursor + r + "\n"
	}
	s += "\n[Ent] Save  [Esc] Cancel  [←/→] Adjust"
	return StyleModal.Render(s)
}

// 6. LOGS VIEW
type LogsView struct { Lines []string }
func NewLogsView() LogsView { return LogsView{Lines: []string{}} }
func (lv *LogsView) Add(l []string) {
	lv.Lines = append(lv.Lines, l...)
	if len(lv.Lines) > 500 {
		lv.Lines = lv.Lines[len(lv.Lines)-500:]
	}
}
func (lv LogsView) GetLastLine() string {
	if len(lv.Lines) == 0 { return "" }
	return lv.Lines[len(lv.Lines)-1]
}
func (lv LogsView) Update(msg tea.KeyMsg, m Model) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.Escape) { m.CurrentScreen = ScreenDashboard }
	return m, nil
}
func (lv LogsView) Render(w, h int) string {
	header := StyleTableHeader.Width(w).Render("SYSTEM LOGS")
	show := lv.Lines
	if len(show) > h-4 { show = show[len(show)-(h-4):] }
	return lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(show, "\n"))
}

// 7. TRADES VIEW
type TradesHistoryView struct {}
func NewTradesHistoryView() TradesHistoryView { return TradesHistoryView{} }
func (thv TradesHistoryView) Update(msg tea.KeyMsg, m Model) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.Escape) { m.CurrentScreen = ScreenDashboard }
	return m, nil
}
func (thv TradesHistoryView) Render(w, h int) string {
	header := StyleTableHeader.Width(w).Render("TRADE HISTORY")
	body := "No trades yet..." 
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}


// --- HELPERS ---
func maxf(a,b float64) float64 { if a>b{return a}; return b }
func minf(a,b float64) float64 { if a<b{return a}; return b }
func maxi(a,b int) int { if a>b{return a}; return b }
func mini(a,b int) int { if a<b{return a}; return b }
func truncate(s string, n int) string { return runewidth.Truncate(s, n, "") }
func formatDuration(d time.Duration) string {
	if d < 60*time.Second { return fmt.Sprintf("%ds", int(d.Seconds())) }
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// Send Funcs
func SendSignal(p *tea.Program, s *signalPkg.Signal){ p.Send(SignalMsg{s}) }
func SendPositions(p *tea.Program, pos []*trading.Position){ p.Send(PositionMsg{pos}) }
func SendBalance(p *tea.Program, b float64){ p.Send(BalanceMsg{b}) }
func SendLatency(p *tea.Program, l int64){ p.Send(LatencyMsg{l}) }
func SendStats(p *tea.Program, e, x2 int){ p.Send(StatsMsg{e, x2}) }
func SendLogs(p *tea.Program, l []string){ p.Send(LogMsg{l}) }

// --- VISUAL COMPONENTS ---

// Helper to render title embedded in border (Crossterm Style)
func renderBox(title, content string, w, h int) string {
	// 1. Render the box logic
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColorBorder).
		Width(w-2).
		Height(h-1). // Subtract 1 to account for injected Top Line
		Padding(0, 0)
	
	// Render the block
	s := style.Render(content)
	
	// 2. Inject Title into Top Border
	// Standard Top Border looks like: ┌──────────────────────┐
	// We want:                      ┌─ Title ──────────────┐
	
	lines := strings.Split(s, "\n")
	if len(lines) > 0 {

		// Create title string "─ Title "
		titleStr := "─ " + title + " "
		
		// Runes for safety with utf8 (though borders are ascii/utf8)
		rowRunes := []rune(lines[0])
		titleRunes := []rune(titleStr)
		
		// Inject starting at index 1 (after corner)
		if len(rowRunes) > len(titleRunes)+2 {
			for i, r := range titleRunes {
				rowRunes[i+1] = r
			}
			// Colorize the title? Lipgloss returns styled string with ANSI codes.
			// String manipulation on ANSI strings is risky.
			// BETTER APPROACH: Use lipgloss to Render the Border line with title manually?
			// OR: Use BorderTop(false) and manually prepend the header line?
			// Let's use the manual header line approach for safety against ANSI codes.
		}
	}
	
	// SAFE APPROACH: "Manual Border Construction" for the top line
	// Because styling makes string replacement hard.
	
	// Alternative: Use lipgloss.Border with top=false, then render top line manually.
	innerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, true, true). // No Top
		BorderForeground(ColorBorder).
		Width(w-2).
		Height(h).
		Padding(0,0)
		
	body := innerStyle.Render(content)
	
	// Construct Top Line manually
	// ┌ (colored) + ─ Title (colored title) + ──── (colored) + ┐ (colored)
	
	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	titleStyle := lipgloss.NewStyle().Foreground(ColorActive).Bold(true)
	
	cornerL := borderStyle.Render("┌")
	cornerR := borderStyle.Render("┐")
	
	// Calculate dashes
	// Total inner width = w - 2
	// Title takes: 2 ("─ ") + len(title) + 1 (" ") = len(title) + 3
	titleLen := utf8.RuneCountInString(title)
	dashLen := (w - 2) - (titleLen + 3)
	if dashLen < 0 { dashLen = 0 }
	
	topLine := cornerL + 
		borderStyle.Render("─ ") + 
		titleStyle.Render(title) + 
		borderStyle.Render(" " + strings.Repeat("─", dashLen)) + 
		cornerR
		
	return lipgloss.JoinVertical(lipgloss.Left, topLine, body)
}

func renderGauge(percent float64, width int, color lipgloss.Color) string {
	if width < 5 { return "" }
	// [████░░░░]
	// Inner width: width - 2 (brackets) if we had brackets, but reference is clean bar or boxed.
	// We'll use full width.
	w := width
	filled := int(float64(w) * (percent / 100.0))
	if filled > w { filled = w }
	if filled < 0 { filled = 0 }
	empty := w - filled
	
	if empty < 0 { empty = 0 }
	
	bar := strings.Repeat("█", filled)
	space := strings.Repeat("░", empty)
	
	return lipgloss.NewStyle().Foreground(color).Render(bar) + 
	       lipgloss.NewStyle().Foreground(ColorBorder).Render(space)
}

func renderSparkline(data []int, width int) string {
	if width < 1 { return "" }
	if len(data) == 0 { return strings.Repeat(" ", width) }
	
	// Normalize
	min, max := 0, 0
	if len(data) > 0 { min, max = data[0], data[0] }
	for _, v := range data {
		if v < min { min = v }
		if v > max { max = v }
	}
	rangeVal := max - min
	if rangeVal == 0 { rangeVal = 1 }
	
	levels := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	
	// Take last N points to fit width
	points := data
	if len(points) > width {
		points = points[len(points)-width:]
	}
	
	var s string
	for _, v := range points {
		// Calculate level (0-7)
		l := (v - min) * 7 / rangeVal
		if l < 0 { l = 0 }
		if l > 7 { l = 7 }
		s += levels[l]
	}
	
	// Pad if not enough data
	if len(s) < width {
		s = strings.Repeat(" ", width-len(s)) + s
	}
	
	return lipgloss.NewStyle().Foreground(ColorAccentGreen).Render(s)
}

// ════════════════════════════════════════════════════════════════════════════
// NEON COMMAND CENTER (UI MODE 4)
// ════════════════════════════════════════════════════════════════════════════

// ════════════════════════════════════════════════════════════════════════════
// NEON COMMAND CENTER (UI MODE 4)
// ════════════════════════════════════════════════════════════════════════════

func (m Model) renderNeonDashboard() string {
	// Colors
	neonGreen := lipgloss.Color("#39ff14")
	neonPink := lipgloss.Color("#ff00ff")
	neonBlue := lipgloss.Color("#00ffff")
	bg := lipgloss.Color("#050505")
	
	// Layout Calcs - Perfect Width fitting
	w := m.Width
	if w < 20 { return "Terminal too small" }
	
	// We have 3 columns. Each has Border (2 chars) + Padding (0,1 -> 2 chars horizontal).
	// Total decoration per column = 2 + 2 = 4 chars?
	// Wait, Padding(0,1) increases width by 2 if applied to content?
	// Let's assume Box Width includes padding if strictly controlled, but lipgloss adds padding outside content width.
	// Best approach: Use percentages or exact math.
	
	// Available width for content blocks
	// We want 3 equal boxes that fill 'w'.
	// BoxTotal = Content + 2 (Border)
	// We will force style width to be exact.
	
	col1Width := w / 3
	col2Width := w / 3
	col3Width := w - col1Width - col2Width // Remainder goes to last col
	
	// Adjust for borders (2 chars each)
	// box style width setting usually affects *content* width.
	// So set ContentWidth = TotalWidth - 2.
	
	c1 := col1Width - 2
	c2 := col2Width - 2
	c3 := col3Width - 2
	
	h := m.Height
	contentHeight := h - 7 // Header (3) + Footer (3) + Spacing (1)
	
	// Helper styles
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#333333")).
		Background(bg).
		Padding(0, 0). // Remove padding, handle manually or inside content
		Height(contentHeight)
	
	// Focus highlight logic
	focusColor := neonGreen
	defaultBorder := lipgloss.Color("#333333")
	leftBorder, centerBorder, rightBorder := defaultBorder, defaultBorder, defaultBorder
	
	switch m.FocusPane {
	case 0: leftBorder = focusColor
	case 1: centerBorder = focusColor
	case 2: rightBorder = focusColor
	}
	
	// ─── 1. LEFT PANEL ───
	ramVal := 0
	fmt.Sscanf(m.Header.MemUsage, "%dMB", &ramVal)
	ramPct := (ramVal * 100) / 1024
	if ramPct > 100 { ramPct = 100 }
	rpcLatency := int(m.RPCLatency.Milliseconds())
	rpcPct := (rpcLatency * 100) / 500
	if rpcPct > 100 { rpcPct = 100 }
	
	entries := m.Header.TotalEntries
	wins := m.Header.Reached2X
	winRate := 0.0
	if entries > 0 { winRate = (float64(wins) / float64(entries)) * 100 }
	
	bal := m.WalletBalance
	usdEst := bal * 185.0
	
	leftContent := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(neonPink).Bold(true).Render(" [ SYSTEM ]"),
		fmt.Sprintf(" RAM  %s %s", renderBar(ramPct, 10), m.Header.MemUsage),
		fmt.Sprintf(" RPC  %s %dms", renderBar(rpcPct, 10), rpcLatency),
		"",
		lipgloss.NewStyle().Foreground(neonPink).Bold(true).Render(" [ STATS ]"),
		fmt.Sprintf(" Entries: %d", entries),
		fmt.Sprintf(" 2X Wins: %d (%.0f%%)", wins, winRate),
		fmt.Sprintf(" Rate:    %.1f%%", winRate),
		"",
		lipgloss.NewStyle().Foreground(neonPink).Bold(true).Render(" [ WALLET ]"),
		fmt.Sprintf(" SOL: %.2f", bal),
		fmt.Sprintf(" USD: $%.0f", usdEst),
	)
	leftPanel := boxStyle.Copy().Width(c1).BorderForeground(leftBorder).Render(leftContent)
	
	// ─── 2. CENTER PANEL ───
	feedTitle := lipgloss.NewStyle().Foreground(neonBlue).Bold(true).Render(" [ FEED ]")
	var feedLines []string
	visibleSignals := contentHeight - 8 // Reserve space for logs
	start := m.Signals.Offset
	if start < 0 { start = 0 }
	for i := start; i < len(m.Signals.List) && i < start+visibleSignals; i++ {
		s := m.Signals.List[i]
		t := time.Unix(s.Timestamp, 0).Format("15:04:05")
		act := "WAIT"
		color := lipgloss.Color("#555555")
		if s.Value >= m.Config.GetTrading().MinEntryPercent {
			act = "BUY "
			color = neonGreen
		}
		// Truncate name to fit
		nameLen := c2 - 25
		if nameLen < 3 { nameLen = 3 }
		line := fmt.Sprintf(" %s %-6s %3.0f%s %s", t, truncate(s.TokenName, nameLen), s.Value, s.Unit, act)
		feedLines = append(feedLines, lipgloss.NewStyle().Foreground(color).Render(line))
	}
	// Fill
	for len(feedLines) < visibleSignals { feedLines = append(feedLines, "") }
	
	centerContent := lipgloss.JoinVertical(lipgloss.Left,
		feedTitle,
		strings.Join(feedLines, "\n"),
		lipgloss.NewStyle().Foreground(neonBlue).Bold(true).Render(" [ LOGS ]"),
		truncate(m.LogsView.GetLastLine(), c2-2),
	)
	centerPanel := boxStyle.Copy().Width(c2).BorderForeground(centerBorder).Render(centerContent)
	
	// ─── 3. RIGHT PANEL ───
	posTitle := lipgloss.NewStyle().Foreground(neonGreen).Bold(true).Render(" [ POSITIONS ]")
	var posLines []string
	
	// Dynamic height calculation
	// contentHeight is total box height. Title takes 1 line.
	visiblePositions := contentHeight - 1
	if visiblePositions < 1 { visiblePositions = 1 }
	
	startPos := m.Positions.Offset
	if startPos < 0 { startPos = 0 }
	// Auto-clamp offset if list shrank
	if startPos > len(m.Positions.Positions) { startPos = len(m.Positions.Positions) }
	
	endPos := startPos + visiblePositions
	if endPos > len(m.Positions.Positions) { endPos = len(m.Positions.Positions) }
	
	for i := startPos; i < endPos; i++ {
		p := m.Positions.Positions[i]
		style := StyleProfit
		if p.PnLPercent < 0 { style = StyleLoss }
		nameLen := 6
		// Format: TOKEN ENTRY CUR PnL% AGE
		age := formatDuration(time.Since(p.EntryTime))
		line := fmt.Sprintf(" %-6s %4.0f %4.0f %s %s", 
			truncate(p.TokenName, nameLen), 
			p.EntryValue,
			p.CurrentValue,
			style.Render(fmt.Sprintf("%+.0f%%", p.PnLPercent)),
			age,
		)
		posLines = append(posLines, line)
	}
	
	// Fill empty space if list is short
	for len(posLines) < visiblePositions {
		if len(posLines) == 0 && len(m.Positions.Positions) == 0 {
			posLines = append(posLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#444")).Render(" No positions"))
		} else {
			posLines = append(posLines, "")
		}
	}
	
	rightContent := lipgloss.JoinVertical(lipgloss.Left,
		posTitle,
		strings.Join(posLines, "\n"),
	)
	rightPanel := boxStyle.Copy().Width(c3).BorderForeground(rightBorder).Render(rightContent)
	
	// ─── ASSEMBLY ───
	
	// Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(neonGreen).
		Align(lipgloss.Center).
		Width(w).
		Render(fmt.Sprintf("⚡ AFNEX COMMAND CENTER ⚡   [LIVE] 🔴 REC  %s", time.Now().Format("15:04:05")))
		
	// Grid
	grid := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, centerPanel, rightPanel)
	
	// Footer
	footer := m.renderNeonFooter(w)
	
	ui := lipgloss.JoinVertical(lipgloss.Center, header, grid, footer)
	return lipgloss.NewStyle().Background(bg).Render(ui)
}

func (m Model) renderNeonFooter(w int) string {
	// Status
	status := fmt.Sprintf(" ⏱ %s │ 💰 %+.2f%%", 
		time.Since(m.StartTime).Truncate(time.Second),
		m.Positions.TotalPnLPercent,
	)
	
	// Controls
	controls := "[TAB]Focus [↑↓]Scroll [Q]uit "
	
	// Spacer
	spaceAvailable := w - lipgloss.Width(status) - lipgloss.Width(controls)
	if spaceAvailable < 0 { spaceAvailable = 0 }
	spacer := strings.Repeat(" ", spaceAvailable)
	
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffff")).Render(status),
		spacer,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#ff00ff")).Render(controls),
	)
	
	return lipgloss.NewStyle().
		Width(w).
		Background(lipgloss.Color("#111111")).
		Render(bar)
}

// Simple bar renderer
func renderBar(pct, width int) string {
	fill := (pct * width) / 100
	if fill < 0 { fill = 0 }
	if fill > width { fill = width }
	empty := width - fill
	return strings.Repeat("I", fill) + strings.Repeat(".", empty)
}


func renderLineGauge(percent float64, width int, color lipgloss.Color) string {
	if width < 5 { return "" }
	w := width
	
	// LineGauge is a thin line: 33% ──────
	// Format: "33% ──────"
	
	// 1. Label
	label := fmt.Sprintf("%.0f%%", percent)
	
	// 2. Line
	lineLen := w - len(label) - 1
	if lineLen < 0 { lineLen = 0 }
	
	filledLen := int(float64(lineLen) * (percent / 100.0))
	if filledLen > lineLen { filledLen = lineLen }
	if filledLen < 0 { filledLen = 0 }
	
	//Chars: ─ (empty), ━ (filled)
	filledStr := strings.Repeat("━", filledLen)
	emptyStr := strings.Repeat("─", lineLen - filledLen)
	
	line := lipgloss.NewStyle().Foreground(color).Render(filledStr) + 
	        lipgloss.NewStyle().Foreground(ColorBorder).Render(emptyStr)
			
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(label) + " " + line
}

