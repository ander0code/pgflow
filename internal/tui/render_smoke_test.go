package tui

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ander0code/pgflow/internal/backups"
)

// newSized returns a ready-to-render model: sized, past the splash.
func newSized() *Model {
	m := New()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m.splashUntil = time.Now().Add(-time.Second)
	return m
}

func sampleFolders() []backups.Folder {
	now := time.Now()
	return []backups.Folder{
		{Name: "tienda-web", Path: "/b/tienda-web", Dumps: []backups.Dump{
			{Name: "shop_20260620_143200.dump", Path: "/b/tienda-web/shop_20260620_143200.dump", SizeBytes: 19293000, ModTime: now, Valid: true, Objects: 142},
			{Name: "broken.dump", Path: "/b/tienda-web/broken.dump", SizeBytes: 0, ModTime: now, Valid: false},
		}},
		{Name: "blog", Path: "/b/blog"},
	}
}

// manyDumpFolder simula una carpeta con muchos dumps de nombre largo (el caso
// que se veía mal: wrap + overflow). Nombres genéricos, no de clientes reales.
func manyDumpFolder() []backups.Folder {
	now := time.Now()
	var dumps []backups.Dump
	for i := 0; i < 20; i++ {
		dumps = append(dumps, backups.Dump{
			Name:      fmt.Sprintf("shop_PRODUCCION-2026_06_%02d_12_06_15-tienda.dump", i+1),
			Path:      fmt.Sprintf("/b/tienda-web/d%02d.dump", i),
			SizeBytes: int64(1_700_000 - i*60_000),
			ModTime:   now.Add(-time.Duration(i) * time.Hour),
			Valid:     i != 4,
		})
	}
	return []backups.Folder{{Name: "tienda-web", Path: "/b/tienda-web", Dumps: dumps}}
}

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func mustRender(t *testing.T, m *Model, label string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked in %q: %v", label, r)
		}
	}()
	if out := m.View(); out == "" {
		t.Fatalf("empty View in %q", label)
	}
}

