// Package chmod implements the chmod command — change file mode bits.
package chmod

import (
	"os"
	"strconv"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run changes the mode bits of one or more files. Only octal modes
// (e.g. 644, 0755) are supported — symbolic forms like u+x are not.
func Run(stdio *core.Stdio, args []string) int {
	if len(args) < 2 {
		return core.UsageError(stdio, "chmod", "missing operand")
	}

	modeStr := args[0]
	// Strip leading 0 if present so strconv treats the rest as octal.
	if len(modeStr) > 1 && modeStr[0] == '0' {
		modeStr = modeStr[1:]
	}
	mode64, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		stdio.Errorf("chmod: invalid mode: %s\n", args[0])
		return core.ExitUsage
	}
	mode := os.FileMode(mode64)

	exit := core.ExitSuccess
	for _, path := range args[1:] {
		if err := os.Chmod(path, mode); err != nil {
			exit = core.FileError(stdio, "chmod", path, err)
		}
	}
	return exit
}
