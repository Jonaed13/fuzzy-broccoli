package tui

import "github.com/charmbracelet/lipgloss"

// Theme defines a color scheme for the TUI
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
}

// Predefined themes
var Themes = []Theme{
	// 0: Tokyo Night (Crossterm Demo style)
	{
		Name:         "Tokyo Night",
		Background:   lipgloss.Color("#1a1b26"),
		Border:       lipgloss.Color("#7aa2f7"),
		Text:         lipgloss.Color("#c0caf5"),
		Active:       lipgloss.Color("#7aa2f7"),
		AccentGreen:  lipgloss.Color("#9ece6a"),
		AccentPurple: lipgloss.Color("#bb9af7"),
		Profit:       lipgloss.Color("#9ece6a"),
		Loss:         lipgloss.Color("#f7768e"),
	},
	// 1: Light
	{
		Name:         "Light",
		Background:   lipgloss.Color("#ffffff"),
		Border:       lipgloss.Color("#0969da"),
		Text:         lipgloss.Color("#24292f"),
		Active:       lipgloss.Color("#0550ae"),
		AccentGreen:  lipgloss.Color("#1a7f37"),
		AccentPurple: lipgloss.Color("#8250df"),
		Profit:       lipgloss.Color("#1a7f37"),
		Loss:         lipgloss.Color("#cf222e"),
	},
	// 2: Cyberpunk/Neon
	{
		Name:         "Cyberpunk",
		Background:   lipgloss.Color("#0a0a0a"),
		Border:       lipgloss.Color("#00ffff"),
		Text:         lipgloss.Color("#ffffff"),
		Active:       lipgloss.Color("#ff00ff"),
		AccentGreen:  lipgloss.Color("#39ff14"),
		AccentPurple: lipgloss.Color("#bf00ff"),
		Profit:       lipgloss.Color("#39ff14"),
		Loss:         lipgloss.Color("#ff0000"),
	},
}

// CurrentThemeIndex tracks which theme is active
var CurrentThemeIndex = 0

// GetTheme returns the current theme
func GetTheme() Theme {
	return Themes[CurrentThemeIndex]
}

// CycleTheme switches to the next theme
func CycleTheme() {
	CurrentThemeIndex = (CurrentThemeIndex + 1) % len(Themes)
	ApplyTheme(Themes[CurrentThemeIndex])
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
	ColorGray = ColorText
}
