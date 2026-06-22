package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Local.Host == "" || c.Local.Port == "" || c.Local.User == "" {
		t.Fatalf("Default missing local fields: %+v", c.Local)
	}
	if c.ProdSSH != "" {
		t.Fatalf("ProdSSH should default to empty, got %q", c.ProdSSH)
	}
	if c.BackupDir == "" {
		t.Fatalf("BackupDir should default")
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	c := Load()
	if c.Local.Host == "" {
		t.Fatal("expected defaults")
	}
}

func TestLoad_ParsesShellFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	content := `
# comment
PGFLOW_LOCAL_HOST="myhost"
PGFLOW_LOCAL_PORT="5433"
PGFLOW_LOCAL_USER="alice"
PGFLOW_LOCAL_PASS="hunter2"
PGFLOW_PROD_SSH="prodalias"
PGFLOW_PROD_HOST="localhost"
PGFLOW_PROD_PORT="5433"
PGFLOW_PROD_USER="bob"
PGFLOW_PROD_PASS=""
PGFLOW_BACKUP_DIR="~/my-backups"
`
	if err := os.WriteFile(filepath.Join(dir, ".pgflow.conf"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	c := Load()
	if c.Local.Host != "myhost" {
		t.Errorf("Local.Host = %q", c.Local.Host)
	}
	if c.Local.Pass != "hunter2" {
		t.Errorf("Local.Pass = %q", c.Local.Pass)
	}
	if c.BackupDir != filepath.Join(dir, "my-backups") {
		t.Errorf("BackupDir = %q, expected ~/my-backups expanded", c.BackupDir)
	}
}

// TestSave_EscapesQuotes verifies that a password containing " is safely
// round-tripped and cannot break out of the quoted shell format.
func TestSave_EscapesQuotes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	c := Default()
	c.Local.Pass = `evil"value`

	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2 := Load()
	if c2.Local.Pass != `evil"value` {
		t.Fatalf("password not preserved across roundtrip: got %q", c2.Local.Pass)
	}
}

func TestSave_RejectsNewlinesInValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	c := Default()
	c.Local.Pass = "line1\nline2"

	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Newlines collapse to "" so the file structure stays valid.
	c2 := Load()
	if strings.Contains(c2.Local.Pass, "\n") {
		t.Fatalf("password should not contain newlines after roundtrip: %q", c2.Local.Pass)
	}
}

func TestSave_AppliesMode0600(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	c := Default()
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, ".pgflow.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("config file mode = %o, want 0o600 (no world-readable)", perm)
	}
}

func TestSave_ReappliesMode_OnPreExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	// Pre-create with looser permissions (legacy install).
	path := filepath.Join(dir, ".pgflow.conf")
	if err := os.WriteFile(path, []byte(`PGFLOW_LOCAL_HOST="x"`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Default().Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, _ := os.Stat(path)
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("mode after Save = %o, want 0o600", perm)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"~":          home,
		"~/x":        filepath.Join(home, "x"),
		"$HOME/x":    filepath.Join(home, "x"),
		"${HOME}/x":  filepath.Join(home, "x"),
		"/abs/path":  "/abs/path",
		"relative/x": "relative/x",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripValue(t *testing.T) {
	cases := map[string]string{
		`"hello"`:     "hello",
		`"he\"llo"`:   `he"llo`,
		`"he\\llo"`:   `he\llo`,
		`'single'`:    "single",
		`plain`:       "plain",
		`"with # OK"`: "with # OK", // # inside double quotes is literal
		`bare # tail`: "bare",
		`""`:          "",
	}
	for in, want := range cases {
		if got := stripValue(in); got != want {
			t.Errorf("stripValue(%q) = %q, want %q", in, got, want)
		}
	}
}
