package tui

import "github.com/charmbracelet/lipgloss"

// ═══════════════════════════════════════════════════════════════════════════════
// NEO-SOL THEME ENGINE - Dynamic Color Palettes for TUI Modes
// ═══════════════════════════════════════════════════════════════════════════════

// Theme defines a complete color scheme for the TUI
type Theme struct {
	Name         string
	Background   lipgloss.Color
	Border       lipgloss.Color
	Text         lipgloss.Color
	Active       lipgloss.Color
	AccentGreen  lipgloss.Color
	AccentPurple lipgloss.Color
	Profit       lipgloss.Color
	Loss         lipgloss.Color
	Warning      lipgloss.Color
	Muted        lipgloss.Color
}

// ─── MODE 1: CLASSIC (The "Analyst" State) ───
// Best for: Reviewing logs, debugging, standard office lighting
var ClassicTheme = Theme{
	Name:         "CLASSIC",
	Background:   lipgloss.Color("#0000aa"), // BIOS Blue
	Border:       lipgloss.Color("#aaaaaa"), // DOS Gray
	Text:         lipgloss.Color("#ffffff"), // Pure White
	Active:       lipgloss.Color("#00aaaa"), // Cyan
	AccentGreen:  lipgloss.Color("#00aa00"), // Green
	AccentPurple: lipgloss.Color("#aa00aa"), // Magenta
	Profit:       lipgloss.Color("#00aa00"), // Green
	Loss:         lipgloss.Color("#aa0000"), // Red
	Warning:      lipgloss.Color("#ffff00"), // Retro Yellow
	Muted:        lipgloss.Color("#555555"), // Dark Gray
}

// ─── MODE 2: CROSSTERM (The "Legacy" State) ───
// Best for: High contrast, readability on bad monitors
var CrosstermTheme = Theme{
	Name:         "CROSSTERM",
	Background:   lipgloss.Color("#1a1a1a"), // Dark Gray
	Border:       lipgloss.Color("#ffb000"), // Amber Monitor
	Text:         lipgloss.Color("#ffcc00"), // Amber Text
	Active:       lipgloss.Color("#ffb000"), // Amber
	AccentGreen:  lipgloss.Color("#00ff00"), // Green
	AccentPurple: lipgloss.Color("#5c4000"), // Burnt Amber
	Profit:       lipgloss.Color("#00ff00"), // Green
	Loss:         lipgloss.Color("#ff0000"), // Red
	Warning:      lipgloss.Color("#ffff00"), // Yellow
	Muted:        lipgloss.Color("#804000"), // Dark Amber
}

// ─── MODE 3: MATRIX / CRT (The "Hacker" State) ───
// Best for: Low-light coding, focusing on data streams
// PHOSPHOR GREEN PALETTE - Retro-Futuristic CRT Terminal aesthetic
var MatrixTheme = Theme{
	Name:         "MATRIX",
	Background:   lipgloss.Color("#000000"), // Pure Black (CRT Void)
	Border:       lipgloss.Color("#33FF33"), // Phosphor Glow Green
	Text:         lipgloss.Color("#E0FFE0"), // Pale Green (Readable)
	Active:       lipgloss.Color("#33FF33"), // Phosphor Glow Green
	AccentGreen:  lipgloss.Color("#33FF33"), // Phosphor Glow Green
	AccentPurple: lipgloss.Color("#004400"), // Dark Green (Dim Labels)
	Profit:       lipgloss.Color("#33FF33"), // Phosphor Glow Green
	Loss:         lipgloss.Color("#FF3333"), // Alert Red
	Warning:      lipgloss.Color("#FFFF00"), // Yellow
	Muted:        lipgloss.Color("#005500"), // Dark Green (Secondary)
}

