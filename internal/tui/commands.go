package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ander0code/pgflow/internal/backups"
	"github.com/ander0code/pgflow/internal/config"
	"github.com/ander0code/pgflow/internal/naming"
	"github.com/ander0code/pgflow/internal/pg"
	"github.com/ander0code/pgflow/internal/tunnel"
)

// scanCmd rescans the backup directory.
func scanCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		folders, err := backups.Scan(cfg.BackupDir)
		return scanDoneMsg{folders: folders, err: err}
	}
}

// statusCmd probes local + tunnel + prod health in the background.
func statusCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		localOK := false
		if _, err := pg.TestConn(cfg.Local); err == nil {
			localOK = true
		}
		tunnelOK := tunnel.IsUp(cfg.Prod.Host, cfg.Prod.Port)
		prodOK := false
		if tunnelOK {
			if _, err := pg.TestConn(cfg.Prod); err == nil {
				prodOK = true
			}
		}
		return statusDoneMsg{localOK: localOK, tunnelOK: tunnelOK, prodOK: prodOK}
	}
}

// prepBackupCmd ensures the tunnel + prod connection, then lists prod DBs.
func prepBackupCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		if _, err := tunnel.Ensure(cfg); err != nil {
			return prodDBsMsg{err: err}
		}
		if _, err := pg.TestConn(cfg.Prod); err != nil {
			return prodDBsMsg{err: err}
		}
		dbs, err := pg.ListDatabases(cfg.Prod)
		return prodDBsMsg{dbs: dbs, err: err}
	}
}

// localDBsCmd lists local databases (restore "replace existing" step).
func localDBsCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		dbs, err := pg.ListDatabases(cfg.Local)
		return localDBsMsg{dbs: dbs, err: err}
	}
}

// streamDumpCmd starts pg_dump in a goroutine and returns a channel of log
// events (progress lines + a final result).
func streamDumpCmd(cfg *config.Config, db, folder, filename string) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan logEvent, 256)
		go func() {
			defer close(ch)
			out := filepath.Join(folder, filename)
			ts := time.Now().Format("20060102_150405")
			// `db` and `target` come from the prod server (resp. user input);
			// sanitize them before they land in a path component so a hostile
			// server name or a local ".." cannot redirect the .err log
			// outside of LogDir.
			errlog := filepath.Join(cfg.LogDir(), "pgdump_"+naming.Sanitize(db)+"_"+ts+".err")
			ch <- logEvent{line: "▶ pg_dump " + db + " → " + filename}
			res, err := pg.DumpStream(cfg.Prod, db, out, errlog, func(l string) { ch <- logEvent{line: l} })
			ch <- logEvent{done: true, kind: "dump", dumpRes: res, err: err}
		}()
		return streamStartedMsg{ch: ch}
	}
}

// streamRestoreCmd verifies + (re)creates the target + runs pg_restore, all
// while streaming progress lines.
func streamRestoreCmd(cfg *config.Config, dump, mode, target string) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan logEvent, 256)
		go func() {
			defer close(ch)
			ch <- logEvent{line: "▶ verificando dump…"}
			if vr := pg.Verify(dump); !vr.Valid {
				ch <- logEvent{done: true, kind: "restore", target: target, err: fmt.Errorf("el dump no es válido: %s", vr.Err)}
				return
			}
			if mode == "REPLACE" {
				ch <- logEvent{line: "▶ recreando '" + target + "' (DROP + CREATE)…"}
				if err := pg.RecreateDatabase(cfg.Local, target); err != nil {
					ch <- logEvent{done: true, kind: "restore", target: target, err: err}
					return
				}
			} else {
				ch <- logEvent{line: "▶ creando '" + target + "'…"}
				if err := pg.CreateDatabase(cfg.Local, target); err != nil {
					ch <- logEvent{done: true, kind: "restore", target: target, err: err}
					return
				}
			}
			ts := time.Now().Format("20060102_150405")
			errlog := filepath.Join(cfg.LogDir(), "pgrestore_"+naming.Sanitize(target)+"_"+ts+".err")
			ch <- logEvent{line: "▶ pg_restore → " + target}
			res, err := pg.RestoreStream(cfg.Local, dump, target, errlog, func(l string) { ch <- logEvent{line: l} })
			ch <- logEvent{done: true, kind: "restore", restoreRes: res, target: target, err: err}
		}()
		return streamStartedMsg{ch: ch}
	}
}

// waitForLog blocks until the next log event is available, then delivers it.
// The model re-issues this command after each non-terminal event.
func waitForLog(ch chan logEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return logEventMsg{done: true}
		}
		return logEventMsg(ev)
	}
}

// verifyCmd re-checks a single dump's integrity.
func verifyCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return verifyDoneMsg{path: path, res: pg.Verify(path)}
	}
}

// deleteCmd removes a dump file from disk.
func deleteCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return deleteDoneMsg{name: filepath.Base(path), err: os.Remove(path)}
	}
}

// tunnelToggleCmd opens the tunnel if down, or closes it if up. If the
// port is listening but the tunnel was not opened by pgflow (IsOpen ==
// false), we report that explicitly so the user does not assume the
// external tunnel has been torn down.
func tunnelToggleCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		up := tunnel.IsUp(cfg.Prod.Host, cfg.Prod.Port)
		if up && !tunnel.IsOpen() {
			return tunnelDoneMsg{up: true, external: true, err: fmt.Errorf("el puerto %s ya está abierto por otro proceso — pgflow no lo cierra; ciérralo manualmente", cfg.Prod.Port)}
		}
		if up {
			err := tunnel.Close(cfg.ProdSSH)
			return tunnelDoneMsg{up: tunnel.IsUp(cfg.Prod.Host, cfg.Prod.Port), err: err}
		}
		_, err := tunnel.Ensure(cfg)
		return tunnelDoneMsg{up: tunnel.IsUp(cfg.Prod.Host, cfg.Prod.Port), err: err}
	}
}

// testConnCmd tests a single connection (config screen).
func testConnCmd(label string, c config.Conn) tea.Cmd {
	return func() tea.Msg {
		v, err := pg.TestConn(c)
		return connTestMsg{label: label, version: v, err: err}
	}
}

// timers
func tickCmd() tea.Cmd {
	return tea.Tick(20*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

func splashTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return splashTickMsg(t) })
}

func statusClearCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg { return statusClearMsg{} })
}
