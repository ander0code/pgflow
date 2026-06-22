package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ander0code/pgflow/internal/backups"
	"github.com/ander0code/pgflow/internal/config"
)

type screen int

const (
	screenDashboard screen = iota
	screenBackup
	screenRestore
	screenConfig
)

type modal int

const (
	modalNone modal = iota
	modalConfirmDelete
	modalError
	modalHelp
)

type inputPurpose int

const (
	inpNone inputPurpose = iota
	inpNewFolder
	inpNewDBName
	inpConfigField
)

var tsSuffix = regexp.MustCompile(`_\d{8}_\d{6}$`)

// dashItem is one row in the dashboard list: a folder header or a dump.
type dashItem struct {
	isHeader bool
	folder   string
	dump     backups.Dump
}

// pickItem is one option in a wizard picker.
type pickItem struct {
	label string
	value string
	mark  int // 0 none, 1 ✓ valid, -1 ✗ corrupt
	hint  string
}

type picker struct {
	title  string
	items  []pickItem
	cursor int
}

func (p *picker) move(d int) {
	n := len(p.items)
	if n == 0 {
		return
	}
	p.cursor = (p.cursor + d + n) % n
}

func (p *picker) current() pickItem {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return pickItem{}
	}
	return p.items[p.cursor]
}

// cfgField is one editable configuration value, grouped under a section.
type cfgField struct {
	section string
	label   string
	get     func(*config.Config) string
	set     func(*config.Config, string)
	secret  bool
}

type Model struct {
	cfg    *config.Config
	width  int
	height int

	scr   screen
	modal modal

	// dashboard
	folders    []backups.Folder
	visible    []dashItem
	cursor     int
	collapsed  map[string]bool
	lastScan   time.Time
	selectPath string // dump a seleccionar tras el próximo scan (p.ej. el backup recién creado)

	// health
	localOK     bool
	tunnelOK    bool
	prodOK      bool
	statusKnown bool

	// transient status line
	status    string
	statusErr bool

	// running op
	running   bool
	streaming bool
	runLabel  string
	runStart  time.Time
	spinner   int

	// live log (streaming dump/restore)
	logLines []string
	logCh    chan logEvent

	// splash
	splashUntil time.Time

	// shared text input
	input        textinput.Model
	inputPurpose inputPurpose

	// wizard picker
	pick picker
	step int

	// cached db lists (for wizard back-navigation + existence checks)
	prodDBs          []string
	localDBs         []string
	awaitLocalPicker bool

	// backup flow
	bkDB     string
	bkFolder string

	// restore flow
	rsFolder      string
	rsDump        string
	rsMode        string // CREATE | REPLACE
	rsTarget      string
	suggestedName string

	// modal payloads
	errTitle string
	errText  string
	delPath  string
	delName  string

	// config screen
	cfgCursor int
	cfgFields []cfgField
}