// ─── MODE 4: NEON (The "Hunter" State) ───
// Best for: Active trading, high-adrenaline environments
var NeonTheme = Theme{
	Name:         "NEON",
	Background:   lipgloss.Color("#0d0d15"), // Deep Void
	Border:       lipgloss.Color("#ff00ff"), // Hyper-Magenta
	Text:         lipgloss.Color("#ffffff"), // Pure White
	Active:       lipgloss.Color("#00ffff"), // Cyber-Cyan
	AccentGreen:  lipgloss.Color("#00ff9f"), // Spring Green
	AccentPurple: lipgloss.Color("#ff00ff"), // Hyper-Magenta
	Profit:       lipgloss.Color("#00ff9f"), // Spring Green
	Loss:         lipgloss.Color("#ff0055"), // Razor Red
	Warning:      lipgloss.Color("#ffcc00"), // Warning Yellow
	Muted:        lipgloss.Color("#666666"), // Dim Gray
}

// ─── MODE 5: FNEX (User's Custom Mockup) ───
// Based on the provided mockup images
var FnexTheme = Theme{
	Name:         "FNEX",
	Background:   lipgloss.Color("#0d1117"), // Dark Teal/Navy
	Border:       lipgloss.Color("#00ffff"), // Cyan
	Text:         lipgloss.Color("#ffffff"), // White
	Active:       lipgloss.Color("#00ffff"), // Cyan
	AccentGreen:  lipgloss.Color("#00ff00"), // Bright Green
	AccentPurple: lipgloss.Color("#ff00ff"), // Magenta
	Profit:       lipgloss.Color("#00ff00"), // Bright Green
	Loss:         lipgloss.Color("#ff3366"), // Red
	Warning:      lipgloss.Color("#ff9900"), // Orange
	Muted:        lipgloss.Color("#666666"), // Gray
}

// Themes contains all available themes in order (matches UIMode 1-5)
var Themes = []Theme{
	ClassicTheme,   // Mode 1 - CLASSIC
	CrosstermTheme, // Mode 2 - CROSSTERM
	MatrixTheme,    // Mode 3 - MATRIX
	NeonTheme,      // Mode 4 - NEON
	FnexTheme,      // Mode 5 - FNEX
}

// CurrentThemeIndex tracks which theme is active (0-indexed)
var CurrentThemeIndex = 4 // Default to FNEX (Mode 5)

// GetTheme returns the current theme
func GetTheme() Theme {
	if CurrentThemeIndex >= 0 && CurrentThemeIndex < len(Themes) {
		return Themes[CurrentThemeIndex]
	}
	return FnexTheme
}

// GetThemeByMode returns a theme by mode index (1-indexed UIMode)
func GetThemeByMode(modeIndex int) Theme {
	if modeIndex >= 1 && modeIndex <= len(Themes) {
		return Themes[modeIndex-1]
	}
	return FnexTheme
}

// CycleTheme switches to the next theme
func CycleTheme() {
	CurrentThemeIndex = (CurrentThemeIndex + 1) % len(Themes)
	ApplyTheme(Themes[CurrentThemeIndex])
}

// SetThemeByMode sets the theme based on UIMode (1-indexed)
func SetThemeByMode(modeIndex int) {
	if modeIndex >= 1 && modeIndex <= len(Themes) {
		CurrentThemeIndex = modeIndex - 1
		ApplyTheme(Themes[CurrentThemeIndex])
	}
}

// ApplyTheme updates the global color variables to match the theme
func ApplyTheme(t Theme) {
	ColorBg = t.Background
	ColorBorder = t.Border
	ColorText = t.Text
	ColorActive = t.Active
	ColorAccentGreen = t.AccentGreen
	ColorAccentPurple = t.AccentPurple
	ColorProfit = t.Profit
	ColorLoss = t.Loss

	// Update styles that derive from colors
	StylePage = lipgloss.NewStyle().Background(ColorBg).Foreground(ColorText)
	StyleHeader = lipgloss.NewStyle().Bold(true).Foreground(ColorActive)
	StyleKey = lipgloss.NewStyle().Foreground(ColorAccentPurple).Bold(true)
	StyleProfit = lipgloss.NewStyle().Foreground(ColorProfit)
	StyleLoss = lipgloss.NewStyle().Foreground(ColorLoss)
	StyleTableHeader = lipgloss.NewStyle().Foreground(ColorActive).Bold(true)
	StyleFooter = lipgloss.NewStyle().Foreground(ColorText)
	StyleModal = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(ColorBorder).Padding(1, 2)
	ColorGray = t.Muted
}
