// Package tunnel manages the SSH tunnel to the production database. It opens
// the tunnel on demand using a ~/.ssh/config alias (which must carry its own
// LocalForward) and closes it again on request.
//
// The tunnel is started as a Go-managed subprocess (no `ssh -f`), so it works
// identically on macOS, Linux and Windows (where `ssh.exe` ships with Win10
// 1809+). Closing is by PID via Process.Kill — no `pkill` needed.
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
func Ensure(c *config.Config) (opened bool, err error) {
	currentMu.Lock()
	defer currentMu.Unlock()

	if IsUp(c.Prod.Host, c.Prod.Port) {
		return false, nil
	}
	if c.ProdSSH == "" {
		return false, fmt.Errorf("puerto %s cerrado y sin alias SSH configurado", c.Prod.Port)
	}

	// If we previously opened a tunnel that we haven't reaped, kill it first
	// so we never accumulate orphans (e.g. after a failed dial loop).
	if current != nil && current.Process != nil {
		_ = current.Process.Kill()
		_ = current.Wait()
		current = nil
	}

	cmd := newSSHCommand(c.ProdSSH)

	if e := cmd.Start(); e != nil {
		return false, fmt.Errorf("no se pudo abrir el túnel vía '%s': %v", c.ProdSSH, e)
	}

	for i := 0; i < 10; i++ {
		if IsUp(c.Prod.Host, c.Prod.Port) {
			current = cmd
			return true, nil
		}
		time.Sleep(time.Second)
	}
	// Tunnel didn't come up — reap the orphan before reporting the failure.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return false, fmt.Errorf("el túnel no respondió tras abrirlo vía '%s'", c.ProdSSH)
}

// Close kills the tunnel that pgflow itself opened. It is a no-op if no
// tunnel is tracked (e.g. the user opened one externally — we don't touch it).
func Close(sshAlias string) error {
	currentMu.Lock()
	defer currentMu.Unlock()

	if current == nil || current.Process == nil {
		return nil
	}
	killErr := current.Process.Kill()
	_ = current.Wait() // reap the zombie, regardless of killErr
	current = nil
	return killErr
}

// IsOpen reports whether pgflow is currently tracking a tunnel subprocess.
// Useful for tests and diagnostics; the TUI uses IsUp for the user-facing badge.
func IsOpen() bool {
	currentMu.Lock()
	defer currentMu.Unlock()
	return current != nil && current.Process != nil
}
