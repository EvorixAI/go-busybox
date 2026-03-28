//go:build !js && !wasm && !wasip1

package core

import (
	"io"
	"os"
	"os/exec"
)

// ExecCommand runs a command, either as a registered applet (in-process) or
// as an external OS process. Returns the exit code.
//
// On native platforms, this first checks the applet registry, then falls back
// to exec.Command for external binaries.
func ExecCommand(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) int {
	// Check applet registry first
	if run := LookupApplet(name); run != nil {
		stdio := &Stdio{In: stdin, Out: stdout, Err: stderr}
		return run(stdio, args)
	}

	// Fall back to OS exec
	cmd := exec.Command(name, args...) // #nosec G204
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return ExitFailure
	}
	return ExitSuccess
}

// StartCommandAsync starts a command asynchronously and returns a channel
// that will receive the exit code when the command completes.
// On native platforms, applets run in a goroutine; external commands use exec.
func StartCommandAsync(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, env []string) (pid int, done chan int) {
	done = make(chan int, 1)

	// Check applet registry first
	if run := LookupApplet(name); run != nil {
		go func() {
			stdio := &Stdio{In: stdin, Out: stdout, Err: stderr}
			done <- run(stdio, args)
			close(done)
		}()
		return 0, done // no real PID for in-process applets
	}

	// Fall back to OS exec
	cmd := exec.Command(name, args...) // #nosec G204
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		done <- ExitFailure
		close(done)
		return 0, done
	}

	pid = cmd.Process.Pid
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				done <- exitErr.ExitCode()
			} else {
				done <- ExitFailure
			}
		} else {
			done <- ExitSuccess
		}
		close(done)
	}()
	return pid, done
}

// IsExecutable checks if a command is available (applet or PATH lookup).
func IsExecutable(name string) bool {
	if LookupApplet(name) != nil {
		return true
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// PlatformSupportsJobs returns true on native platforms.
func PlatformSupportsJobs() bool {
	return true
}

// GetPid returns the current process PID.
func GetPid() int {
	return os.Getpid()
}
