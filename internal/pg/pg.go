// Package pg wraps the PostgreSQL CLI tools (psql, pg_dump, pg_restore) with
// the same behaviour the original bash pgflow had: custom-format dumps,
// single-transaction restores, integrity checks, and human-friendly error
// translation. Dumps and restores stream their --verbose output line by line.
package pg

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ander0code/pgflow/internal/config"
)

// run executes a command capturing stdout/stderr. A nil environ inherits the
// parent process environment.
func run(name string, environ []string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command(name, args...)
	cmd.Env = environ
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// psqlRun validates the connection fields and the database name against a
// strict allowlist before invoking psql. Validation failures return
// (stderr, err) with err non-nil, so the caller can treat them the same as
// any other psql failure (which is also what they are from the user's
// perspective).
func psqlRun(c config.Conn, db string, extra ...string) (stdout, stderr string, err error) {
	args, aerr := psqlArgs(c, db, extra...)
	if aerr != nil {
		return "", aerr.Error(), aerr
	}
	return run("psql", env(c.Pass), args...)
}

// runStreaming runs a command, forwarding each stderr line to onLine as it
// arrives (for live logs) while also buffering the full output into buf.
func runStreaming(cmd *exec.Cmd, buf *bytes.Buffer, onLine func(string)) error {
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}
	return cmd.Wait()
}

// env returns the environment for a libpq tool, setting PGPASSWORD only when a
// password is configured. Empty password → fall back to ~/.pgpass.
func env(pass string) []string {
	e := os.Environ()
	if pass != "" {
		e = append(e, "PGPASSWORD="+pass)
	}
	return e
}

// psqlArgs builds the argv for a libpq CLI invocation. Each field is
// validated against a strict allowlist so a misconfigured ~/.pgflow.conf
// (or a shared dotfiles repo) cannot smuggle extra options to psql / pg_dump
// / pg_restore (e.g. c.Host = "-cDROP DATABASE postgres").
func psqlArgs(c config.Conn, db string, extra ...string) ([]string, error) {
	if !ValidHost(c.Host) {
		return nil, fmt.Errorf("host inválido: %q", c.Host)
	}
	if !ValidPort(c.Port) {
		return nil, fmt.Errorf("puerto inválido: %q", c.Port)
	}
	if !ValidIdent(c.User) {
		return nil, fmt.Errorf("usuario inválido: %q", c.User)
	}
	if !ValidIdent(db) {
		return nil, fmt.Errorf("base de datos inválida: %q", db)
	}
	args := []string{"-w", "-h", c.Host, "-p", c.Port, "-U", c.User, "-d", db}
	return append(args, extra...), nil
}

