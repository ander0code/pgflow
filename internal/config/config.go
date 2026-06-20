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
	"strconv"
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
	// the tunnel to production. ProdRemotePort is postgres' port on the server.
	ProdSSH        string
	ProdRemotePort string

	BackupDir string
	MinDiskMB int
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
		Local:          Conn{Host: "localhost", Port: "5432", User: "postgres"},
		Prod:           Conn{Host: "localhost", Port: "5433", User: "postgres"},
		ProdRemotePort: "5432",
		BackupDir:      filepath.Join(home, "pgflow-backups"),
		MinDiskMB:      200,
	}
}

// Load reads ~/.pgflow.conf layered over the defaults. A missing file just
// yields the defaults.
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
	set("PGFLOW_PROD_REMOTE_PORT", &c.ProdRemotePort)
	set("PGFLOW_PROD_USER", &c.Prod.User)
	set("PGFLOW_PROD_PASS", &c.Prod.Pass)
	set("PGFLOW_BACKUP_DIR", &c.BackupDir)
	if v, ok := vals["PGFLOW_MIN_DISK_MB"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			c.MinDiskMB = n
		}
	}

	c.BackupDir = expandHome(c.BackupDir)
	return c
}

// Save writes the config back to ~/.pgflow.conf (mode 0600), in the same
// shell format the bash version used.
func (c *Config) Save() error {
	content := fmt.Sprintf(`# pgflow — conexiones (gestionado por pgflow)
PGFLOW_LOCAL_HOST="%s"
PGFLOW_LOCAL_PORT="%s"
PGFLOW_LOCAL_USER="%s"
PGFLOW_LOCAL_PASS="%s"
PGFLOW_PROD_SSH="%s"
PGFLOW_PROD_HOST="%s"
PGFLOW_PROD_PORT="%s"
PGFLOW_PROD_REMOTE_PORT="%s"
PGFLOW_PROD_USER="%s"
PGFLOW_PROD_PASS="%s"
PGFLOW_BACKUP_DIR="%s"
`,
		c.Local.Host, c.Local.Port, c.Local.User, c.Local.Pass,
		c.ProdSSH, c.Prod.Host, c.Prod.Port, c.ProdRemotePort,
		c.Prod.User, c.Prod.Pass, c.BackupDir)
	return os.WriteFile(Path(), []byte(content), 0o600)
}

// stripValue removes surrounding quotes and any trailing inline # comment.
func stripValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 1 && (s[0] == '"' || s[0] == '\'') {
		q := s[0]
		if end := strings.IndexByte(s[1:], q); end >= 0 {
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
