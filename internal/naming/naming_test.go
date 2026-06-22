package naming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRender_Tokens(t *testing.T) {
	now := time.Date(2026, 6, 21, 18, 30, 45, 0, time.UTC)
	got := Render("{prefix}-{db}-{datetime}", "shopdb", "TIENDA", now, 7)
	want := "TIENDA-shopdb-20260621_183045.dump"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRender_AllTokens(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := Render("{db}-{date}-{time}-{datetime}-{seq}-{prefix}", "d", "P", now, 42)
	want := "d-20260102-030405-20260102_030405-042-P.dump"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRender_EmptyTemplateFallsBack(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := Render("", "shopdb", "", now, 0)
	if !strings.HasPrefix(got, "shopdb") {
		t.Fatalf("Render empty tmpl should use db name, got %q", got)
	}
	if !strings.HasSuffix(got, ".dump") {
		t.Fatalf("Render always appends .dump, got %q", got)
	}
}

func TestRender_AlwaysAddsDotDump(t *testing.T) {
	now := time.Now()
	got := Render("{db}", "shopdb", "", now, 0)
	if !strings.HasSuffix(got, ".dump") {
		t.Fatalf("expected .dump suffix, got %q", got)
	}
}

func TestDefaultTemplate(t *testing.T) {
	if got := DefaultTemplate("TIENDA"); got != "{prefix}-{db}-{datetime}" {
		t.Fatalf("DefaultTemplate with prefix = %q", got)
	}
	if got := DefaultTemplate(""); got != "{db}_{datetime}" {
		t.Fatalf("DefaultTemplate without prefix = %q", got)
	}
}

func TestSanitize_StripsUnsafe(t *testing.T) {
	cases := map[string]string{
		"hello world":    "hello-world",
		"a/b\\c":         "abc",
		"a..b":           "ab", // dot is rejected
		"  trim me  ":    "trim-me",
		"hello\x00world": "helloworld",
		"ABC123_ok-":     "ABC123_ok-", // only ASCII letters / digits / - / _
		"drop-the-bom!":  "drop-the-bom",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitize_WindowsReserved(t *testing.T) {
	for _, in := range []string{"CON", "PRN", "AUX", "NUL", "COM1", "LPT9", "con", "nul"} {
		got := Sanitize(in)
		if got == in {
			t.Errorf("Sanitize(%q) = %q, should be suffixed with _", in, got)
		}
		if !strings.HasSuffix(got, "_") {
			t.Errorf("Sanitize(%q) = %q, expected _ suffix", in, got)
		}
	}
}

func TestRecommend(t *testing.T) {
	if got := Recommend("tienda-web"); got != "TIENDA-WEB" {
		t.Fatalf("Recommend = %q", got)
	}
}

// TestPrefixRoundtrip verifies the on-disk roundtrip: set, load, set again.
func TestPrefixRoundtrip(t *testing.T) {
	// Redirect HOME so we don't touch the real ~/.pgflow.json.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // for Windows

	if err := SetPrefix("tienda", "TIENDA"); err != nil {
		t.Fatalf("SetPrefix: %v", err)
	}
	if got := Prefix("tienda"); got != "TIENDA" {
		t.Fatalf("Prefix = %q, want TIENDA", got)
	}
	// Set empty clears the key.
	if err := SetPrefix("tienda", ""); err != nil {
		t.Fatalf("SetPrefix clear: %v", err)
	}
	if got := Prefix("tienda"); got != "" {
		t.Fatalf("Prefix after clear = %q, want \"\"", got)
	}
}

func TestSeqRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if got := Seq("shopdb"); got != 0 {
		t.Fatalf("Seq initial = %d, want 0", got)
	}
	for i := 1; i <= 3; i++ {
		if err := BumpSeq("shopdb"); err != nil {
			t.Fatalf("BumpSeq %d: %v", i, err)
		}
		if got := Seq("shopdb"); got != i {
			t.Fatalf("Seq after %d bumps = %d, want %d", i, got, i)
		}
	}
}

func TestTemplateRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if got := Template("shopdb"); got != "" {
		t.Fatalf("Template initial = %q", got)
	}
	if err := SetTemplate("shopdb", "{db}-{seq}"); err != nil {
		t.Fatalf("SetTemplate: %v", err)
	}
	if got := Template("shopdb"); got != "{db}-{seq}" {
		t.Fatalf("Template after set = %q", got)
	}
	if err := SetTemplate("shopdb", ""); err != nil {
		t.Fatalf("SetTemplate clear: %v", err)
	}
	if got := Template("shopdb"); got != "" {
		t.Fatalf("Template after clear = %q", got)
	}
}

func TestLoad_CorruptJSON_RecoversDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	// Write a deliberately corrupt JSON.
	bad := filepath.Join(dir, ".pgflow.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Reset our package mutex (load is mutex-guarded).
	mu.Lock()
	_ = load() // this should rename the file and reset
	mu.Unlock()

	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("corrupt file should have been renamed away: stat err = %v", err)
	}

	// A subsequent save should succeed.
	if err := BumpSeq("shopdb"); err != nil {
		t.Fatalf("BumpSeq after corrupt-recover: %v", err)
	}
	if got := Seq("shopdb"); got != 1 {
		t.Fatalf("Seq after recovery = %d, want 1", got)
	}
}
