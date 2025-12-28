package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Animation frame rate
const (
	AnimationFPS     = 30
	AnimationTickMs  = 1000 / AnimationFPS // ~33ms
	StartupDuration  = 60                  // 60 frames = 2 seconds
	ButtonFlashDur   = 6                   // 6 frames = 200ms
	TransitionDur    = 9                   // 9 frames = 300ms
)

// AnimationType identifies what animation is playing
type AnimationType int

const (
	AnimNone AnimationType = iota
	AnimStartup
	AnimButtonFlash
	AnimTransition
)

// Animation represents a single animation instance
type Animation struct {
	Type      AnimationType
	Frame     int
	MaxFrames int
	Active    bool
	Target    string // Button name or pane ID
}

// AnimationState manages all active animations
type AnimationState struct {
	Startup    Animation
	Button     Animation
	Transition Animation
	StartTime  time.Time
	
	// Continuous animation counter (never stops in Mode 3)
	GlobalFrame int
}

// NewAnimationState creates fresh animation state with startup animation
func NewAnimationState() AnimationState {
	return AnimationState{
		Startup: Animation{
			Type:      AnimStartup,
			Frame:     0,
			MaxFrames: StartupDuration,
			Active:    true,
		},
		StartTime: time.Now(),
	}
}

// Tick advances all active animations by one frame
func (a *AnimationState) Tick() bool {
	// Always increment global frame for continuous animations
	a.GlobalFrame++
	
	anyActive := true // Always active in Mode 3 for continuous animations
	
	if a.Startup.Active {
		a.Startup.Frame++
		if a.Startup.Frame >= a.Startup.MaxFrames {
			a.Startup.Active = false
		}
	}
	
	if a.Button.Active {
		a.Button.Frame++
		if a.Button.Frame >= a.Button.MaxFrames {
			a.Button.Active = false
		}
	}
	
	if a.Transition.Active {
		a.Transition.Frame++
		if a.Transition.Frame >= a.Transition.MaxFrames {
			a.Transition.Active = false
		}
	}
	
	return anyActive
}

// TriggerButtonFlash starts button click animation
func (a *AnimationState) TriggerButtonFlash(buttonName string) {
	a.Button = Animation{
		Type:      AnimButtonFlash,
		Frame:     0,
		MaxFrames: ButtonFlashDur,
		Active:    true,
		Target:    buttonName,
	}
}

// TriggerTransition starts pane transition animation
func (a *AnimationState) TriggerTransition(targetPane string) {
	a.Transition = Animation{
		Type:      AnimTransition,
		Frame:     0,
		MaxFrames: TransitionDur,
		Active:    true,
		Target:    targetPane,
	}
}

// IsAnimating returns true if any animation is active
// For Mode 3, we always want continuous animations running
func (a *AnimationState) IsAnimating() bool {
	// Always return true to keep continuous animations running
	// (pulsing gauge, wave sparkline, border color cycling)
	return true
}

// AnimationTickMsg is sent on each animation frame
type AnimationTickMsg time.Time

// AnimationTickCmd creates the tick command for animations
func AnimationTickCmd() tea.Cmd {
	return tea.Tick(time.Duration(AnimationTickMs)*time.Millisecond, func(t time.Time) tea.Msg {
		return AnimationTickMsg(t)
	})
}

// --- ANIMATION RENDER HELPERS ---

// GetStartupProgress returns 0.0-1.0 progress of startup animation
func (a *AnimationState) GetStartupProgress() float64 {
	if !a.Startup.Active && a.Startup.Frame >= a.Startup.MaxFrames {
		return 1.0
	}
	return float64(a.Startup.Frame) / float64(a.Startup.MaxFrames)
}

// GetButtonFlashActive returns true if button should show flash effect
func (a *AnimationState) GetButtonFlashActive(buttonName string) bool {
	return a.Button.Active && a.Button.Target == buttonName
}

// FadeChar returns a density character based on progress (0.0-1.0)
func FadeChar(progress float64) rune {
	// Unicode block characters by density: ░▒▓█
	switch {
	case progress < 0.25:
		return ' '
	case progress < 0.50:
		return '░'
	case progress < 0.75:
		return '▒'
	case progress < 0.90:
		return '▓'
	default:
		return '█'
	}
}

// TypewriterString returns partial string based on progress
func TypewriterString(s string, progress float64) string {
	if progress >= 1.0 {
		return s
	}
	chars := int(float64(len(s)) * progress)
	if chars > len(s) {
		chars = len(s)
	}
	return s[:chars]
}

// --- CONTINUOUS ANIMATION HELPERS ---

// PulseValue returns a value that oscillates between min and max
// period is in frames (e.g., 30 = 1 second at 30 FPS)
func (a *AnimationState) PulseValue(min, max float64, period int) float64 {
	if period <= 0 { period = 30 }
	// Sine wave oscillation
	phase := float64(a.GlobalFrame % period) / float64(period)
	// sin returns -1 to 1, map to 0 to 1
	factor := (sin(phase*2*3.14159) + 1) / 2
	return min + (max-min)*factor
}

// sin approximation (avoid math import bloat)
func sin(x float64) float64 {
	// Taylor series approximation for sin
	x = x - float64(int(x/(2*3.14159)))*2*3.14159
	if x > 3.14159 { x -= 2*3.14159 }
	x3 := x * x * x
	x5 := x3 * x * x
	return x - x3/6 + x5/120
}

// WaveOffset returns shifting offset for wave animation
func (a *AnimationState) WaveOffset(period int) int {
	if period <= 0 { period = 15 }
	return a.GlobalFrame / 2 % period
}

// CycleColor returns alternating color based on frame
func (a *AnimationState) CycleIndex(numColors, period int) int {
	if period <= 0 { period = 90 }
	return (a.GlobalFrame / period) % numColors
}

// GetGaugePulse returns gauge fill multiplier (0.95 to 1.05)
func (a *AnimationState) GetGaugePulse() float64 {
	return a.PulseValue(0.95, 1.05, 30)
}

// GetBorderColorIndex returns cycling color index for animated borders
func (a *AnimationState) GetBorderColorIndex() int {
	return a.CycleIndex(3, 45) // 3 colors, change every 1.5s
}

