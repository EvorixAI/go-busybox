// Package lua wraps github.com/yuin/gopher-lua to provide a Lua 5.1
// interpreter as a busybox applet.
//
// Supported usage:
//
//	lua FILE [args...]   → run script
//	lua -e CHUNK         → run inline chunk
//	lua -                → read script from stdin
//
// Lua 5.1 only — no <close>, integer subtype, bitwise ops, or other
// 5.4-only features. If a recipe demands 5.4, file a fork issue.
package lua

import (
	"io"
	"os"
	"strings"

	"github.com/rcarmo/go-busybox/pkg/core"
	glua "github.com/yuin/gopher-lua"
)

// Run executes a Lua chunk read from -e, a file argument, or stdin.
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
				return core.UsageError(stdio, "lua", "option -e requires an argument")
			}
			inlineChunks = append(inlineChunks, args[i])
			i++
		case arg == "-":
			stdinScript = true
			i++
			scriptArgs = append(scriptArgs, args[i:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-"):
			return core.UsageError(stdio, "lua", "unknown option: "+arg)
		default:
			scriptPath = arg
			i++
			scriptArgs = append(scriptArgs, args[i:]...)
			i = len(args)
		}
	}

	L := glua.NewState()
	defer L.Close()

	// Wire stdio: print() goes to stdio.Out; io.read uses stdio.In.
	L.SetGlobal("print", L.NewFunction(func(L *glua.LState) int {
		n := L.GetTop()
		parts := make([]string, 0, n)
		for j := 1; j <= n; j++ {
			parts = append(parts, L.ToStringMeta(L.Get(j)).String())
		}
		stdio.Println(strings.Join(parts, "\t"))
		return 0
	}))

	// Populate `arg` table for script arguments (Lua convention).
	argTable := L.NewTable()
	for j, a := range scriptArgs {
		L.RawSetInt(argTable, j+1, glua.LString(a))
	}
	L.SetGlobal("arg", argTable)

	for _, chunk := range inlineChunks {
		if err := L.DoString(chunk); err != nil {
			stdio.Errorf("lua: %v\n", err)
			return core.ExitFailure
		}
	}

	if stdinScript {
		src, err := io.ReadAll(stdio.In)
		if err != nil {
			stdio.Errorf("lua: read stdin: %v\n", err)
			return core.ExitFailure
		}
		if err := L.DoString(string(src)); err != nil {
			stdio.Errorf("lua: %v\n", err)
			return core.ExitFailure
		}
		return core.ExitSuccess
	}

	if scriptPath != "" {
		f, err := os.Open(scriptPath)
		if err != nil {
			return core.FileError(stdio, "lua", scriptPath, err)
		}
		src, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			return core.FileError(stdio, "lua", scriptPath, readErr)
		}
		if err := L.DoString(string(src)); err != nil {
			stdio.Errorf("lua: %v\n", err)
			return core.ExitFailure
		}
	}

	if scriptPath == "" && !stdinScript && len(inlineChunks) == 0 {
		return core.UsageError(stdio, "lua", "missing script (use FILE, -e CHUNK, or -)")
	}

	return core.ExitSuccess
}
