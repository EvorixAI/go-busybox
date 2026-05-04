// Package basename implements the basename command — strip directory
// and optional suffix from a path.
package basename

import (
	"path/filepath"
	"strings"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run prints the basename of args[0]. If args[1] is given, it is treated
// as a suffix to strip from the basename.
func Run(stdio *core.Stdio, args []string) int {
	if len(args) == 0 {
		return core.UsageError(stdio, "basename", "missing operand")
	}
	base := filepath.Base(args[0])
	if len(args) >= 2 {
		base = strings.TrimSuffix(base, args[1])
	}
	stdio.Println(base)
	return core.ExitSuccess
}
