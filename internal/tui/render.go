package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ander0code/pgflow/internal/backups"
)

type keyHint struct{ key, desc string }

var (
	dashboardKeys = []keyHint{
		{"b", "backup"}, {"r", "restore"}, {"v", "verify"}, {"d", "delete"},
		{"t", "túnel"}, {"c", "config"}, {"?", "ayuda"}, {"q", "quit"},
	}
	flowKeys   = []keyHint{{"↑/↓", "elegir"}, {"enter", "ok"}, {"←", "atrás"}, {"esc", "cancelar"}}
	configKeys = []keyHint{{"↑/↓", "campo"}, {"enter", "editar"}, {"l", "probar local"}, {"p", "probar prod"}, {"esc", "volver"}}
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "cargando…"
	}
	if time.Now().Before(m.splashUntil) {
		return m.renderSplash()
	}
	if m.running {
		if m.streaming {
			return m.renderRunScreen()
		}
		return m.overlay(m.renderRunning())
	}
	if m.modal != modalNone {
		return m.overlay(m.renderModal())
	}
	switch m.scr {
	case screenBackup, screenRestore:
		return m.renderFlow()
	case screenConfig:
		return m.renderConfig()
	default:
		return m.renderDashboard()
	}
}

func (m *Model) overlay(content string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// modalWidth is the shared width for centered modal-style boxes (pickers,
// confirm, config, error). It adapts to the terminal but stays readable.
func (m *Model) modalWidth() int {
	w := m.width - 8
	if w > 76 {
		w = 76
	}
	if w < 54 {
		w = 54
	}
	return w
}

// ── splash ───────────────────────────────────────────────────────────────────

func (m *Model) renderSplash() string {
	logo := []string{
		"┌─┐┌─┐┌─┐┬  ┌─┐┬ ┬",
		"├─┘│ ┬├┤ │  │ │││││",
		"┴  └─┘└  ┴─┘└─┘└┴┘",
	}
	block := logoStyle.Render(strings.Join(logo, "\n"))
	sub := helpStyle.Render("backup & restore de PostgreSQL")
	hint := okStyle.Render("b backup · r restore · v verify · t túnel · q quit")
	content := lipgloss.JoinVertical(lipgloss.Center, block, "", sub, "", hint)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// ── dashboard ────────────────────────────────────────────────────────────────

func (m *Model) renderDashboard() string {
	header := m.renderHeader()
	footer := m.renderFooter(dashboardKeys)
	sb := m.renderStatusBar()
	sbH := 0
	if sb != "" {
		sbH = lipgloss.Height(sb)
	}

	avail := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - sbH
	if avail < 6 {
		avail = 6
	}

	leftW := m.width * 58 / 100
	rightW := m.width - leftW - 2
	if rightW < 30 {
		rightW = 30
		leftW = m.width - rightW - 2
	}
	if leftW < 20 {
		leftW = 20
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderBackupList(leftW, avail),
		m.renderDetail(rightW, avail))

	parts := []string{header, body}
	if sb != "" {
		parts = append(parts, sb)
	}
	parts = append(parts, footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderHeader() string {
	left := headerStyle.Render("🐘 pgflow")
	mid := "  " + m.connSummary()

	right := "  escaneando…"
	if !m.lastScan.IsZero() {
		right = fmt.Sprintf("  %d backup(s) · %s", backups.Total(m.folders), m.lastScan.Format("15:04:05"))
	}
	right = dateStyle.Render(right)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, strings.Repeat(" ", gap), right)
}

func (m *Model) connSummary() string {
	if !m.statusKnown {
		return helpStyle.Render("comprobando conexiones…")
	}
	local := okStyle.Render("✓")
	if !m.localOK {
		local = errStyle.Render("✗")
	}
	var prod string
	switch {
	case m.prodOK:
		prod = okStyle.Render("✓")
	case m.tunnelOK:
		prod = warnStyle.Render("sin auth")
	default:
		prod = errStyle.Render("✗")
	}
	tun := errStyle.Render("cerrado")
	if m.tunnelOK {
		tun = okStyle.Render("abierto")
	}
	return fmt.Sprintf("%s %s  ·  %s %s  ·  %s %s",
		dateStyle.Render("local"), local,
		dateStyle.Render("prod"), prod,
		dateStyle.Render("túnel"), tun)
}

func (m *Model) renderBackupList(width, height int) string {
	contentW := width - 4
	if contentW < 16 {
		contentW = 16
	}
	contentH := height - 2
	if contentH < 1 {
		contentH = 1
	}

	hint := "· por proyecto"
	var rows []string
	if len(m.visible) == 0 {
		if m.lastScan.IsZero() {
			rows = append(rows, helpStyle.Render(" escaneando…"))
		} else {
			rows = append(rows,
				"",
				" "+helpStyle.Render("Aún no hay backups."),
				"",
				" "+footerKeyStyle.Render("b")+footerDescStyle.Render(" hace tu primer backup de producción"))
		}
	} else {
		start, end := windowBounds(m.cursor, len(m.visible), contentH)
		for i := start; i < end; i++ {
			rows = append(rows, m.renderDashRow(m.visible[i], i == m.cursor, contentW))
		}
		if sh := scrollHint(m.cursor, len(m.visible), start, end); sh != "" {
			hint = "· por proyecto · " + sh
		}
	}

	title := lipgloss.JoinHorizontal(lipgloss.Bottom,
		panelTitleStyle.Render(fmt.Sprintf(" Backups (%d) ", backups.Total(m.folders))),
		panelSubtitleStyle.Render(hint))
	// Active (focused) panel: purple border tells the user "navega aquí".
	box := panelStyleActive.Width(width - 2).Height(height - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, title, box)
}

func (m *Model) renderDashRow(it dashItem, selected bool, w int) string {
	if it.isHeader {
		arrow := "▾"
		if m.collapsed[it.folder] {
			arrow = "▸"
		}
		label := fmt.Sprintf(" %s %s  (%d)", arrow, it.folder, m.folderCount(it.folder))
		if selected {
			return rowSelectedStyle.Render(padLine(label, w))
		}
		return folderHeaderStyle.Render(label)
	}

	d := it.dump
	nameW := w - 34
	if nameW < 8 {
		nameW = 8
	}
	name := padR(truncate(d.Name, nameW), nameW)
	size := fmt.Sprintf("%8s", humanSize(d.SizeBytes))
	date := d.ModTime.Format("2006-01-02 15:04")

	if selected {
		gl := "✓"
		if !d.Valid {
			gl = "✗"
		}
		return rowSelectedStyle.Render(padLine(fmt.Sprintf("   %s  %s  %s  %s", gl, name, size, date), w))
	}
	return "   " + statusGlyph(d.Valid) + "  " +
		rowNormalStyle.Render(name) + "  " +
		sizeStyle.Render(size) + "  " +
		dateStyle.Render(date)
}

func (m *Model) renderDetail(width, height int) string {
	title := lipgloss.JoinHorizontal(lipgloss.Bottom,
		panelTitleStyle.Render(" Detalle "),
		panelSubtitleStyle.Render("· de lo seleccionado"))

	var content string
	switch it := m.curItem(); {
	case it == nil:
		content = m.renderOverview(width)
	case it.isHeader:
		content = m.renderFolderSummary(it.folder, width)
	default:
		content = m.renderDumpDetail(&it.dump, it.folder, width)
	}

	box := panelStyle.Width(width - 2).Height(height - 2).Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, title, box)
}

// renderOverview is the empty/no-selection state: a project-wide summary plus a
// one-line explanation of how the two panels relate.
func (m *Model) renderOverview(width int) string {
	var size int64
	valid, corrupt := 0, 0
	var newest time.Time
	for _, f := range m.folders {
		for _, d := range f.Dumps {
			size += d.SizeBytes
			if d.Valid {
				valid++
			} else {
				corrupt++
			}
			if d.ModTime.After(newest) {
				newest = d.ModTime
			}
		}
	}
	last := "—"
	if !newest.IsZero() {
		last = newest.Format("2006-01-02 15:04")
	}

	return strings.Join([]string{
		"",
		" " + brandStyle.Render("Resumen"),
		"",
		" " + detailLabelStyle.Render("proyectos") + statStyle.Render(fmt.Sprintf("%d", len(m.folders))),
		" " + detailLabelStyle.Render("backups") + statStyle.Render(fmt.Sprintf("%d", backups.Total(m.folders))),
		" " + detailLabelStyle.Render("tamaño") + statStyle.Render(humanSize(size)),
		" " + detailLabelStyle.Render("válidos") + okStyle.Render(fmt.Sprintf("%d", valid)) + corruptSuffix(corrupt),
		" " + detailLabelStyle.Render("último") + detailValueStyle.Render(last),
		"",
		" " + helpStyle.Render(strings.Repeat("─", maxInt(10, width-8))),
		"",
		" " + detailDimStyle.Render("Izquierda: tus backups por proyecto."),
		" " + detailDimStyle.Render("Aquí: detalle de lo que elijas."),
		"",
		" " + detailDimStyle.Render("prod ─b▶ .dump ─r▶ base local nueva"),
		" " + helpStyle.Render("pulsa ? para ver qué hace cada cosa"),
	}, "\n")
}

// renderFolderSummary is shown when a folder header is selected.
func (m *Model) renderFolderSummary(name string, width int) string {
	var f *backups.Folder
	for i := range m.folders {
		if m.folders[i].Name == name {
			f = &m.folders[i]
			break
		}
	}
	if f == nil {
		return ""
	}
	var size int64
	valid, corrupt := 0, 0
	var newest, oldest time.Time
	for _, d := range f.Dumps {
		size += d.SizeBytes
		if d.Valid {
			valid++
		} else {
			corrupt++
		}
		if newest.IsZero() || d.ModTime.After(newest) {
			newest = d.ModTime
		}
		if oldest.IsZero() || d.ModTime.Before(oldest) {
			oldest = d.ModTime
		}
	}
	fmtT := func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("2006-01-02 15:04")
	}

	return strings.Join([]string{
		"",
		" " + brandStyle.Render("📁 "+name),
		"",
		" " + detailLabelStyle.Render("backups") + statStyle.Render(fmt.Sprintf("%d", len(f.Dumps))),
		" " + detailLabelStyle.Render("tamaño") + statStyle.Render(humanSize(size)),
		" " + detailLabelStyle.Render("válidos") + okStyle.Render(fmt.Sprintf("%d", valid)) + corruptSuffix(corrupt),
		" " + detailLabelStyle.Render("reciente") + detailValueStyle.Render(fmtT(newest)),
		" " + detailLabelStyle.Render("antiguo") + detailValueStyle.Render(fmtT(oldest)),
		"",
		" " + helpStyle.Render(strings.Repeat("─", maxInt(10, width-8))),
		" " + footerKeyStyle.Render("enter") + footerDescStyle.Render(" contraer  ") +
			footerKeyStyle.Render("r") + footerDescStyle.Render(" restaurar"),
	}, "\n")
}

// renderDumpDetail is shown when an individual dump is selected.
func (m *Model) renderDumpDetail(d *backups.Dump, folder string, width int) string {
	valid := okStyle.Render("✓ válido")
	if !d.Valid {
		valid = errStyle.Render("✗ corrupto")
	}
	return strings.Join([]string{
		"",
		" " + brandStyle.Render(truncate(d.Name, width-6)),
		"",
		" " + detailLabelStyle.Render("proyecto") + detailValueStyle.Render(folder),
		" " + detailLabelStyle.Render("tamaño") + sizeStyle.Render(humanSize(d.SizeBytes)),
		" " + detailLabelStyle.Render("objetos") + detailValueStyle.Render(fmt.Sprintf("%d", d.Objects)),
		" " + detailLabelStyle.Render("creado") + detailValueStyle.Render(d.ModTime.Format("2006-01-02 15:04:05")),
		" " + detailLabelStyle.Render("estado") + valid,
		"",
		" " + detailDimStyle.Render("restaurar → "+truncate(suggestLocalName(d.Path), width-16)),
		"",
		" " + helpStyle.Render(strings.Repeat("─", maxInt(10, width-8))),
		" " + footerKeyStyle.Render("r") + footerDescStyle.Render(" restaurar  ") +
			footerKeyStyle.Render("v") + footerDescStyle.Render(" verificar  ") +
			footerKeyStyle.Render("d") + footerDescStyle.Render(" borrar"),
	}, "\n")
}

func corruptSuffix(corrupt int) string {
	if corrupt == 0 {
		return ""
	}
	return "  " + errStyle.Render(fmt.Sprintf("· %d ✗", corrupt))
}

func (m *Model) renderStatusBar() string {
	if m.status == "" {
		return ""
	}
	style := statusBarStyle.Width(m.width)
	if m.statusErr {
		style = style.Background(colError).Foreground(colWhite)
	} else {
		style = style.Background(colAccent).Foreground(colDark)
	}
	return style.Render(" " + m.status + " ")
}

func (m *Model) renderFooter(keys []keyHint) string {
	var parts []string
	for _, k := range keys {
		parts = append(parts,
			footerKeyStyle.Render(" "+k.key+" "), " ",
			footerDescStyle.Render(k.desc), "   ")
	}
	return footerStyle.Width(m.width).Render(strings.Join(parts, ""))
}

// ── live run screen (streaming logs) ─────────────────────────────────────────

func (m *Model) renderRunScreen() string {
	elapsed := time.Since(m.runStart).Round(time.Second)
	left := headerStyle.Render("🐘 pgflow")
	mid := "  " + logTitleStyle.Render(spinnerFrame(m.spinner)+" "+m.runLabel)
	right := panelSubtitleStyle.Render(fmt.Sprintf("%s · %d líneas  ", elapsed, len(m.logLines)))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, strings.Repeat(" ", gap), right)

	footer := footerStyle.Width(m.width).Render(
		footerKeyStyle.Render(" ctrl+c ") + " " + footerDescStyle.Render("salir"))

	avail := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if avail < 4 {
		avail = 4
	}
	body := strings.Join(m.tailLogs(avail-2, m.width-4), "\n")
	box := panelStyleActive.Width(m.width - 2).Height(avail - 2).Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, header, box, footer)
}

