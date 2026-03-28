//go:build js || wasm || wasip1

package core

import (
	"io"
)

// ExecCommand runs a command as a registered applet (in-process).
// On WASI platforms, only registered applets can be executed — no external
// processes are available.
func ExecCommand(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) int {
	if run := LookupApplet(name); run != nil {
		stdio := &Stdio{In: stdin, Out: stdout, Err: stderr}
		return run(stdio, args)
	}
	errStdio := &Stdio{Err: stderr}
	errStdio.Errorf("ash: %s: command not found\n", name)
	return 127
}

// StartCommandAsync starts a command asynchronously in a goroutine.
// On WASI, all commands are applets running in-process.
func StartCommandAsync(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) (pid int, done chan int) {
	done = make(chan int, 1)

	if run := LookupApplet(name); run != nil {
		go func() {
			stdio := &Stdio{In: stdin, Out: stdout, Err: stderr}
			done <- run(stdio, args)
			close(done)
		}()
		return 0, done
	}

	errStdio := &Stdio{Err: stderr}
	errStdio.Errorf("ash: %s: command not found\n", name)
	done <- 127
	close(done)
	return 0, done
}

// IsExecutable checks if a command is a registered applet.
func IsExecutable(name string) bool {
	return LookupApplet(name) != nil
}

// PlatformSupportsJobs returns false on WASI — no process control.
func PlatformSupportsJobs() bool {
	return false
}

// GetPid returns a dummy PID on WASI.
func GetPid() int {
	return 1
}
