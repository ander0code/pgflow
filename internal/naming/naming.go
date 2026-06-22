// Package naming builds and remembers dump file names. Each backup folder can
// have a prefix (stored in ~/.pgflow.json) so its dumps are tagged and easy to
// identify; the file name encodes prefix, database and timestamp.
package naming

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type store struct {
	Prefixes map[string]string `json:"prefixes"`
}

var mu sync.Mutex

func path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pgflow.json")
}

func load() store {
	s := store{Prefixes: map[string]string{}}
	data, err := os.ReadFile(path())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	if s.Prefixes == nil {
		s.Prefixes = map[string]string{}
	}
	return s
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
	prefix = Sanitize(prefix)
	if prefix == "" {
		delete(s.Prefixes, folder)
	} else {
		s.Prefixes[folder] = prefix
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), data, 0o644)
}

// DumpFileName builds the dump file name. With a prefix it is
// "<prefix>-<db>-<ts>.dump"; without, "<db>_<ts>.dump" (the original scheme).
func DumpFileName(prefix, db, ts string) string {
	if prefix == "" {
		return db + "_" + ts + ".dump"
	}
	return prefix + "-" + db + "-" + ts + ".dump"
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
