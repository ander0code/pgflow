// Package config loads and persists pgflow connection settings.
//
// The on-disk format is the same shell KEY="value" file the original bash
// version used (~/.pgflow.conf), so existing setups keep working.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Conn is a PostgreSQL connection target.
type Conn struct {
	Host string
	Port string
	User string
	Pass string
}

// Config is the full pgflow configuration.
type Config struct {
	Local Conn
	Prod  Conn

	// ProdSSH is the ~/.ssh/config alias (with a LocalForward) used to open
	// the tunnel to production.
	ProdSSH string

	BackupDir string
}

// Path returns the config file location (~/.pgflow.conf).
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pgflow.conf")
}

// LogDir is where per-operation logs are written.
func (c *Config) LogDir() string {
	return filepath.Join(c.BackupDir, "logs")
}

// Default returns the built-in defaults (matching the bash script).
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Local:     Conn{Host: "localhost", Port: "5432", User: "postgres"},
		Prod:      Conn{Host: "localhost", Port: "5433", User: "postgres"},
		BackupDir: filepath.Join(home, "pgflow-backups"),
	}
}

// Load reads ~/.pgflow.conf layered over the defaults. A missing file just
// yields the defaults.
//
// Legacy fields that are no longer used (PGFLOW_PROD_REMOTE_PORT,
// PGFLOW_MIN_DISK_MB) are still recognized as keys so a stale config from
// an older version does not break; they are simply ignored.
func Load() *Config {
	c := Default()

	f, err := os.Open(Path())
	if err != nil {
		return c
	}
	defer f.Close()

	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		vals[key] = stripValue(line[eq+1:])
	}

	set := func(key string, dst *string) {
		if v, ok := vals[key]; ok {
			*dst = v
		}
	}
	set("PGFLOW_LOCAL_HOST", &c.Local.Host)
	set("PGFLOW_LOCAL_PORT", &c.Local.Port)
	set("PGFLOW_LOCAL_USER", &c.Local.User)
	set("PGFLOW_LOCAL_PASS", &c.Local.Pass)
	set("PGFLOW_PROD_SSH", &c.ProdSSH)
	set("PGFLOW_PROD_HOST", &c.Prod.Host)
	set("PGFLOW_PROD_PORT", &c.Prod.Port)
	set("PGFLOW_PROD_USER", &c.Prod.User)
	set("PGFLOW_PROD_PASS", &c.Prod.Pass)
	set("PGFLOW_BACKUP_DIR", &c.BackupDir)

	c.BackupDir = expandHome(c.BackupDir)
	return c
}

// Save writes the config back to ~/.pgflow.conf (mode 0600), in the same
// shell format the bash version used. Values are escaped to be safe in a
// double-quoted shell context: a backslash or double-quote in a password
// (or any other field) is preserved through a Save/Load round-trip and
// cannot be used to break out of the quote or to forge a new line.
//
// Empty values still get a blank pair of quotes so the file is uniform.
func (c *Config) Save() error {
	esc := func(v string) string {
		// Escape backslash first, then double-quote. Newlines and null bytes
		// are rejected outright (they would break the line-based format and
		// the user almost certainly didn't mean to put them in a config
		// value).
		if strings.ContainsAny(v, "\n\r\x00") {
			return "" // caller will fall through to the empty-value case
		}
		s := strings.ReplaceAll(v, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	content := fmt.Sprintf(`# pgflow — conexiones (gestionado por pgflow)
PGFLOW_LOCAL_HOST="%s"
PGFLOW_LOCAL_PORT="%s"
PGFLOW_LOCAL_USER="%s"
PGFLOW_LOCAL_PASS="%s"
PGFLOW_PROD_SSH="%s"
PGFLOW_PROD_HOST="%s"
PGFLOW_PROD_PORT="%s"
PGFLOW_PROD_USER="%s"
PGFLOW_PROD_PASS="%s"
PGFLOW_BACKUP_DIR="%s"
`,
		esc(c.Local.Host), esc(c.Local.Port), esc(c.Local.User), esc(c.Local.Pass),
		esc(c.ProdSSH), esc(c.Prod.Host), esc(c.Prod.Port),
		esc(c.Prod.User), esc(c.Prod.Pass), esc(c.BackupDir))
	if err := os.WriteFile(Path(), []byte(content), 0o600); err != nil {
		return err
	}
	// Defensive: re-apply the mode in case the file pre-existed with looser
	// permissions (legacy install from the bash version, manual chmod, etc.).
	// Best effort — if the file no longer exists this just errors and we move on.
	_ = os.Chmod(Path(), 0o600)
	return nil
}

// stripValue removes surrounding quotes and any trailing inline # comment.
// Backslash escapes (\" \\) inside a double-quoted value are reversed so a
// Save/Load round-trip is faithful even when the value contains characters
// that would otherwise break the shell format.
func stripValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		// Find the matching closing quote, skipping over escaped quotes (\").
		raw := s[1:]
		var b strings.Builder
		for i := 0; i < len(raw); i++ {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				next := raw[i+1]
				if next == '"' || next == '\\' {
					b.WriteByte(next)
					i++
					continue
				}
			}
			if c == '"' {
				return b.String()
			}
			b.WriteByte(c)
		}
		return b.String()
	}
	if len(s) >= 2 && s[0] == '\'' {
		// Single-quoted values: no escapes (matches shell semantics).
		if end := strings.IndexByte(s[1:], '\''); end >= 0 {
			return s[1 : 1+end]
		}
	}
	if h := strings.IndexByte(s, '#'); h >= 0 {
		s = strings.TrimSpace(s[:h])
	}
	return s
}

// expandHome expands ${HOME}, $HOME and a leading ~ in a path.
func expandHome(p string) string {
	home, _ := os.UserHomeDir()
	p = strings.ReplaceAll(p, "${HOME}", home)
	p = strings.ReplaceAll(p, "$HOME", home)
	switch {
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	}
	return p
}
