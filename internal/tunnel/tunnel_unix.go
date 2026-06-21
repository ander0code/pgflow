//go:build !windows

package tunnel

import "os/exec"

// hideWindow is a no-op on Unix-like systems: ssh running as a child of pgflow
// inherits no TTY, so no spurious console window is ever shown.
func hideWindow(*exec.Cmd) {}
