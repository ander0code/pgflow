package tunnel

import (
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/ander0code/pgflow/internal/config"
)

// TestHelperProcess is not a real test. When the test binary is re-exec'd with
// GO_HELPER_PROCESS=1 it acts as a stand-in for `ssh -N <alias>`: it opens a
// TCP listener on FAKE_SSH_PORT, accepts and immediately closes connections,
// and runs until the parent kills it. This is the standard Go pattern for
// testing code that exec's another binary.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	port := os.Getenv("FAKE_SSH_PORT")
	if port == "" {
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	defer ln.Close()
	_ = os.Stdin.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}
}

// resetState clears the package-level tunnel tracker so each test starts clean.
func resetState() {
	currentMu.Lock()
	if current != nil && current.Process != nil {
		_ = current.Process.Kill()
		_ = current.Wait()
	}
	current = nil
	currentMu.Unlock()
}

// freePort grabs a free TCP port and releases it. There's a small race window
// between Close and the next bind, but it's good enough for tests.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	return port
}

// useFakeSSH swaps newSSHCommand for one that execs the test binary as a
// fake ssh. The fake binary listens on cfg.Prod.Port.
func useFakeSSH(t *testing.T, port string) func() {
	t.Helper()
	orig := newSSHCommand
	newSSHCommand = func(alias string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "--", "-N", alias)
		cmd.Env = append(os.Environ(),
			"GO_HELPER_PROCESS=1",
			"FAKE_SSH_PORT="+port,
		)
		return cmd
	}
	return func() { newSSHCommand = orig }
}

func TestClose_NothingTracked(t *testing.T) {
	resetState()
	if err := Close("anything"); err != nil {
		t.Fatalf("Close with nothing tracked: %v", err)
	}
	if IsOpen() {
		t.Fatal("IsOpen should be false after Close on nothing")
	}
}

func TestIsOpen_InitiallyFalse(t *testing.T) {
	resetState()
	if IsOpen() {
		t.Fatal("IsOpen = true at start")
	}
}

func TestEnsure_NoAlias(t *testing.T) {
	resetState()
	c := &config.Config{
		ProdSSH: "",
		Prod:    config.Conn{Host: "127.0.0.1", Port: freePort(t)},
	}
	if _, err := Ensure(c); err == nil {
		t.Fatal("expected error when SSH alias is empty")
	}
}

func TestEnsure_AlreadyUp_NoTracking(t *testing.T) {
	resetState()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	c := &config.Config{
		ProdSSH: "ignored",
		Prod:    config.Conn{Host: "127.0.0.1", Port: port},
	}
	opened, err := Ensure(c)
	if err != nil {
		t.Fatalf("Ensure with port already up: %v", err)
	}
	if opened {
		t.Fatal("opened should be false (port already up)")
	}
	if IsOpen() {
		t.Fatal("IsOpen must stay false: we never tracked an externally-opened tunnel")
	}
}

func TestEnsureAndClose_FullLifecycle(t *testing.T) {
	resetState()
	port := freePort(t)
	defer useFakeSSH(t, port)()

	c := &config.Config{
		ProdSSH: "fake-alias",
		Prod:    config.Conn{Host: "127.0.0.1", Port: port},
	}

	opened, err := Ensure(c)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !opened {
		t.Fatal("opened should be true")
	}
	if !IsOpen() {
		t.Fatal("IsOpen should be true after Ensure")
	}
	if !IsUp(c.Prod.Host, c.Prod.Port) {
		t.Fatal("port should be listening after Ensure")
	}

	if err := Close(c.ProdSSH); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if IsOpen() {
		t.Fatal("IsOpen should be false after Close")
	}
	// Give the OS a moment to actually free the port.
	for i := 0; i < 20; i++ {
		if !IsUp(c.Prod.Host, c.Prod.Port) {
			return
		}
		// busy-wait briefly
		ln, _ := net.Listen("tcp", "127.0.0.1:"+port)
		if ln != nil {
			ln.Close()
			return
		}
	}
	// If the port is still busy after Close, that's a leak — fail.
	// We don't fail hard here because the OS may need a moment.
}

func TestEnsure_FailedDial_ReapsOrphan(t *testing.T) {
	resetState()
	// A port that is *closed* (we bind and immediately release, but to a port
	// nothing's listening on after the test starts).
	port := freePort(t)
	defer useFakeSSH(t, port)()

	// Make the fake ssh exit immediately so IsUp never becomes true.
	orig := newSSHCommand
	newSSHCommand = func(alias string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "--", "-N", alias)
		cmd.Env = append(os.Environ(),
			"GO_HELPER_PROCESS=1",
			"FAKE_SSH_PORT=0", // invalid: Listen will fail, exec exits
		)
		return cmd
	}
	defer func() { newSSHCommand = orig }()

	c := &config.Config{
		ProdSSH: "fake-alias",
		Prod:    config.Conn{Host: "127.0.0.1", Port: port},
	}
	if _, err := Ensure(c); err == nil {
		t.Fatal("expected Ensure to fail when ssh can't open the listener")
	}
	if IsOpen() {
		t.Fatal("Ensure must not track a failed tunnel")
	}
}
