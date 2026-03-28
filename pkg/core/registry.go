package core

import "io"

// AppletFunc is the signature for all busybox applet entry points.
type AppletFunc func(stdio *Stdio, args []string) int

// Global applet registry, populated by cmd/busybox/main.go at init time.
var appletRegistry = map[string]AppletFunc{}

// RegisterApplet adds an applet to the global registry.
func RegisterApplet(name string, run AppletFunc) {
	appletRegistry[name] = run
}

// LookupApplet returns the applet function for the given name, or nil.
func LookupApplet(name string) AppletFunc {
	return appletRegistry[name]
}

// AppletNames returns all registered applet names.
func AppletNames() []string {
	names := make([]string, 0, len(appletRegistry))
	for name := range appletRegistry {
		names = append(names, name)
	}
	return names
}

// CommandResult holds the exit code and output pipes from a command execution.
type CommandResult struct {
	ExitCode int
	Stdout   io.Reader // only set for piped execution
}
