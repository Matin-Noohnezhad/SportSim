package tui

import "github.com/charmbracelet/lipgloss"

// Colours are declared as adaptive pairs so the interface stays legible on both
// light and dark terminal themes.
var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#1c6fd3", Dark: "#63a4ff"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#8b949e"}
	colGood    = lipgloss.AdaptiveColor{Light: "#137a3c", Dark: "#4ec97e"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#a35b00", Dark: "#e3a038"}
	colBad     = lipgloss.AdaptiveColor{Light: "#b3261e", Dark: "#f2726a"}
	colText    = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#e6edf3"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#c9ced6", Dark: "#39414d"}
	colInverse = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#0d1117"}
)

var (
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	stMuted = lipgloss.NewStyle().Foreground(colMuted)
	stGood  = lipgloss.NewStyle().Foreground(colGood)
	stWarn  = lipgloss.NewStyle().Foreground(colWarn)
	stBad   = lipgloss.NewStyle().Foreground(colBad)
	stBold  = lipgloss.NewStyle().Bold(true).Foreground(colText)

	stHeader = lipgloss.NewStyle().Bold(true).Foreground(colMuted)

	stSelected = lipgloss.NewStyle().Bold(true).
			Foreground(colInverse).Background(colAccent)

	stBar = lipgloss.NewStyle().
		Foreground(colMuted).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		BorderTop(true).Padding(0, 1)

	stPanel = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Padding(0, 1)

	stStatusOK  = lipgloss.NewStyle().Foreground(colGood)
	stStatusErr = lipgloss.NewStyle().Foreground(colBad)
)

// ratingStyle colours a player rating so squad quality reads at a glance.
func ratingStyle(v int) lipgloss.Style {
	switch {
	case v >= 82:
		return stGood.Bold(true)
	case v >= 74:
		return stGood
	case v >= 66:
		return lipgloss.NewStyle().Foreground(colText)
	case v >= 58:
		return stWarn
	default:
		return stBad
	}
}

// meterStyle colours a 0-100 gauge such as fitness or morale.
func meterStyle(v int) lipgloss.Style {
	switch {
	case v >= 80:
		return stGood
	case v >= 55:
		return stWarn
	default:
		return stBad
	}
}

// formStyle colours a W/D/L character.
func formStyle(c byte) lipgloss.Style {
	switch c {
	case 'W':
		return stGood
	case 'D':
		return stMuted
	default:
		return stBad
	}
}