func New() *Model {
	ti := textinput.New()
	ti.CharLimit = 128
	ti.Prompt = "❯ "

	return &Model{
		cfg:         config.Load(),
		scr:         screenDashboard,
		modal:       modalNone,
		collapsed:   map[string]bool{},
		input:       ti,
		splashUntil: time.Now().Add(1400 * time.Millisecond),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(scanCmd(m.cfg), statusCmd(m.cfg), splashTickCmd(), tickCmd())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case splashTickMsg:
		if time.Now().Before(m.splashUntil) {
			return m, splashTickCmd()
		}
		return m, nil

	case spinnerTickMsg:
		if m.running {
			m.spinner++
			return m, spinnerTickCmd()
		}
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}
		if m.scr == screenDashboard && m.modal == modalNone && !m.running {
			cmds = append(cmds, statusCmd(m.cfg))
		}
		return m, tea.Batch(cmds...)

	case statusClearMsg:
		m.status = ""
		return m, nil

	case scanDoneMsg:
		if msg.err != nil {
			m.setStatus("error al escanear: "+msg.err.Error(), true)
			return m, statusClearCmd()
		}
		m.folders = msg.folders
		m.lastScan = time.Now()
		m.rebuildVisible()
		if m.selectPath != "" {
			m.selectDumpByPath(m.selectPath)
			m.selectPath = ""
		}
		return m, nil

	case statusDoneMsg:
		m.localOK, m.tunnelOK, m.prodOK = msg.localOK, msg.tunnelOK, msg.prodOK
		m.statusKnown = true
		return m, nil

	case prodDBsMsg:
		m.running = false
		if msg.err != nil {
			return m, m.fail("No se pudo preparar el backup", msg.err)
		}
		if len(msg.dbs) == 0 {
			return m, m.fail("Sin bases", fmt.Errorf("no se encontraron bases en producción"))
		}
		m.prodDBs = msg.dbs
		m.pick = dbsPicker("base a respaldar", msg.dbs)
		m.scr = screenBackup
		m.step = 1
		return m, nil

	case localDBsMsg:
		m.localDBs = msg.dbs
		if m.awaitLocalPicker {
			m.awaitLocalPicker = false
			m.running = false
			if msg.err != nil {
				return m, m.fail("No se pudieron listar las bases locales", msg.err)
			}
			if len(msg.dbs) == 0 {
				m.setStatus("no hay bases locales; usa 'crear nueva'", true)
				return m, statusClearCmd()
			}
			m.pick = dbsPicker("base local a reemplazar", msg.dbs)
			m.step = 31
		}
		return m, nil

	case streamStartedMsg:
		m.logCh = msg.ch
		return m, waitForLog(msg.ch)

	case logEventMsg:
		if !msg.done {
			if msg.line != "" {
				m.logLines = append(m.logLines, msg.line)
				if len(m.logLines) > 500 {
					m.logLines = m.logLines[len(m.logLines)-500:]
				}
			}
			if m.logCh != nil {
				return m, waitForLog(m.logCh)
			}
			return m, nil
		}
		// terminal event: operation finished
		m.running = false
		m.streaming = false
		m.logCh = nil
		m.scr = screenDashboard
		m.step = 0
		switch msg.kind {
		case "dump":
			if msg.err != nil {
				return m, m.fail("El backup falló", msg.err)
			}
			extra := ""
			if msg.dumpRes.Warnings > 0 {
				extra = fmt.Sprintf(" · %d aviso(s)", msg.dumpRes.Warnings)
			}
			m.selectPath = msg.dumpRes.File // quedará seleccionado tras el scan
			m.setStatus(fmt.Sprintf("✓ backup listo en %s — %s%s",
				msg.dumpRes.Elapsed.Round(time.Second), filepath.Base(msg.dumpRes.File), extra), false)
		case "restore":
			if msg.err != nil {
				return m, m.fail("El restore falló", msg.err)
			}
			if msg.restoreRes.Status == "warnings" {
				m.setStatus(fmt.Sprintf("⚠ restore con %d aviso(s) — %d tablas en '%s'",
					msg.restoreRes.Warnings, msg.restoreRes.Tables, msg.target), true)
			} else {
				m.setStatus(fmt.Sprintf("✓ restore correcto (%s) — %d tablas en '%s'",
					msg.restoreRes.Elapsed.Round(time.Second), msg.restoreRes.Tables, msg.target), false)
			}
		}
		return m, tea.Batch(scanCmd(m.cfg), statusClearCmd())

	case verifyDoneMsg:
		if msg.res.Valid {
			m.setStatus(fmt.Sprintf("✓ %s válido — %s · %d objetos",
				filepath.Base(msg.path), humanSize(msg.res.SizeBytes), msg.res.Objects), false)
		} else {
			m.setStatus(fmt.Sprintf("✗ %s — %s", filepath.Base(msg.path), msg.res.Err), true)
		}
		return m, statusClearCmd()

	case deleteDoneMsg:
		if msg.err != nil {
			return m, m.fail("No se pudo borrar", msg.err)
		}
		m.setStatus("🗑 borrado "+msg.name, false)
		return m, tea.Batch(scanCmd(m.cfg), statusClearCmd())

	case tunnelDoneMsg:
		m.running = false
		if msg.err != nil {
			return m, m.fail("Túnel", msg.err)
		}
		m.tunnelOK = msg.up
		if msg.up {
			m.setStatus("✓ túnel abierto", false)
		} else {
			m.setStatus("túnel cerrado", false)
		}
		return m, tea.Batch(statusCmd(m.cfg), statusClearCmd())

	case connTestMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("✗ %s: %s", msg.label, msg.err.Error()), true)
		} else {
			m.setStatus(fmt.Sprintf("✓ %s — PostgreSQL %s", msg.label, msg.version), false)
		}
		return m, statusClearCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// ── key dispatch ─────────────────────────────────────────────────────────────

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.running {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.modal != modalNone {
		return m.handleModalKey(msg)
	}
	switch m.scr {
	case screenBackup:
		return m.handleBackupKey(msg)
	case screenRestore:
		return m.handleRestoreKey(msg)
	case screenConfig:
		return m.handleConfigKey(msg)
	default:
		return m.handleDashboardKey(msg)
	}
}

