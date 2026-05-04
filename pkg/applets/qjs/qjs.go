// Package qjs wraps github.com/dop251/goja to provide an ECMAScript
// runtime as a busybox applet. Named `qjs` for muscle memory with
// QuickJS, but the engine is goja — pure Go, no JIT.
//
// Supported usage:
//
//	qjs FILE [args...]   → run script
//	qjs -e SOURCE        → run inline source
//	qjs -                → read source from stdin
//
// Differences from QuickJS:
//   - No JIT (interpreted only — App Store safe)
//   - Some ES2020+ features may be incomplete; see goja's compatibility matrix
package qjs

import (
	"io"
	"os"
	"strings"

	"github.com/dop251/goja"
	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run evaluates JavaScript from -e, a file, or stdin.
func Run(stdio *core.Stdio, args []string) int {
	var inlineChunks []string
	var scriptPath string
	stdinScript := false
	scriptArgs := []string{}

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "-e":
			i++
			if i >= len(args) {
				return core.UsageError(stdio, "qjs", "option -e requires an argument")
			}
			inlineChunks = append(inlineChunks, args[i])
			i++
		case arg == "-":
			stdinScript = true
			i++
			scriptArgs = append(scriptArgs, args[i:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-"):
			return core.UsageError(stdio, "qjs", "unknown option: "+arg)
		default:
			scriptPath = arg
			i++
			scriptArgs = append(scriptArgs, args[i:]...)
			i = len(args)
		}
	}

	vm := goja.New()

	// Minimal console.log shim — agents reach for this constantly.
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for k, v := range call.Arguments {
			parts[k] = v.String()
		}
		stdio.Println(strings.Join(parts, " "))
		return goja.Undefined()
	})
	console.Set("error", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for k, v := range call.Arguments {
			parts[k] = v.String()
		}
		stdio.Errorf("%s\n", strings.Join(parts, " "))
		return goja.Undefined()
	})
	vm.Set("console", console)

	// Expose script args under `process.argv` (Node-style).
	process := vm.NewObject()
	argv := []string{"qjs"}
	if scriptPath != "" {
		argv = append(argv, scriptPath)
	}
	argv = append(argv, scriptArgs...)
	process.Set("argv", argv)
	vm.Set("process", process)

	for _, chunk := range inlineChunks {
		if _, err := vm.RunString(chunk); err != nil {
			stdio.Errorf("qjs: %v\n", err)
			return core.ExitFailure
		}
	}

	if stdinScript {
		src, err := io.ReadAll(stdio.In)
		if err != nil {
			stdio.Errorf("qjs: read stdin: %v\n", err)
			return core.ExitFailure
		}
		if _, err := vm.RunString(string(src)); err != nil {
			stdio.Errorf("qjs: %v\n", err)
			return core.ExitFailure
		}
		return core.ExitSuccess
	}

	if scriptPath != "" {
		f, err := os.Open(scriptPath)
		if err != nil {
			return core.FileError(stdio, "qjs", scriptPath, err)
		}
		src, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			return core.FileError(stdio, "qjs", scriptPath, readErr)
		}
		if _, err := vm.RunString(string(src)); err != nil {
			stdio.Errorf("qjs: %v\n", err)
			return core.ExitFailure
		}
	}

	if scriptPath == "" && !stdinScript && len(inlineChunks) == 0 {
		return core.UsageError(stdio, "qjs", "missing script (use FILE, -e SOURCE, or -)")
	}

	return core.ExitSuccess
}
