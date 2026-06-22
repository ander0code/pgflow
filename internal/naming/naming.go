// Package naming builds and remembers dump file names. State lives in
// ~/.pgflow.json: a per-folder prefix, a per-database naming template, and a
// per-database sequence counter. The template (tokens {db} {date} {time}
// {datetime} {seq} {prefix}) lets each database standardize how its dumps are
// named, with an optional auto-incrementing {seq}.
package naming

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type store struct {
	Prefixes  map[string]string `json:"prefixes"`
	Templates map[string]string `json:"templates"`
	Seq       map[string]int    `json:"seq"`
}

var mu sync.Mutex

func path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pgflow.json")
}

func load() store {
	s := store{
		Prefixes:  map[string]string{},
		Templates: map[string]string{},
		Seq:       map[string]int{},
	}
	data, err := os.ReadFile(path())
	if err != nil {
		return s
	}
	if uerr := json.Unmarshal(data, &s); uerr != nil {
		// The file is corrupt (truncated by a crash mid-write, edited by
		// hand, etc.). Move it aside so the next save() does not silently
		// overwrite a recoverable snapshot, and start from defaults.
		_ = os.Rename(path(), path()+".corrupt."+time.Now().Format("20060102_150405"))
		fmt.Fprintln(os.Stderr, "pgflow: ~/.pgflow.json estaba corrupto, lo moví a un .corrupt.* y arranco con defaults:", uerr)
		s = store{
			Prefixes:  map[string]string{},
			Templates: map[string]string{},
			Seq:       map[string]int{},
		}
	}
	if s.Prefixes == nil {
		s.Prefixes = map[string]string{}
	}
	if s.Templates == nil {
		s.Templates = map[string]string{}
	}
	if s.Seq == nil {
		s.Seq = map[string]int{}
	}
	return s
}

// save writes ~/.pgflow.json atomically (write to .tmp + rename) with mode
// 0600. Atomicity matters because a crash mid-write would otherwise corrupt
// the user's entire naming state; the mode matters because the file
// contains the list of databases the user backs up — information that is
// useful for an attacker scoping a follow-on.
func save(s store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(path(), 0o600)
	return nil
}

// Prefix returns the saved prefix for a folder (empty if none).
func Prefix(folder string) string {
	mu.Lock()
	defer mu.Unlock()
	return load().Prefixes[folder]
}

// SetPrefix saves (or clears, when empty) the prefix for a folder.
func SetPrefix(folder, prefix string) error {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if prefix = Sanitize(prefix); prefix == "" {
		delete(s.Prefixes, folder)
	} else {
		s.Prefixes[folder] = prefix
	}
	return save(s)
}

// Template returns the saved naming template for a database (empty if none).
func Template(db string) string {
	mu.Lock()
	defer mu.Unlock()
	return load().Templates[db]
}

// SetTemplate saves (or clears, when empty) the naming template for a database.
func SetTemplate(db, tmpl string) error {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	if tmpl = strings.TrimSpace(tmpl); tmpl == "" {
		delete(s.Templates, db)
	} else {
		s.Templates[db] = tmpl
	}
	return save(s)
}

// Seq returns the last sequence number used for a database (0 if none).
func Seq(db string) int {
	mu.Lock()
	defer mu.Unlock()
	return load().Seq[db]
}

// BumpSeq advances the sequence counter for a database (call after a backup).
func BumpSeq(db string) error {
	mu.Lock()
	defer mu.Unlock()
	s := load()
	s.Seq[db]++
	return save(s)
}

// DefaultTemplate is used when a database has no saved template; it includes the
// folder prefix when there is one.
func DefaultTemplate(prefix string) string {
	if prefix != "" {
		return "{prefix}-{db}-{datetime}"
	}
	return "{db}_{datetime}"
}

// Tokens lists the placeholders a template understands (for the editor hint).
const Tokens = "{db} {date} {time} {datetime} {seq} {prefix}"

// Render expands a template into a dump file name. The result is sanitized and
// always ends in ".dump".
func Render(tmpl, db, prefix string, t time.Time, seq int) string {
	r := strings.NewReplacer(
		"{db}", db,
		"{date}", t.Format("20060102"),
		"{time}", t.Format("150405"),
		"{datetime}", t.Format("20060102_150405"),
		"{seq}", fmt.Sprintf("%03d", seq),
		"{prefix}", prefix,
	)
	name := strings.Trim(Sanitize(r.Replace(tmpl)), "-_")
	if name == "" {
		name = db
	}
	if !strings.HasSuffix(name, ".dump") {
		name += ".dump"
	}
	return name
}

// Recommend suggests a prefix derived from a folder name (UPPER, safe chars).
func Recommend(folder string) string {
	return Sanitize(strings.ToUpper(folder))
}

// Sanitize keeps only filename-safe characters (letters, digits, - _); spaces
// become hyphens. Dot is rejected (no "..") and Windows-reserved names
// (CON, PRN, AUX, NUL, COM1..9, LPT1..9) are suffixed with "_" so they
// remain usable but never collide with device names that the OS would
// otherwise interpret.
func Sanitize(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if isWindowsReserved(out) {
		return out + "_"
	}
	return out
}

// isWindowsReserved reports whether base is a Windows device name
// (case-insensitive). The trailing extension, if any, is ignored; we only
// check the stem, which is enough to disambiguate "CON.dump" from
// "console" in pgflow's use.
func isWindowsReserved(base string) bool {
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}