func (m *Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalError:
		switch msg.String() {
		case "enter", "esc", "q":
			m.modal = modalNone
		}
	case modalHelp:
		switch msg.String() {
		case "enter", "esc", "q", "?":
			m.modal = modalNone
		}
	case modalConfirmDelete:
		switch msg.String() {
		case "y", "Y", "enter":
			m.modal = modalNone
			return m, deleteCmd(m.delPath)
		case "n", "N", "esc", "q":
			m.modal = modalNone
		}
	}
	return m, nil
}

func (m *Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "g":
		m.cursor = 0
		m.skipHeaderForward()
	case "G":
		if len(m.visible) > 0 {
			m.cursor = len(m.visible) - 1
		}
	case "enter":
		if it := m.curItem(); it != nil {
			if it.isHeader {
				m.collapsed[it.folder] = !m.collapsed[it.folder]
				m.rebuildVisible()
			} else {
				return m.quickRestore()
			}
		}
	case " ":
		if it := m.curItem(); it != nil && it.isHeader {
			m.collapsed[it.folder] = !m.collapsed[it.folder]
			m.rebuildVisible()
		}
	case "b":
		return m.startRun("conectando a producción…", prepBackupCmd(m.cfg))
	case "r":
		if m.curDump() != nil {
			return m.quickRestore()
		}
		return m.startRestore()
	case "v":
		if d := m.curDump(); d != nil {
			return m, verifyCmd(d.Path)
		}
	case "d":
		if d := m.curDump(); d != nil {
			m.delPath, m.delName = d.Path, d.Name
			m.modal = modalConfirmDelete
		}
	case "t":
		label := "abriendo túnel…"
		if m.tunnelOK {
			label = "cerrando túnel…"
		}
		return m.startRun(label, tunnelToggleCmd(m.cfg))
	case "c":
		m.openConfig()
		return m, statusCmd(m.cfg)
	case "?":
		m.modal = modalHelp
	case "ctrl+r", "R":
		return m, tea.Batch(scanCmd(m.cfg), statusCmd(m.cfg))
	}
	return m, nil
}

// ── backup wizard ────────────────────────────────────────────────────────────

func (m *Model) handleBackupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		return m.handleInputKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.cancelFlow()
	case "up", "k":
		m.pick.move(-1)
	case "down", "j":
		m.pick.move(1)
	case "left", "h":
		return m.backupBack()
	case "enter":
		return m.backupSelect()
	}
	return m, nil
}

func (m *Model) backupSelect() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1:
		m.bkDB = m.pick.current().value
		m.step = 2
		m.pick = m.folderPicker()
	case 2:
		it := m.pick.current()
		if it.value == "__new__" {
			m.startInput(inpNewFolder, "nombre de la carpeta", "")
			return m, nil
		}
		m.bkFolder = it.value
		m.step = 3
	case 3:
		return m.startStream("Backup · "+m.bkDB, streamDumpCmd(m.cfg, m.bkDB, m.bkFolder))
	}
	return m, nil
}

