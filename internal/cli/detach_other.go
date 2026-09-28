//go:build !unix

package cli

import "os/exec"

// detach is a no-op where there is no process group to leave. basa ships for
// macOS and Linux only; this keeps the package building elsewhere.
func detach(*exec.Cmd) {}
