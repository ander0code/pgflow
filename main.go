// Command pgflow is a terminal UI for backing up and restoring PostgreSQL
// databases — a Bubble Tea front-end for the pg_dump / pg_restore workflow.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ander0code/pgflow/internal/backups"
	"github.com/ander0code/pgflow/internal/config"
	"github.com/ander0code/pgflow/internal/tui"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	listOnly, jsonOut := false, false

	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v":
			fmt.Printf("pgflow %s\n", version)
			return
		case "--help", "-h":
			printHelp()
			return
		case "--list":
			listOnly = true
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintf(os.Stderr, "pgflow: flag desconocida %q\n", a)
			fmt.Fprintln(os.Stderr, "usa `pgflow --help`")
			os.Exit(2)
		}
	}

	if listOnly {
		runList(jsonOut)
		return
	}

	p := tea.NewProgram(tui.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pgflow: %v\n", err)
		os.Exit(1)
	}
}

func runList(jsonOut bool) {
	cfg := config.Load()
	folders, err := backups.Scan(cfg.BackupDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan error:", err)
		os.Exit(1)
	}

	if jsonOut {
		type dumpOut struct {
			Folder  string `json:"folder"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Size    int64  `json:"size_bytes"`
			Objects int    `json:"objects"`
			Valid   bool   `json:"valid"`
			Created string `json:"created"`
		}
		out := make([]dumpOut, 0)
		for _, f := range folders {
			for _, d := range f.Dumps {
				out = append(out, dumpOut{
					Folder: f.Name, Name: d.Name, Path: d.Path,
					Size: d.SizeBytes, Objects: d.Objects, Valid: d.Valid,
					Created: d.ModTime.Format("2006-01-02 15:04:05"),
				})
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	fmt.Printf("%d backup(s) en %s\n", backups.Total(folders), cfg.BackupDir)
	for _, f := range folders {
		fmt.Printf("\n%s (%d)\n", f.Name, len(f.Dumps))
		for _, d := range f.Dumps {
			mark := "✓"
			if !d.Valid {
				mark = "✗"
			}
			fmt.Printf("  %s  %-44s  %8s  %s\n",
				mark, d.Name, human(d.SizeBytes), d.ModTime.Format("2006-01-02 15:04"))
		}
	}
}

func human(b int64) string {
	const u = 1024
	if b < u {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func printHelp() {
	fmt.Println(`pgflow — backup & restore de PostgreSQL (TUI)

Uso:
  pgflow                 abre la interfaz (TUI)
  pgflow --list          lista los backups (texto)
  pgflow --list --json   lista los backups (JSON)
  pgflow --version       versión
  pgflow --help          esta ayuda

Dentro de la TUI:
  ↑/↓ j/k   navegar
  b         backup de producción
  r         restaurar a local
  v         verificar un dump
  d         borrar un dump
  t         abrir / cerrar el túnel SSH
  c         configurar conexiones
  ^r        refrescar
  q         salir

Config: ~/.pgflow.conf`)
}