func (m *Model) backupBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case 2:
		m.step = 1
		m.pick = dbsPicker("base a respaldar", m.prodDBs)
	case 3:
		m.step = 2
		m.pick = m.folderPicker()
	}
	return m, nil
}

func (m *Model) folderPicker() picker {
	var items []pickItem
	for _, f := range m.folders {
		items = append(items, pickItem{
			label: f.Name, value: f.Path,
			hint: fmt.Sprintf("%d backup(s)", len(f.Dumps)),
		})
	}
	items = append(items, pickItem{label: "➕ Crear carpeta nueva", value: "__new__"})
	return picker{title: "carpeta destino", items: items}
}

// ── restore wizard ───────────────────────────────────────────────────────────

func (m *Model) startRestore() (tea.Model, tea.Cmd) {
	p := m.restoreFolderPicker()
	if len(p.items) == 0 {
		return m, m.fail("Sin backups", fmt.Errorf("no hay carpetas con dumps en %s", m.cfg.BackupDir))
	}
	m.pick = p
	m.scr = screenRestore
	m.step = 1
	return m, localDBsCmd(m.cfg) // populate in the background for existence checks
}

func (m *Model) quickRestore() (tea.Model, tea.Cmd) {
	d := m.curDump()
	if d == nil {
		return m, nil
	}
	m.rsFolder = m.curItem().folder
	m.rsDump = d.Path
	m.scr = screenRestore
	m.step = 3
	m.pick = targetPicker()
	return m, localDBsCmd(m.cfg)
}

func (m *Model) handleRestoreKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		return m.handleInputKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.cancelFlow()
	case "up", "k":
		m.pick.move(-1)
	case "down", "j":
		m.pick.move(1)
	case "left", "h":
		return m.restoreBack()
	case "enter":
		return m.restoreSelect()
	}
	return m, nil
}

func (m *Model) restoreSelect() (tea.Model, tea.Cmd) {
	switch m.step {
	case 1:
		m.rsFolder = m.pick.current().value
		m.step = 2
		m.pick = m.dumpPicker(m.rsFolder)
	case 2:
		m.rsDump = m.pick.current().value
		m.step = 3
		m.pick = targetPicker()
	case 3:
		switch m.pick.current().value {
		case "create":
			m.suggestedName = suggestLocalName(m.rsDump)
			m.startInput(inpNewDBName, "nombre de la base nueva", m.suggestedName)
		case "replace":
			m.awaitLocalPicker = true
			return m.startRun("listando bases locales…", localDBsCmd(m.cfg))
		}
	case 31:
		m.rsTarget = m.pick.current().value
		m.rsMode = "REPLACE"
		m.step = 4
	case 4:
		return m.startStream("Restore · "+m.rsTarget, streamRestoreCmd(m.cfg, m.rsDump, m.rsMode, m.rsTarget))
	}
	return m, nil
}

func (m *Model) restoreBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case 2:
		m.step = 1
		m.pick = m.restoreFolderPicker()
	case 3:
		m.step = 2
		m.pick = m.dumpPicker(m.rsFolder)
	case 31:
		m.step = 3
		m.pick = targetPicker()
	case 4:
		m.step = 3
		m.pick = targetPicker()
	}
	return m, nil
}

func (m *Model) restoreFolderPicker() picker {
	var items []pickItem
	for _, f := range m.folders {
		if len(f.Dumps) == 0 {
			continue
		}
		items = append(items, pickItem{
			label: f.Name, value: f.Path,
			hint: fmt.Sprintf("%d backup(s) · últ %s", len(f.Dumps), f.Dumps[0].ModTime.Format("2006-01-02")),
		})
	}
	return picker{title: "carpeta de backups", items: items}
}