func (m *Model) tailLogs(n, w int) []string {
	if n < 1 {
		n = 1
	}
	if len(m.logLines) == 0 {
		return []string{helpStyle.Render("  esperando salida…")}
	}
	start := 0
	if len(m.logLines) > n {
		start = len(m.logLines) - n
	}
	out := make([]string, 0, n)
	for _, l := range m.logLines[start:] {
		out = append(out, logLineStyle.Render(truncate(l, w)))
	}
	return out
}

// ── wizard flows ─────────────────────────────────────────────────────────────

func (m *Model) isConfirmStep() bool {
	return (m.scr == screenBackup && m.step == 3) || (m.scr == screenRestore && m.step == 4)
}

func (m *Model) renderFlow() string {
	header := m.renderFlowHeader()
	footer := m.renderFooter(flowKeys)
	sb := m.renderStatusBar()
	sbH := 0
	if sb != "" {
		sbH = lipgloss.Height(sb)
	}

	avail := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - sbH
	if avail < 6 {
		avail = 6
	}

	var body string
	switch {
	case m.input.Focused():
		body = lipgloss.Place(m.width, avail, lipgloss.Center, lipgloss.Center, m.renderInputPanel())
	case m.isConfirmStep():
		body = lipgloss.Place(m.width, avail, lipgloss.Center, lipgloss.Center, m.renderConfirm())
	default:
		body = m.renderPickerPanel(m.width, avail) // lista a pantalla completa, con scroll
	}

	parts := []string{header, body}
	if sb != "" {
		parts = append(parts, sb)
	}
	parts = append(parts, footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderFlowHeader() string {
	left := headerStyle.Render("🐘 pgflow")
	crumb := "  " + m.crumb()
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(crumb)
	if gap < 0 {
		gap = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, crumb, strings.Repeat(" ", gap))
}

func (m *Model) crumb() string {
	var labels []string
	var total, cur int
	if m.scr == screenBackup {
		labels = []string{"Base", "Carpeta", "Confirmar"}
		total, cur = 3, m.step
	} else {
		labels = []string{"Carpeta", "Backup", "Destino", "Confirmar"}
		total, cur = 4, m.step
		if m.step == 31 {
			cur = 3
		}
	}
	if cur < 1 {
		cur = 1
	}
	if cur > total {
		cur = total
	}

	var b strings.Builder
	b.WriteString(crumbStyle.Render(fmt.Sprintf("Paso %d/%d   ·   ", cur, total)))
	for i, l := range labels {
		if i+1 == cur {
			b.WriteString(crumbActiveStyle.Render("[" + l + "]"))
		} else {
			b.WriteString(crumbStyle.Render(l))
		}
		if i < len(labels)-1 {
			b.WriteString(crumbStyle.Render(" ─ "))
		}
	}
	return b.String()
}

// renderPickerPanel renders the wizard list full-screen, with scroll, so long
// listas (muchos dumps) ya no se parten ni desbordan la pantalla.
func (m *Model) renderPickerPanel(width, height int) string {
	total := len(m.pick.items)
	contentW := width - 4
	if contentW < 20 {
		contentW = 20
	}
	contentH := height - 2
	if contentH < 1 {
		contentH = 1
	}

	hint := ""
	var rows []string
	if total == 0 {
		rows = append(rows, helpStyle.Render("  (vacío)"))
	} else {
		start, end := windowBounds(m.pick.cursor, total, contentH)
		for i := start; i < end; i++ {
			rows = append(rows, m.renderPickRow(m.pick.items[i], i == m.pick.cursor, contentW))
		}
		hint = "· " + scrollHint(m.pick.cursor, total, start, end)
	}

	title := lipgloss.JoinHorizontal(lipgloss.Bottom,
		panelTitleStyle.Render(" "+m.pick.title+" "),
		panelSubtitleStyle.Render(hint))
	box := panelStyleActive.Width(width - 2).Height(height - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, title, box)
}

// renderPickRow renders one option on a single line: " ✓ label … hint" con el
// hint alineado a la derecha y el label truncado para que quepa (nunca wrap).
func (m *Model) renderPickRow(it pickItem, selected bool, w int) string {
	gl := " "
	switch it.mark {
	case 1:
		gl = "✓"
	case -1:
		gl = "✗"
	}
	label := it.label
	maxLabel := w - 3 - lipgloss.Width(it.hint) - 2
	if maxLabel < 6 {
		maxLabel = 6
	}
	if lipgloss.Width(label) > maxLabel {
		label = truncate(label, maxLabel)
	}
	gap := w - 3 - lipgloss.Width(label) - lipgloss.Width(it.hint)
	if gap < 1 {
		gap = 1
	}

	if selected {
		line := " " + gl + " " + label + strings.Repeat(" ", gap) + it.hint
		return rowSelectedStyle.Render(padLine(line, w))
	}

	glyph := gl
	switch it.mark {
	case 1:
		glyph = okStyle.Render("✓")
	case -1:
		glyph = errStyle.Render("✗")
	}
	return " " + glyph + " " + rowNormalStyle.Render(label) +
		strings.Repeat(" ", gap) + helpStyle.Render(it.hint)
}

func (m *Model) renderInputPanel() string {
	var title, hint string
	switch m.inputPurpose {
	case inpNewFolder:
		title, hint = "📁  Nueva carpeta", "Se crea dentro de "+m.cfg.BackupDir
	case inpNewDBName:
		title, hint = "🗄  Nombre de la base nueva", "Si ya existe, se reemplazará (DROP + CREATE)."
	case inpConfigField:
		name := "valor"
		if m.cfgCursor >= 0 && m.cfgCursor < len(m.cfgFields) {
			f := m.cfgFields[m.cfgCursor]
			name = f.section + " · " + f.label
		}
		title, hint = "✎  Editar "+name, "Enter guarda · Esc cancela"
	case inpDumpName:
		title, hint = "✎  Nombre del archivo", "Se guarda en la carpeta destino · Enter ok · Esc cancela"
	case inpFolderPrefix:
		title, hint = "🏷  Prefijo de la carpeta", "Se aplica a los dumps de esta carpeta · vacío = sin prefijo"
	}
	w := m.modalWidth()
	content := strings.Join([]string{
		"",
		modalTitleStyle.Render(title),
		"",
		" " + helpStyle.Render(truncate(hint, w-8)),
		"",
		" " + m.input.View(),
		"",
		" " + footerKeyStyle.Render("enter") + footerDescStyle.Render("  ok   ") +
			footerKeyStyle.Render("esc") + footerDescStyle.Render("  cancelar"),
		"",
	}, "\n")
	return modalBoxStyle.Width(w).Render(content)
}

func (m *Model) renderConfirm() string {
	w := m.modalWidth()
	valW := w - 17
	if valW < 12 {
		valW = 12
	}
	field := func(label, value string, leftTrunc bool) string {
		v := value
		if leftTrunc {
			v = truncateLeft(value, valW)
		} else {
			v = truncate(value, valW)
		}
		return "  " + modalLabelStyle.Render(padR(label, 8)) + " " + modalValueStyle.Render(v)
	}

	if m.scr == screenBackup {
		origin := fmt.Sprintf("%s@%s:%s · túnel %s",
			m.cfg.Prod.User, m.cfg.Prod.Host, m.cfg.Prod.Port, quoteOrDash(m.cfg.ProdSSH))
		folderLabel := filepathBase(m.bkFolder)
		if m.bkPrefix != "" {
			folderLabel += "  (prefijo: " + m.bkPrefix + ")"
		}
		rows := []string{
			modalTitleStyle.Render("Resumen del backup"),
			"",
			field("base", m.bkDB, false),
			field("origen", origin, false),
			field("carpeta", folderLabel, false),
			field("archivo", m.bkFile, true),
			"",
			modalHintStyle.Render(truncate("  e editar nombre · p prefijo de la carpeta", w-6)),
			modalHintStyle.Render(truncate("  ⓘ el rol debe poder leer toda la base (GRANT pg_read_all_data).", w-6)),
			"",
			confirmButtons("Sí, ejecutar el backup", false),
		}
		return modalBoxStyle.Width(w).Render(strings.Join(rows, "\n"))
	}

	replace := m.rsMode == "REPLACE"
	titleStyle, boxStyle := modalTitleStyle, modalBoxStyle
	modeStyled := okStyle.Render("crear nueva")
	if replace {
		titleStyle, boxStyle = modalTitleWarnStyle, modalWarnStyle
		modeStyled = modalDangerStyle.Render("reemplazar · DROP + CREATE (irreversible)")
	}
	dest := fmt.Sprintf("%s  ·  %s@%s:%s",
		m.rsTarget, m.cfg.Local.User, m.cfg.Local.Host, m.cfg.Local.Port)
	rows := []string{
		titleStyle.Render("Resumen del restore"),
		"",
		field("backup", filepathBase(m.rsDump), false),
		field("destino", dest, false),
		"  " + modalLabelStyle.Render(padR("modo", 8)) + " " + modeStyled,
		"",
		modalHintStyle.Render(truncate("  Restore con --single-transaction (todo o nada).", w-6)),
		"",
		confirmButtons("Sí, restaurar ahora", replace),
	}
	return boxStyle.Width(w).Render(strings.Join(rows, "\n"))
}

func confirmButtons(yesLabel string, danger bool) string {
	yesKey := modalButtonKeyStyle.Render(" enter ")
	if danger {
		yesKey = modalButtonDangerKeyStyle.Render(" enter ")
	}
	return "  " + lipgloss.JoinHorizontal(lipgloss.Center,
		yesKey, modalButtonDescStyle.Render("  "+yesLabel+"    "),
		modalButtonCancelKeyStyle.Render(" esc "), modalButtonDescStyle.Render("  cancelar"))
}

// ── config screen ────────────────────────────────────────────────────────────

func (m *Model) renderConfig() string {
	header := m.renderConfigHeader()
	footer := m.renderFooter(configKeys)
	sb := m.renderStatusBar()
	sbH := 0
	if sb != "" {
		sbH = lipgloss.Height(sb)
	}
	avail := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - sbH
	if avail < 6 {
		avail = 6
	}

	var body string
	if m.input.Focused() {
		body = lipgloss.Place(m.width, avail, lipgloss.Center, lipgloss.Center, m.renderInputPanel())
	} else {
		w := m.modalWidth()
		rows := []string{
			panelTitleStyle.Render(" Conexiones "),
			" " + panelSubtitleStyle.Render("de dónde sale el backup · a dónde se restaura · dónde se guarda"),
			"",
		}
		lastSection := ""
		for i, f := range m.cfgFields {
			if f.section != lastSection {
				if lastSection != "" {
					rows = append(rows, "")
				}
				lastSection = f.section
				rows = append(rows, m.cfgSectionHeader(f.section))
			}
			rows = append(rows, m.cfgFieldRow(i, f, w))
		}
		rows = append(rows, "", " "+helpStyle.Render("enter editar · l / p probar conexión · esc volver"))
		panel := modalBoxStyle.Width(w).Render(strings.Join(rows, "\n"))
		body = lipgloss.Place(m.width, avail, lipgloss.Center, lipgloss.Center, panel)
	}

	parts := []string{header, body}
	if sb != "" {
		parts = append(parts, sb)
	}
	parts = append(parts, footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderConfigHeader() string {
	left := headerStyle.Render("🐘 pgflow")
	mid := "  " + brandStyle.Render("Configuración de conexiones")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(mid)
	if gap < 0 {
		gap = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, strings.Repeat(" ", gap))
}

// cfgSectionHeader renders a group header with a role description and a live
// connection badge, so it's obvious what LOCAL / PRODUCCIÓN / BACKUPS are.
func (m *Model) cfgSectionHeader(section string) string {
	var title, desc, status string
	switch section {
	case "LOCAL":
		title, desc = "LOCAL", "tu PostgreSQL — destino del restore"
		status = m.localBadge()
	case "PROD":
		title, desc = "PRODUCCIÓN", "vía túnel SSH — origen del backup"
		status = m.prodBadge()
	case "BACKUPS":
		title, desc = "BACKUPS", "carpeta donde se guardan los .dump"
	}
	out := " " + sectionTitleStyle.Render(title)
	if status != "" {
		out += "  " + status
	}
	return out + "  " + panelSubtitleStyle.Render("· "+desc)
}

func (m *Model) localBadge() string {
	switch {
	case !m.statusKnown:
		return helpStyle.Render("comprobando…")
	case m.localOK:
		return okStyle.Render("✓ conecta")
	default:
		return errStyle.Render("✗ sin conexión")
	}
}

func (m *Model) prodBadge() string {
	switch {
	case !m.statusKnown:
		return helpStyle.Render("comprobando…")
	case m.prodOK:
		return okStyle.Render("✓ conecta")
	case m.tunnelOK:
		return warnStyle.Render("túnel ok · sin auth")
	default:
		return errStyle.Render("✗ túnel cerrado")
	}
}

func (m *Model) cfgFieldRow(i int, f cfgField, w int) string {
	raw := f.get(m.cfg)
	val := raw
	if f.secret && raw != "" {
		val = "••••"
	}
	label := padR(f.label, 14)
	if i == m.cfgCursor {
		shown := val
		if shown == "" {
			shown = "(vacío — enter para editar)"
		}
		return rowSelectedStyle.Render(padLine("   "+label+" "+shown, w-8))
	}
	styledVal := detailValueStyle.Render(val)
	if val == "" {
		styledVal = helpStyle.Render("(vacío)")
	}
	return "   " + detailDimStyle.Render(label) + " " + styledVal
}

// ── modals / running ─────────────────────────────────────────────────────────

func (m *Model) helpWidth() int {
	w := m.width - 6
	if w > 96 {
		w = 96
	}
	if w < 60 {
		w = 60
	}
	return w
}

// renderHelp explains what each action does and the prod → archivo → local flow,
// so the direction/purpose is unambiguous without reading docs.
func (m *Model) renderHelp() string {
	w := m.helpWidth()
	flow := brandStyle.Render("prod") + dateStyle.Render(" ──") + okStyle.Render("b") + dateStyle.Render("──▶ ") +
		statStyle.Render("archivo .dump") + dateStyle.Render(" ──") + okStyle.Render("r") + dateStyle.Render("──▶ ") +
		brandStyle.Render("base local nueva")
	row := func(k, d string) string {
		return "  " + footerKeyStyle.Render(padR(k, 8)) + footerDescStyle.Render(truncate(d, w-14))
	}
	rows := []string{
		modalTitleStyle.Render("pgflow · ¿qué hace cada cosa?"),
		"",
		"  " + flow,
		"",
		row("b", "trae una base de PRODUCCIÓN y la guarda como .dump (tu respaldo)"),
		row("r", "carga un .dump en una base LOCAL nueva (o reemplaza una)"),
		row("v", "verifica que un .dump no esté corrupto"),
		row("d", "borra un .dump del disco"),
		row("t", "abre / cierra el túnel SSH a producción"),
		row("c", "configura conexiones (local · prod · carpeta)"),
		row("j/k", "navegar · enter abrir/restaurar · ^r refrescar · q salir"),
		"",
		modalDividerStyle.Render(strings.Repeat("─", w-8)),
		"",
		"  " + modalHintStyle.Render(truncate("El rol de prod necesita lectura total: GRANT pg_read_all_data TO <rol>;", w-6)),
		"",
		"  " + modalButtonCancelKeyStyle.Render(" enter ") + modalButtonDescStyle.Render("  cerrar"),
	}
	return modalBoxStyle.Width(w).Render(strings.Join(rows, "\n"))
}

func (m *Model) renderModal() string {
	w := m.modalWidth()
	switch m.modal {
	case modalHelp:
		return m.renderHelp()
	case modalError:
		rows := []string{modalTitleErrorStyle.Render("✗  " + m.errTitle), ""}
		for i, ln := range strings.Split(m.errText, "\n") {
			switch {
			case strings.TrimSpace(ln) == "":
				rows = append(rows, "")
			case strings.Contains(ln, "GRANT") || strings.HasPrefix(ln, "  "):
				rows = append(rows, "  "+sizeStyle.Render(truncate(strings.TrimLeft(ln, " "), w-10)))
			case i == 0:
				rows = append(rows, "  "+modalValueStyle.Render(truncate(ln, w-10)))
			default:
				rows = append(rows, "  "+modalHintStyle.Render(truncate(ln, w-10)))
			}
		}
		rows = append(rows,
			"",
			modalDividerStyle.Render(strings.Repeat("─", w-8)),
			"",
			"  "+modalButtonCancelKeyStyle.Render(" enter ")+modalButtonDescStyle.Render("  cerrar"))
		return modalErrorStyle.Width(w).Render(strings.Join(rows, "\n"))
	case modalConfirmDelete:
		rows := []string{
			modalTitleWarnStyle.Render("🗑  Borrar backup"),
			"",
			"  " + modalLabelStyle.Render(padR("archivo", 8)) + " " + modalValueStyle.Render(truncate(m.delName, w-14)),
			"",
			modalHintStyle.Render("  Elimina el dump del disco. No se puede deshacer."),
			"",
			"  " + modalButtonDangerKeyStyle.Render(" y/enter ") + modalButtonDescStyle.Render("  sí, borrar    ") +
				modalButtonCancelKeyStyle.Render(" n/esc ") + modalButtonDescStyle.Render("  cancelar"),
		}
		return modalWarnStyle.Width(w).Render(strings.Join(rows, "\n"))
	}
	return ""
}

func (m *Model) renderRunning() string {
	content := strings.Join([]string{
		"",
		"  " + brandStyle.Render(spinnerFrame(m.spinner)) + "   " + modalValueStyle.Render(m.runLabel),
		"",
	}, "\n")
	return modalBoxStyle.Width(m.modalWidth()).Render(content)
}

// ── small render helpers ─────────────────────────────────────────────────────

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func padR(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// truncateLeft keeps the tail of a string (useful for paths), prefixing "…".
func truncateLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(n-1):])
}

// windowBounds returns the [start,end) slice of `total` items to render in `n`
// rows, keeping `cursor` visible (centered once the list scrolls).
func windowBounds(cursor, total, n int) (int, int) {
	if n <= 0 || total <= n {
		return 0, total
	}
	start := cursor - n/2
	if start < 0 {
		start = 0
	}
	if start > total-n {
		start = total - n
	}
	return start, start + n
}

// scrollHint shows "pos/total" with ↑/↓ when there are items off-screen.
func scrollHint(cursor, total, start, end int) string {
	if total == 0 {
		return ""
	}
	s := fmt.Sprintf("%d/%d", cursor+1, total)
	if start > 0 {
		s = "↑ " + s
	}
	if end < total {
		s += " ↓"
	}
	return s
}

func padLine(s string, target int) string {
	w := lipgloss.Width(s)
	if w >= target {
		return s
	}
	return s + strings.Repeat(" ", target-w)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func filepathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func quoteOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return "'" + s + "'"
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame(i int) string {
	if i < 0 {
		i = 0
	}
	return spinnerFrames[i%len(spinnerFrames)]
}
