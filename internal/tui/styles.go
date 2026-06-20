package tui

import "github.com/charmbracelet/lipgloss"

// Palette — Catppuccin Mocha + the Charm purple accent, matching lazyports so
// the two tools feel like one family.
var (
	colPrimary = lipgloss.Color("#7D56F4") // charm purple — selection / brand
	colAccent  = lipgloss.Color("#04B575") // green — ok / titles
	colWarning = lipgloss.Color("#F9E2AF") // yellow — caution
	colError   = lipgloss.Color("#F38BA8") // red — danger / errors
	colSubtle  = lipgloss.Color("#6C7086") // gray — labels / hints
	colText    = lipgloss.Color("#CDD6F4") // foreground
	colPanel   = lipgloss.Color("#181825") // panel background
	colBorder  = lipgloss.Color("#313244") // borders
	colCyan    = lipgloss.Color("#89DCEB") // sizes / values
	colWhite   = lipgloss.Color("#FFFFFF")
	colDark    = lipgloss.Color("#1E1E2E")
)

var (
	headerStyle = lipgloss.NewStyle().
			Background(colPrimary).
			Foreground(colWhite).
			Bold(true).
			Padding(0, 1)

	brandStyle = lipgloss.NewStyle().
			Foreground(colPrimary).
			Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Background(colPanel).
			Foreground(colSubtle).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	panelStyleActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colPrimary).
				Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true).
			Padding(0, 1)

	panelSubtitleStyle = lipgloss.NewStyle().
				Foreground(colSubtle).
				Italic(true)

	footerStyle = lipgloss.NewStyle().
			Background(colPanel).
			Foreground(colSubtle).
			Padding(0, 1)

	footerKeyStyle  = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	footerDescStyle = lipgloss.NewStyle().Foreground(colText)

	rowSelectedStyle = lipgloss.NewStyle().
				Background(colPrimary).
				Foreground(colWhite).
				Bold(true)

	rowNormalStyle = lipgloss.NewStyle().Foreground(colText)

	folderHeaderStyle = lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	sectionTitleStyle = lipgloss.NewStyle().Foreground(colCyan).Bold(true)

	okStyle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	errStyle  = lipgloss.NewStyle().Foreground(colError).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(colWarning).Bold(true)
	sizeStyle = lipgloss.NewStyle().Foreground(colCyan)
	dateStyle = lipgloss.NewStyle().Foreground(colSubtle)

	detailLabelStyle = lipgloss.NewStyle().Foreground(colSubtle).Width(10)
	detailValueStyle = lipgloss.NewStyle().Foreground(colText)
	detailDimStyle   = lipgloss.NewStyle().Foreground(colSubtle).Italic(true)
	statStyle        = lipgloss.NewStyle().Foreground(colCyan).Bold(true)

	helpStyle = lipgloss.NewStyle().Foreground(colSubtle).Italic(true)

	// Breadcrumb for the wizards ("Base ─ [Carpeta] ─ Confirmar").
	crumbStyle       = lipgloss.NewStyle().Foreground(colSubtle)
	crumbActiveStyle = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)

	// Live log lines during a running operation.
	logLineStyle  = lipgloss.NewStyle().Foreground(colSubtle)
	logTitleStyle = lipgloss.NewStyle().Foreground(colCyan).Bold(true)

	// Modals: rounded (confirm), double (warning/destructive), thick (error).
	// No Background: the box and its inner text share the terminal background,
	// so there are no mismatched dark blocks behind the content.
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colPrimary).
			Padding(1, 3)

	modalWarnStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colWarning).
			Padding(1, 3)

	modalErrorStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(colError).
			Padding(1, 3)

	modalTitleStyle      = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	modalTitleWarnStyle  = lipgloss.NewStyle().Foreground(colWarning).Bold(true)
	modalTitleErrorStyle = lipgloss.NewStyle().Foreground(colError).Bold(true)

	modalLabelStyle   = lipgloss.NewStyle().Foreground(colSubtle)
	modalValueStyle   = lipgloss.NewStyle().Foreground(colText).Bold(true)
	modalDangerStyle  = lipgloss.NewStyle().Foreground(colError).Bold(true)
	modalHintStyle    = lipgloss.NewStyle().Foreground(colSubtle).Italic(true)
	modalDividerStyle = lipgloss.NewStyle().Foreground(colBorder)

	modalButtonKeyStyle = lipgloss.NewStyle().
				Background(colPrimary).Foreground(colWhite).Bold(true).Padding(0, 1)
	modalButtonDangerKeyStyle = lipgloss.NewStyle().
					Background(colError).Foreground(colWhite).Bold(true).Padding(0, 1)
	modalButtonCancelKeyStyle = lipgloss.NewStyle().
					Background(colSubtle).Foreground(colWhite).Bold(true).Padding(0, 1)
	modalButtonDescStyle = lipgloss.NewStyle().Foreground(colText)

	logoStyle = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
)

// statusGlyph returns a colored ✓ / ✗ for a dump's validity.
func statusGlyph(valid bool) string {
	if valid {
		return okStyle.Render("✓")
	}
	return errStyle.Render("✗")
}