func (m *Model) dumpPicker(folderPath string) picker {
	var items []pickItem
	for _, f := range m.folders {
		if f.Path != folderPath {
			continue
		}
		for _, d := range f.Dumps {
			mark := 1
			if !d.Valid {
				mark = -1
			}
			items = append(items, pickItem{
				label: d.Name, value: d.Path, mark: mark,
				hint: fmt.Sprintf("%s · %s", humanSize(d.SizeBytes), d.ModTime.Format("2006-01-02 15:04")),
			})
		}
	}
	return picker{title: "backup (↑ más reciente)", items: items}
}

func targetPicker() picker {
	return picker{title: "destino del restore", items: []pickItem{
		{label: "Crear una base de datos nueva", value: "create"},
		{label: "Reemplazar una base existente", value: "replace"},
	}}
}

func dbsPicker(title string, dbs []string) picker {
	items := make([]pickItem, len(dbs))
	for i, d := range dbs {
		items[i] = pickItem{label: d, value: d}
	}
	return picker{title: title, items: items}
}

// ── shared text input ────────────────────────────────────────────────────────

func (m *Model) startInput(p inputPurpose, placeholder, val string) {
	m.inputPurpose = p
	m.input.SetValue(val)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	m.input.Focus()
}

func (m *Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.inputPurpose = inpNone
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		m.input.Blur()
		p := m.inputPurpose
		m.inputPurpose = inpNone
		return m.submitInput(p, val)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) submitInput(p inputPurpose, val string) (tea.Model, tea.Cmd) {
	switch p {
	case inpNewFolder:
		if val == "" {
			return m, nil
		}
		path := filepath.Join(m.cfg.BackupDir, sanitizeFolder(val))
		if err := os.MkdirAll(path, 0o755); err != nil {
			return m, m.fail("No se pudo crear la carpeta", err)
		}
		m.bkFolder = path
		m.step = 3
	case inpNewDBName:
		if val == "" {
			val = m.suggestedName
		}
		m.rsTarget = val
		if m.localDBContains(val) {
			m.rsMode = "REPLACE"
		} else {
			m.rsMode = "CREATE"
		}
		m.step = 4
	case inpConfigField:
		if m.cfgCursor >= 0 && m.cfgCursor < len(m.cfgFields) {
			m.cfgFields[m.cfgCursor].set(m.cfg, val)
			if err := m.cfg.Save(); err != nil {
				return m, m.fail("No se pudo guardar", err)
			}
			m.setStatus("✓ configuración guardada", false)
			return m, statusClearCmd()
		}
	}
	return m, nil
}

// ── config screen ────────────────────────────────────────────────────────────

func (m *Model) openConfig() {
	m.scr = screenConfig
	m.cfgCursor = 0
	m.buildCfgFields()
}

func (m *Model) buildCfgFields() {
	m.cfgFields = []cfgField{
		{"LOCAL", "host", func(c *config.Config) string { return c.Local.Host }, func(c *config.Config, v string) { c.Local.Host = v }, false},
		{"LOCAL", "puerto", func(c *config.Config) string { return c.Local.Port }, func(c *config.Config, v string) { c.Local.Port = v }, false},
		{"LOCAL", "usuario", func(c *config.Config) string { return c.Local.User }, func(c *config.Config, v string) { c.Local.User = v }, false},
		{"LOCAL", "password", func(c *config.Config) string { return c.Local.Pass }, func(c *config.Config, v string) { c.Local.Pass = v }, true},
		{"PROD", "alias SSH", func(c *config.Config) string { return c.ProdSSH }, func(c *config.Config, v string) { c.ProdSSH = v }, false},
		{"PROD", "puerto túnel", func(c *config.Config) string { return c.Prod.Port }, func(c *config.Config, v string) { c.Prod.Port = v }, false},
		{"PROD", "puerto remoto", func(c *config.Config) string { return c.ProdRemotePort }, func(c *config.Config, v string) { c.ProdRemotePort = v }, false},
		{"PROD", "usuario", func(c *config.Config) string { return c.Prod.User }, func(c *config.Config, v string) { c.Prod.User = v }, false},
		{"PROD", "password", func(c *config.Config) string { return c.Prod.Pass }, func(c *config.Config, v string) { c.Prod.Pass = v }, true},
		{"BACKUPS", "carpeta", func(c *config.Config) string { return c.BackupDir }, func(c *config.Config, v string) { c.BackupDir = v }, false},
	}
}

