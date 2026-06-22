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
	_ = json.Unmarshal(data, &s)
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

func save(s store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), data, 0o644)
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

// Sanitize keeps only filename-safe characters (letters, digits, - _ .); spaces
// become hyphens.
func Sanitize(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}
