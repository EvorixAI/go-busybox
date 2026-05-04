// Package touch implements the touch command — create a file if it
// doesn't exist, update its modification time if it does.
package touch

import (
	"os"
	"time"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run creates or updates the modification time of each file argument.
//
// Supported flags:
//
//	-c   Do not create files that don't exist
//	-a   Change access time only (still creates file unless -c)
//	-m   Change modification time only (default behaviour)
func Run(stdio *core.Stdio, args []string) int {
	noCreate := false
	startIdx := 0
	for i, arg := range args {
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		valid := true
		for _, c := range arg[1:] {
			switch c {
			case 'c', 'a', 'm':
			default:
				valid = false
			}
		}
		if !valid {
			break
		}
		for _, c := range arg[1:] {
			if c == 'c' {
				noCreate = true
			}
		}
		startIdx = i + 1
	}

	if startIdx >= len(args) {
		return core.UsageError(stdio, "touch", "missing file operand")
	}

	now := time.Now()
	exit := core.ExitSuccess
	for _, path := range args[startIdx:] {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			if noCreate {
				continue
			}
			f, createErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
			if createErr != nil {
				exit = core.FileError(stdio, "touch", path, createErr)
				continue
			}
			f.Close()
			continue
		}
		if err != nil {
			exit = core.FileError(stdio, "touch", path, err)
			continue
		}
		if err := os.Chtimes(path, now, now); err != nil {
			exit = core.FileError(stdio, "touch", path, err)
		}
	}
	return exit
}
