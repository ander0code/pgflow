// Package tunnel manages the SSH tunnel to the production database. It opens
// the tunnel on demand using a ~/.ssh/config alias (which must carry its own
// LocalForward) and closes it again on request.
//
// The tunnel is started as a Go-managed subprocess (no `ssh -f`), so it works
// identically on macOS, Linux and Windows (where `ssh.exe` ships with Win10
// 1809+). Closing is by PID via Process.Kill — no `pkill` needed.
//
// Ensure reaps any previous tunnel subprocess that has not yet been waited
// on (both when the previous one is still tracked and when the dial loop
// times out on a new one), so a crashed or restarted pgflow cannot leak ssh
// processes. The alias is validated against an allowlist before being
// passed to ssh.
package tunnel

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/ander0code/pgflow/internal/config"
)

var (
	currentMu sync.Mutex
	current   *exec.Cmd
)

// sshCmd is the binary used to open the tunnel. Tests may override this to
// point at a helper binary; production code should leave it as "ssh".
var sshCmd = "ssh"

// newSSHCommand builds the `ssh -N <alias>` command. Overridable by tests so
// they can inject a fake ssh (see TestHelperProcess in tunnel_test.go).
var newSSHCommand = func(alias string) *exec.Cmd {
	cmd := exec.Command(sshCmd, "-N", alias)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	hideWindow(cmd)
	return cmd
}

// IsUp reports whether something is accepting connections on host:port.
// Uses a plain TCP dial, so no `nc` dependency is needed.
func IsUp(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Ensure makes sure the prod tunnel is up, opening it via the SSH alias if the
// local end isn't already listening. Returns true if it opened a new tunnel.
//
// The tunnel is tracked as a Go subprocess (current) so Close can reap it by
// PID, with no shell or `pkill` involved. Cross-platform: works on Unix and
// Windows OpenSSH alike.
//
// The alias is validated against a strict allowlist so a misconfigured
// ~/.pgflow.conf (or a shared dotfiles repo) cannot smuggle ssh options
// (e.g. "-oProxyCommand=touch /tmp/pwned") that ssh would otherwise parse
// from its own argv.
func Ensure(c *config.Config) (opened bool, err error) {
	// Phase 1 (under lock): validate, take a decision, and capture any
	// previous cmd that needs reaping. We deliberately do NOT call
	// cmd.Wait() here — that can block for an arbitrary time while the
	// OS reaps the ssh process, and the TUI is waiting on us.
	currentMu.Lock()

	if IsUp(c.Prod.Host, c.Prod.Port) {
		currentMu.Unlock()
		return false, nil
	}
	if c.ProdSSH == "" {
		currentMu.Unlock()
		return false, fmt.Errorf("puerto %s cerrado y sin alias SSH configurado", c.Prod.Port)
	}
	if !ValidAlias(c.ProdSSH) {
		currentMu.Unlock()
		return false, fmt.Errorf("alias SSH inválido %q: solo letras, dígitos, '-', '_' o '.'", c.ProdSSH)
	}

	// If we previously opened a tunnel that we haven't reaped, take a
	// reference to it and clear the slot. We'll reap it after releasing
	// the lock so the TUI is not blocked on a slow process exit.
	var stale *exec.Cmd
	if current != nil && current.Process != nil {
		stale = current
		current = nil
	}

	cmd := newSSHCommand(c.ProdSSH)
	if e := cmd.Start(); e != nil {
		currentMu.Unlock()
		if stale != nil {
			_ = stale.Process.Kill()
			_ = stale.Wait()
		}
		return false, fmt.Errorf("no se pudo abrir el túnel vía '%s': %v", c.ProdSSH, e)
	}

	for i := 0; i < 10; i++ {
		if IsUp(c.Prod.Host, c.Prod.Port) {
			current = cmd
			currentMu.Unlock()
			if stale != nil {
				_ = stale.Process.Kill()
				_ = stale.Wait()
			}
			return true, nil
		}
		time.Sleep(time.Second)
	}
	currentMu.Unlock()
	// Tunnel didn't come up — reap the orphan before reporting the failure.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if stale != nil {
		_ = stale.Process.Kill()
		_ = stale.Wait()
	}
	return false, fmt.Errorf("el túnel no respondió tras abrirlo vía '%s'", c.ProdSSH)
}

// Close kills the tunnel that pgflow itself opened. It is a no-op if no
// tunnel is tracked (e.g. the user opened one externally — we don't touch it).
//
// cmd.Wait() is performed outside the package lock so a slow process exit
// does not block other tunnel operations (the TUI's status refresh, for
// example).
func Close(sshAlias string) error {
	currentMu.Lock()
	cmd := current
	current = nil
	currentMu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	killErr := cmd.Process.Kill()
	_ = cmd.Wait() // reap the zombie, regardless of killErr
	return killErr
}

// IsOpen reports whether pgflow is currently tracking a tunnel subprocess.
// Useful for tests and diagnostics; the TUI uses IsUp for the user-facing badge.
func IsOpen() bool {
	currentMu.Lock()
	defer currentMu.Unlock()
	return current != nil && current.Process != nil
}

// ValidAlias accepts the characters that are safe in an ssh_config "Host"
// pattern token. It rejects anything starting with '-' (ssh would parse it
// as an option), '=' (ssh assigns a value), whitespace, and shell metachars.
// Exported so the TUI config editor can validate user input.
func ValidAlias(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r == '_' || r == '.' || r == '*' || r == '?' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}