// ValidHost accepts hostnames, IPv4 and bracketed IPv6 literals. It is
// exported so the TUI config editor can validate user input before it
// reaches a libpq argv slot.
func ValidHost(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, r := range s {
		if !(r == '.' || r == '-' || r == ':' || r == '[' || r == ']' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// ValidPort accepts a numeric port string. Exported for the TUI.
func ValidPort(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ValidIdent allows the characters that are safe in a PostgreSQL identifier
// (or a libpq argument value) — letters, digits, _, -, ., @. It is exposed
// because the TUI uses it to reject user input before it ever reaches a
// psql / pg_dump / pg_restore argv slot.
func ValidIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r == '-' || r == '.' || r == '@' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// TestConn checks connectivity and returns the server version on success.
func TestConn(c config.Conn) (version string, err error) {
	_, se, e := psqlRun(c, "postgres", "-c", "SELECT 1;")
	if e != nil {
		return "", classifyConnError(se, e)
	}
	v, _, _ := psqlRun(c, "postgres", "-t", "-A", "-c", "SHOW server_version;")
	return strings.TrimSpace(v), nil
}

// ListDatabases returns the non-template, user databases sorted by name.
func ListDatabases(c config.Conn) ([]string, error) {
	const q = `SELECT datname FROM pg_database WHERE datistemplate=false AND datname NOT IN ('postgres','rdsadmin') ORDER BY datname;`
	out, se, err := psqlRun(c, "postgres", "-t", "-A", "-c", q)
	if err != nil {
		return nil, classifyConnError(se, err)
	}
	var dbs []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dbs = append(dbs, line)
		}
	}
	return dbs, nil
}

// CreateDatabase creates a new database.
func CreateDatabase(c config.Conn, name string) error {
	if !ValidIdent(name) {
		return fmt.Errorf("nombre de base inválido: %q", name)
	}
	q := fmt.Sprintf(`CREATE DATABASE "%s";`, escapeIdent(name))
	_, se, err := psqlRun(c, "postgres", "-c", q)
	if err != nil {
		if strings.Contains(strings.ToLower(se), "permission denied") {
			return fmt.Errorf("sin permiso para crear la base (el usuario necesita rol CREATEDB)")
		}
		return errors.New(orErr(se, err))
	}
	return nil
}

// RecreateDatabase drops and recreates a database, first terminating any active
// connections to it. This is the destructive REPLACE path.
func RecreateDatabase(c config.Conn, name string) error {
	if !ValidIdent(name) {
		return fmt.Errorf("nombre de base inválido: %q", name)
	}
	ident := escapeIdent(name)
	lit := escapeLiteral(name)

	// Best effort: terminating existing connections is a hint, not a guarantee.
	// A failure here is reported alongside the DROP result so the user
	// understands the cascade ("no pude cerrar conexiones" → "DROP falla").
	termOut, termSE, termErr := psqlRun(c, "postgres", "-c",
		fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='%s' AND pid <> pg_backend_pid();", lit))

	_, se, err := psqlRun(c, "postgres", "-c", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, ident))
	if err != nil {
		low := strings.ToLower(se)
		if strings.Contains(low, "being accessed") || strings.Contains(low, "other users") {
			extra := ""
			if termErr != nil {
				extra = fmt.Sprintf(" (y pg_terminate_backend falló: %s)", firstLine(termSE))
			}
			return fmt.Errorf("hay conexiones activas a '%s'; ciérralas y reintenta%s", name, extra)
		}
		return fmt.Errorf("no se pudo eliminar '%s': %s", name, firstLine(se))
	}

	_, se, err = psqlRun(c, "postgres", "-c", fmt.Sprintf(`CREATE DATABASE "%s";`, ident))
	if err != nil {
		return fmt.Errorf("no se pudo crear '%s': %s", name, firstLine(se))
	}
	// termOut kept to silence the unused-var check; the result is informational
	// and we don't want to bother the user on success.
	_ = termOut
	return nil
}

// DumpResult holds the outcome of a successful pg_dump.
type DumpResult struct {
	File     string
	Elapsed  time.Duration
	Warnings int
}

// DumpStream exports a database in custom format (-F c, --verbose), forwarding
// each progress line to onLine. On failure the partial file is removed.
func DumpStream(c config.Conn, db, outFile, errLog string, onLine func(string)) (DumpResult, error) {
	if !ValidHost(c.Host) || !ValidPort(c.Port) || !ValidIdent(c.User) || !ValidIdent(db) {
		return DumpResult{}, fmt.Errorf("campos de conexión o nombre de base inválidos")
	}
	start := time.Now()
	cmd := exec.Command("pg_dump", "-h", c.Host, "-p", c.Port, "-U", c.User,
		"-F", "c", "--verbose", "--no-password", "-f", outFile, db)
	cmd.Env = env(c.Pass)

	var buf bytes.Buffer
	err := runStreaming(cmd, &buf, onLine)
	elapsed := time.Since(start)
	if werr := writeLog(errLog, buf.Bytes()); werr != nil && err == nil {
		// The dump itself succeeded but persisting its log failed — surface
		// this so the user is not left wondering why the .err file is missing.
		err = fmt.Errorf("dump OK pero no pude escribir el log: %w", werr)
	}

	if err != nil {
		if rmErr := os.Remove(outFile); rmErr != nil && !os.IsNotExist(rmErr) {
			err = fmt.Errorf("%w (además no pude borrar el dump parcial: %v)", err, rmErr)
		}
		return DumpResult{Elapsed: elapsed}, classifyDumpError(buf.String(), c.User, err)
	}
	return DumpResult{
		File:     outFile,
		Elapsed:  elapsed,
		Warnings: countMatches(buf.String(), "warning:", "WARNING"),
	}, nil
}

// RestoreResult holds the outcome of a restore. Status is "ok" or "warnings".
type RestoreResult struct {
	Status   string
	Elapsed  time.Duration
	Warnings int
	Tables   int
}

// RestoreStream loads a custom-format dump into target using a single
// transaction (all-or-nothing), forwarding each --verbose line to onLine. A
// non-fatal exit (code 1) is reported as "warnings".
func RestoreStream(c config.Conn, dumpFile, target, errLog string, onLine func(string)) (RestoreResult, error) {
	if !ValidHost(c.Host) || !ValidPort(c.Port) || !ValidIdent(c.User) || !ValidIdent(target) {
		return RestoreResult{}, fmt.Errorf("campos de conexión o base destino inválidos")
	}
	start := time.Now()
	cmd := exec.Command("pg_restore", "-h", c.Host, "-p", c.Port, "-U", c.User,
		"-d", target, "-F", "c", "--verbose", "--no-owner", "--no-privileges", "--single-transaction", dumpFile)
	cmd.Env = env(c.Pass)

	var buf bytes.Buffer
	err := runStreaming(cmd, &buf, onLine)
	elapsed := time.Since(start)
	if werr := writeLog(errLog, buf.Bytes()); werr != nil && err == nil {
		err = fmt.Errorf("restore OK pero no pude escribir el log: %w", werr)
	}

	res := RestoreResult{Elapsed: elapsed}
	if err == nil {
		res.Status = "ok"
		res.Tables = CountTables(c, target)
		return res, nil
	}
	if exitCode(err) == 1 {
		res.Status = "warnings"
		res.Warnings = countMatches(buf.String(), "error:", "ERROR")
		res.Tables = CountTables(c, target)
		return res, nil
	}
	return res, classifyRestoreError(buf.String(), err)
}

// VerifyResult describes the integrity of a dump file.
type VerifyResult struct {
	SizeBytes int64
	Objects   int
	Valid     bool
	Err       string
}

// Verify checks a dump is present, non-empty, and readable by pg_restore.
func Verify(file string) VerifyResult {
	fi, err := os.Stat(file)
	if err != nil {
		return VerifyResult{Err: "archivo no encontrado"}
	}
	if fi.Size() == 0 {
		return VerifyResult{Err: "dump vacío (0 bytes): la exportación falló"}
	}
	out, se, e := run("pg_restore", nil, "--list", file)
	if e != nil {
		return VerifyResult{SizeBytes: fi.Size(), Err: "dump corrupto o formato inválido: " + firstLine(se)}
	}
	objs := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			objs++
		}
	}
	return VerifyResult{SizeBytes: fi.Size(), Objects: objs, Valid: true}
}

