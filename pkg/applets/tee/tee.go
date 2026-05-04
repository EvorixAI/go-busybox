// Package tee implements the tee command — copy stdin to stdout
// and to one or more files.
package tee

import (
	"io"
	"os"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run copies stdin to stdout and to each file argument.
//
// Supported flags:
//
//	-a   Append to files instead of overwriting
func Run(stdio *core.Stdio, args []string) int {
	doAppend := false
	startIdx := 0
loop:
	for i, arg := range args {
		switch {
		case arg == "-a" || arg == "--append":
			doAppend = true
			startIdx = i + 1
		case len(arg) > 0 && arg[0] == '-' && arg != "-":
			startIdx = i + 1
		default:
			startIdx = i
			break loop
		}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if doAppend {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	writers := []io.Writer{stdio.Out}
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	exit := core.ExitSuccess
	for _, path := range args[startIdx:] {
		f, err := os.OpenFile(path, flags, 0644)
		if err != nil {
			exit = core.FileError(stdio, "tee", path, err)
			continue
		}
		files = append(files, f)
		writers = append(writers, f)
	}

	mw := io.MultiWriter(writers...)
	if _, err := io.Copy(mw, stdio.In); err != nil {
		stdio.Errorf("tee: %v\n", err)
		return core.ExitFailure
	}
	return exit
}
