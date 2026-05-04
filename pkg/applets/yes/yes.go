// Package yes implements the yes command — print a string repeatedly.
//
// In WASI there is no signal-based interruption from a parent process;
// yes still terminates when the calling shell closes its stdout (the
// write returns EPIPE-equivalent and we exit cleanly).
package yes

import (
	"strings"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run prints args joined by spaces (or "y" if no args), once per line,
// forever — until stdout is closed.
func Run(stdio *core.Stdio, args []string) int {
	line := "y"
	if len(args) > 0 {
		line = strings.Join(args, " ")
	}
	line += "\n"
	for {
		if _, err := stdio.Out.Write([]byte(line)); err != nil {
			return core.ExitSuccess
		}
	}
}