// CountTables counts BASE TABLE entries in the public schema of db.
func CountTables(c config.Conn, db string) int {
	const q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';`
	out, _, err := psqlRun(c, db, "-t", "-A", "-c", q)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// ── error classification (ports the bash grep heuristics) ────────────────────

func classifyConnError(stderr string, err error) error {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "connection refused"):
		return fmt.Errorf("conexión rechazada — ¿el túnel está caído? (pulsa t)")
	case strings.Contains(low, "password authentication"):
		return fmt.Errorf("contraseña incorrecta")
	case strings.Contains(low, "no password"), strings.Contains(low, "fe_sendauth"):
		return fmt.Errorf("falta contraseña (config o ~/.pgpass)")
	case strings.Contains(low, "not permitted to log in"):
		return fmt.Errorf("el rol no puede iniciar sesión (NOLOGIN)")
	case strings.Contains(low, "does not exist"):
		return fmt.Errorf("el usuario o la base no existe")
	}
	return errors.New(orErr(stderr, err))
}

func classifyDumpError(stderr, user string, err error) error {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "permission denied for"):
		role := user
		if role == "" {
			role = "tu rol"
		}
		return fmt.Errorf("el rol «%s» no puede leer %s.\n\n"+
			"pg_dump necesita leer TODA la base. En el servidor, como admin:\n"+
			"  GRANT pg_read_all_data TO %s;   — PostgreSQL 14+\n"+
			"o usa un rol superusuario para el backup", role, deniedObject(stderr), role)
	case strings.Contains(low, "could not connect"), strings.Contains(low, "connection refused"):
		return fmt.Errorf("se perdió la conexión (¿túnel caído?)")
	case strings.Contains(low, "no password"), strings.Contains(low, "password authentication"):
		return fmt.Errorf("problema de autenticación")
	case strings.Contains(low, "permission denied"):
		return fmt.Errorf("sin permisos para escribir el archivo de backup")
	case strings.Contains(low, "no space left"):
		return fmt.Errorf("sin espacio en disco")
	}
	return errors.New(orErr(stderr, err))
}

