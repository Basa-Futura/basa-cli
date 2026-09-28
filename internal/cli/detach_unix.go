//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// detach puts the background check in its own process group, so the Ctrl-C
// or hangup that ends the command that started it does not end the check too.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
