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

func psqlArgs(c config.Conn, db string, extra ...string) []string {
	args := []string{"-w", "-h", c.Host, "-p", c.Port, "-U", c.User, "-d", db}
	return append(args, extra...)
}

// TestConn checks connectivity and returns the server version on success.
func TestConn(c config.Conn) (version string, err error) {
	_, se, e := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-c", "SELECT 1;")...)
	if e != nil {
		return "", classifyConnError(se, e)
	}
	v, _, _ := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-t", "-A", "-c", "SHOW server_version;")...)
	return strings.TrimSpace(v), nil
}

// ListDatabases returns the non-template, user databases sorted by name.
func ListDatabases(c config.Conn) ([]string, error) {
	const q = `SELECT datname FROM pg_database WHERE datistemplate=false AND datname NOT IN ('postgres','rdsadmin') ORDER BY datname;`
	out, se, err := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-t", "-A", "-c", q)...)
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

// DatabaseExists reports whether a database with the given name exists.
func DatabaseExists(c config.Conn, name string) bool {
	q := fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s';", escapeLiteral(name))
	out, _, err := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-t", "-A", "-c", q)...)
	return err == nil && strings.TrimSpace(out) == "1"
}

// CreateDatabase creates a new database.
func CreateDatabase(c config.Conn, name string) error {
	q := fmt.Sprintf(`CREATE DATABASE "%s";`, escapeIdent(name))
	_, se, err := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-c", q)...)
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
	ident := escapeIdent(name)
	lit := escapeLiteral(name)

	run("psql", env(c.Pass), psqlArgs(c, "postgres", "-c",
		fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='%s' AND pid <> pg_backend_pid();", lit))...)

	_, se, err := run("psql", env(c.Pass), psqlArgs(c, "postgres", "-c", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, ident))...)
	if err != nil {
		low := strings.ToLower(se)
		if strings.Contains(low, "being accessed") || strings.Contains(low, "other users") {
			return fmt.Errorf("hay conexiones activas a '%s'; ciérralas y reintenta", name)
		}
		return fmt.Errorf("no se pudo eliminar '%s': %s", name, firstLine(se))
	}

	_, se, err = run("psql", env(c.Pass), psqlArgs(c, "postgres", "-c", fmt.Sprintf(`CREATE DATABASE "%s";`, ident))...)
	if err != nil {
		return fmt.Errorf("no se pudo crear '%s': %s", name, firstLine(se))
	}
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
	start := time.Now()
	cmd := exec.Command("pg_dump", "-h", c.Host, "-p", c.Port, "-U", c.User,
		"-F", "c", "--verbose", "--no-password", "-f", outFile, db)
	cmd.Env = env(c.Pass)

	var buf bytes.Buffer
	err := runStreaming(cmd, &buf, onLine)
	elapsed := time.Since(start)
	writeLog(errLog, buf.Bytes())

	if err != nil {
		os.Remove(outFile) // drop the incomplete dump
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
	start := time.Now()
	cmd := exec.Command("pg_restore", "-h", c.Host, "-p", c.Port, "-U", c.User,
		"-d", target, "-F", "c", "--verbose", "--no-owner", "--no-privileges", "--single-transaction", dumpFile)
	cmd.Env = env(c.Pass)

	var buf bytes.Buffer
	err := runStreaming(cmd, &buf, onLine)
	elapsed := time.Since(start)
	writeLog(errLog, buf.Bytes())

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
	out, _, err := run("psql", env(c.Pass), psqlArgs(c, db, "-t", "-A", "-c", q)...)
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

func writeLog(path string, data []byte) {
	if path == "" || len(data) == 0 {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
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
