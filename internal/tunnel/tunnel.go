// Package tunnel manages the SSH tunnel to the production database. It opens
// the tunnel on demand using a ~/.ssh/config alias (which must carry its own
// LocalForward) and closes it again on request.
package tunnel

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/ander0code/pgflow/internal/config"
)

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
func Ensure(c *config.Config) (opened bool, err error) {
	if IsUp(c.Prod.Host, c.Prod.Port) {
		return false, nil
	}
	if c.ProdSSH == "" {
		return false, fmt.Errorf("puerto %s cerrado y sin alias SSH configurado", c.Prod.Port)
	}

	cmd := exec.Command("ssh", "-f", "-N", c.ProdSSH)
	var se bytes.Buffer
	cmd.Stderr = &se
	if e := cmd.Run(); e != nil {
		msg := firstLine(se.String())
		if msg == "" {
			msg = e.Error()
		}
		return false, fmt.Errorf("no se pudo abrir el túnel vía '%s': %s", c.ProdSSH, msg)
	}

	for i := 0; i < 10; i++ {
		if IsUp(c.Prod.Host, c.Prod.Port) {
			return true, nil
		}
		time.Sleep(time.Second)
	}
	return false, fmt.Errorf("el túnel no respondió tras abrirlo vía '%s'", c.ProdSSH)
}

// Close kills the ssh process opened for the given alias.
func Close(sshAlias string) error {
	if sshAlias == "" {
		return fmt.Errorf("no hay alias SSH configurado")
	}
	// pkill exits 1 when nothing matched — that's fine, treat as success.
	_ = exec.Command("pkill", "-f", fmt.Sprintf("ssh.*-N.*%s", sshAlias)).Run()
	return nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
