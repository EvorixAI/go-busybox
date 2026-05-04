// Package dirname implements the dirname command — strip the last
// path component, leaving the parent directory.
package dirname

import (
	"path/filepath"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run prints the directory portion of args[0].
func Run(stdio *core.Stdio, args []string) int {
	if len(args) == 0 {
		return core.UsageError(stdio, "dirname", "missing operand")
	}
	stdio.Println(filepath.Dir(args[0]))
	return core.ExitSuccess
}
