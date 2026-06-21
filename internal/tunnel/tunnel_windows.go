//go:build windows

package tunnel

import (
	"os/exec"
	"syscall"
)

// hideWindow stops ssh.exe from flashing a console window when spawned as a
// child of pgflow. CREATE_NO_WINDOW (0x08000000) is the documented Win32 flag
// for "no new console for this child".
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