// deniedObject extracts the object named in a "permission denied for <kind> <name>"
// line and phrases it in Spanish (e.g. "la tabla «auth_group»").
func deniedObject(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		i := strings.Index(line, "permission denied for ")
		if i < 0 {
			continue
		}
		rest := strings.TrimSpace(line[i+len("permission denied for "):])
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 2 {
			return rest
		}
		name := strings.Trim(parts[1], `"`)
		switch parts[0] {
		case "table":
			return "la tabla «" + name + "»"
		case "schema":
			return "el schema «" + name + "»"
		case "sequence":
			return "la secuencia «" + name + "»"
		case "view":
			return "la vista «" + name + "»"
		default:
			return "«" + name + "»"
		}
	}
	return "algunos objetos de la base"
}

func classifyRestoreError(stderr string, err error) error {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "could not connect"), strings.Contains(low, "connection refused"):
		return fmt.Errorf("se perdió la conexión local")
	case strings.Contains(low, "already exists"):
		return fmt.Errorf("objetos duplicados — usa 'crear base nueva'")
	case strings.Contains(low, "encoding"):
		return fmt.Errorf("encoding incompatible (UTF8 vs LATIN1)")
	case strings.Contains(low, "invalid"):
		return fmt.Errorf("dump dañado — genera uno nuevo")
	case strings.Contains(low, "no space left"):
		return fmt.Errorf("sin espacio en disco")
	}
	return errors.New(orErr(stderr, err))
}

// ── small helpers ────────────────────────────────────────────────────────────

// writeLog persists the captured stderr of a pg_* invocation. The file is
// created with mode 0600 because it can contain the SQL pg_dump/pg_restore
// emitted during --verbose runs (statements, DDL, table data in failure
// messages) — i.e. information a co-located user could use to map the
// schema or extract values from a failed INSERT.
func writeLog(path string, data []byte) error {
	if path == "" || len(data) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// Re-apply the mode in case the file pre-existed (defense vs. legacy 0o644).
	_ = os.Chmod(path, 0o600)
	return nil
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// orErr prefers the first stderr line, falling back to the raw error.
func orErr(stderr string, err error) string {
	if s := firstLine(stderr); s != "" {
		return s
	}
	return err.Error()
}

func countMatches(s string, subs ...string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		for _, sub := range subs {
			if strings.Contains(line, sub) {
				n++
				break
			}
		}
	}
	return n
}

func escapeLiteral(s string) string { return strings.ReplaceAll(s, "'", "''") }
func escapeIdent(s string) string   { return strings.ReplaceAll(s, `"`, `""`) }