func (m *Model) handleConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		return m.handleInputKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.scr = screenDashboard
		return m, tea.Batch(statusCmd(m.cfg), scanCmd(m.cfg))
	case "up", "k":
		if m.cfgCursor > 0 {
			m.cfgCursor--
		}
	case "down", "j":
		if m.cfgCursor < len(m.cfgFields)-1 {
			m.cfgCursor++
		}
	case "enter":
		f := m.cfgFields[m.cfgCursor]
		m.startInput(inpConfigField, f.label, f.get(m.cfg))
	case "l":
		return m, testConnCmd("Local", m.cfg.Local)
	case "p":
		return m, testConnCmd("Producción", m.cfg.Prod)
	}
	return m, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (m *Model) startRun(label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.running = true
	m.streaming = false
	m.runLabel = label
	m.runStart = time.Now()
	m.spinner = 0
	return m, tea.Batch(cmd, spinnerTickCmd())
}

func (m *Model) startStream(label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.running = true
	m.streaming = true
	m.runLabel = label
	m.runStart = time.Now()
	m.spinner = 0
	m.logLines = m.logLines[:0]
	return m, tea.Batch(cmd, spinnerTickCmd())
}

func (m *Model) fail(title string, err error) tea.Cmd {
	m.errTitle = title
	m.errText = err.Error()
	m.modal = modalError
	return nil
}

func (m *Model) cancelFlow() (tea.Model, tea.Cmd) {
	m.scr = screenDashboard
	m.step = 0
	m.input.Blur()
	m.inputPurpose = inpNone
	m.setStatus("cancelado", false)
	return m, statusClearCmd()
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m *Model) moveCursor(d int) {
	n := len(m.visible)
	if n == 0 {
		return
	}
	m.cursor = (m.cursor + d + n) % n
}

func (m *Model) skipHeaderForward() {
	for m.cursor < len(m.visible)-1 && m.visible[m.cursor].isHeader {
		m.cursor++
	}
}

func (m *Model) curItem() *dashItem {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.visible[m.cursor]
}

func (m *Model) curDump() *backups.Dump {
	it := m.curItem()
	if it == nil || it.isHeader {
		return nil
	}
	return &it.dump
}

func (m *Model) folderCount(name string) int {
	for _, f := range m.folders {
		if f.Name == name {
			return len(f.Dumps)
		}
	}
	return 0
}

func (m *Model) localDBContains(name string) bool {
	for _, d := range m.localDBs {
		if d == name {
			return true
		}
	}
	return false
}

// selectDumpByPath mueve el cursor al dump con esa ruta (expandiendo su carpeta
// si está contraída). Se usa para resaltar el backup recién creado.
func (m *Model) selectDumpByPath(path string) {
	for _, f := range m.folders {
		for _, d := range f.Dumps {
			if d.Path == path {
				delete(m.collapsed, f.Name) // asegurar expandida
				m.rebuildVisible()
				for i, it := range m.visible {
					if !it.isHeader && it.dump.Path == path {
						m.cursor = i
						return
					}
				}
				return
			}
		}
	}
}

func (m *Model) rebuildVisible() {
	m.visible = m.visible[:0]
	for _, f := range m.folders {
		m.visible = append(m.visible, dashItem{isHeader: true, folder: f.Name})
		if m.collapsed[f.Name] {
			continue
		}
		for _, d := range f.Dumps {
			m.visible = append(m.visible, dashItem{folder: f.Name, dump: d})
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func sanitizeFolder(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "-")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == '.',
			r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func suggestLocalName(dumpPath string) string {
	base := strings.TrimSuffix(filepath.Base(dumpPath), ".dump")
	base = tsSuffix.ReplaceAllString(base, "")
	return base + "_local"
}