// TestViewAcrossStates drives the model through every screen and renders each
// one, asserting View never panics or returns empty. Catches the nil-deref /
// slice-bounds bugs the compiler can't.
func TestViewAcrossStates(t *testing.T) {
	m := newSized()
	m.folders = sampleFolders()
	m.rebuildVisible()
	m.statusKnown, m.localOK, m.tunnelOK = true, true, true
	mustRender(t, m, "dashboard (folder selected)")

	m.Update(key("j")) // onto a dump
	mustRender(t, m, "dashboard (dump selected)")

	// backup wizard
	m.Update(prodDBsMsg{dbs: []string{"db1", "db2"}})
	mustRender(t, m, "backup-step1")
	m.backupSelect()
	mustRender(t, m, "backup-step2")
	m.bkFolder = "/b/tienda-web"
	m.proposeDumpName()
	m.step = 3
	mustRender(t, m, "backup-confirm")
	m.startInput(inpDumpName, "nombre", m.bkFile)
	mustRender(t, m, "backup-edit-name")

	// restore wizard
	r := newSized()
	r.folders = sampleFolders()
	r.rebuildVisible()
	r.startRestore()
	mustRender(t, r, "restore-step1")
	r.restoreSelect() // folder → dump
	mustRender(t, r, "restore-step2")
	r.restoreSelect() // dump → target
	mustRender(t, r, "restore-step3")
	r.rsMode, r.rsTarget = "REPLACE", "shop_local"
	r.rsDump, r.step = "/b/tienda-web/shop_20260620_143200.dump", 4
	mustRender(t, r, "restore-confirm-replace")

	// new-db name input
	r2 := newSized()
	r2.folders, r2.scr, r2.step = sampleFolders(), screenRestore, 3
	r2.startInput(inpNewDBName, "nombre", "shop_local")
	mustRender(t, r2, "restore-input")

	// config + modals + running + live logs
	c := newSized()
	c.openConfig()
	mustRender(t, c, "config")
	c.running, c.runLabel = true, "trabajando…"
	mustRender(t, c, "running (spinner)")
	c.streaming, c.runStart = true, time.Now()
	c.logLines = []string{"pg_dump: dumping contents of table users", "pg_dump: dumping contents of table orders"}
	mustRender(t, c, "run-screen (live logs)")
	c.streaming, c.running = false, false
	c.modal, c.errTitle, c.errText = modalError, "Error", "algo falló"
	mustRender(t, c, "modal-error")
	c.modal, c.delName = modalConfirmDelete, "x.dump"
	mustRender(t, c, "modal-delete")
	c.modal = modalHelp
	mustRender(t, c, "modal-help")

	// empty dashboard (overview state) + splash
	e := newSized()
	e.statusKnown, e.localOK = true, true
	mustRender(t, e, "empty-dashboard (overview)")
	e.splashUntil = time.Now().Add(time.Second)
	mustRender(t, e, "splash")
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

// TestSnapshot renders key screens with ANSI stripped and logs them. Run with
// `go test ./internal/tui/ -run TestSnapshot -v` to eyeball the real layout.
func TestSnapshot(t *testing.T) {
	snap := func(label string, m *Model) {
		t.Logf("\n──────── %s ────────\n%s\n", label, strip(m.View()))
	}

	m := newSized()
	m.folders = sampleFolders()
	m.rebuildVisible()
	m.statusKnown, m.localOK, m.tunnelOK, m.prodOK = true, true, true, true
	m.lastScan = time.Now()
	snap("DASHBOARD · folder selected", m)

	m.Update(key("j"))
	snap("DASHBOARD · dump selected", m)

	e := newSized()
	e.statusKnown, e.localOK = true, true
	e.lastScan = time.Now()
	snap("DASHBOARD · empty (overview)", e)

	b := newSized()
	b.scr, b.step = screenBackup, 3
	b.bkDB = "shopdb"
	b.bkFolder = "/srv/backups/tienda-web"
	b.bkPrefix = "TIENDA_PROD"
	b.bkFile = "TIENDA_PROD-shopdb-20260622_104207.dump"
	b.cfg.Prod.User, b.cfg.Prod.Host, b.cfg.Prod.Port, b.cfg.ProdSSH = "produser", "localhost", "5433", "mi-servidor"
	snap("BACKUP · confirm", b)

	r := newSized()
	r.folders = sampleFolders()
	r.rebuildVisible()
	r.scr, r.step = screenRestore, 4
	r.rsMode, r.rsTarget = "REPLACE", "shop_local"
	r.rsDump = "/b/tienda-web/shop_20260620_143200.dump"
	snap("RESTORE · confirm (replace)", r)

	cfg := newSized()
	cfg.openConfig()
	cfg.statusKnown, cfg.localOK, cfg.tunnelOK, cfg.prodOK = true, true, true, true
	snap("CONFIG · conexiones", cfg)

	er := newSized()
	er.modal = modalError
	er.errTitle = "El backup falló"
	er.errText = "el rol «produser» no puede leer la tabla «auth_group».\n\n" +
		"pg_dump necesita leer TODA la base. En el servidor, como admin:\n" +
		"  GRANT pg_read_all_data TO produser;   — PostgreSQL 14+\n" +
		"o usa un rol superusuario para el backup"
	snap("ERROR · permisos (accionable)", er)

	hp := newSized()
	hp.modal = modalHelp
	snap("HELP · ? ayuda", hp)

	pk := newSized()
	pk.folders = manyDumpFolder()
	pk.scr, pk.step = screenRestore, 2
	pk.pick = pk.dumpPicker("/b/tienda-web")
	pk.pick.cursor = 8
	snap("PICKER · 20 dumps (scroll, full-width, sin wrap)", pk)
}

func TestSuggestLocalName(t *testing.T) {
	if got := suggestLocalName("/b/tienda-web/shop_20260620_143200.dump"); got != "shop_local" {
		t.Fatalf("suggestLocalName = %q, want shop_local", got)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0B", 512: "512B", 1024: "1.0KB", 1048576: "1.0MB"}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
