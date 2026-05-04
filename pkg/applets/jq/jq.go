// Package jq wraps github.com/itchyny/gojq to provide a jq-compatible
// JSON processor as a busybox applet.
//
// Supported usage:
//
//	jq FILTER                 → read JSON from stdin
//	jq FILTER FILE [FILE...]  → read JSON from each file
//	jq -n FILTER              → input is null (use with --argjson)
//	jq -r FILTER              → raw output (strings unquoted)
//	jq -c FILTER              → compact output (no pretty-print)
//	jq --slurp FILTER         → read entire input into array first
//
// Not supported in this minimal wrapper: --arg/--argjson, modules,
// imports, multiple --filter args. Reach for python or qjs if jq
// can't express what you need.
package jq

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run executes a jq filter against stdin or one/more files.
func Run(stdio *core.Stdio, args []string) int {
	rawOutput := false
	compact := false
	nullInput := false
	slurp := false
	startIdx := 0

flagloop:
	for i, arg := range args {
		switch arg {
		case "-r", "--raw-output":
			rawOutput = true
		case "-c", "--compact-output":
			compact = true
		case "-n", "--null-input":
			nullInput = true
		case "-s", "--slurp":
			slurp = true
		case "--":
			startIdx = i + 1
			break flagloop
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return core.UsageError(stdio, "jq", "unknown option: "+arg)
			}
			startIdx = i
			break flagloop
		}
		startIdx = i + 1
	}

	if startIdx >= len(args) {
		return core.UsageError(stdio, "jq", "missing filter argument")
	}
	filterSrc := args[startIdx]
	files := args[startIdx+1:]

	query, err := gojq.Parse(filterSrc)
	if err != nil {
		stdio.Errorf("jq: parse error: %v\n", err)
		return core.ExitFailure
	}

	encoderFor := func(w io.Writer) *json.Encoder {
		enc := json.NewEncoder(w)
		if !compact {
			enc.SetIndent("", "  ")
		}
		return enc
	}

	emit := func(v any) error {
		if rawOutput {
			if s, ok := v.(string); ok {
				_, err := io.WriteString(stdio.Out, s+"\n")
				return err
			}
		}
		return encoderFor(stdio.Out).Encode(v)
	}

	runOne := func(input any) int {
		iter := query.Run(input)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if e, isErr := v.(error); isErr {
				stdio.Errorf("jq: %v\n", e)
				return core.ExitFailure
			}
			if err := emit(v); err != nil {
				stdio.Errorf("jq: write: %v\n", err)
				return core.ExitFailure
			}
		}
		return core.ExitSuccess
	}

	if nullInput {
		return runOne(nil)
	}

	processReader := func(r io.Reader) int {
		if slurp {
			var slice []any
			dec := json.NewDecoder(r)
			for {
				var v any
				if err := dec.Decode(&v); err == io.EOF {
					break
				} else if err != nil {
					stdio.Errorf("jq: decode: %v\n", err)
					return core.ExitFailure
				}
				slice = append(slice, v)
			}
			return runOne(slice)
		}
		dec := json.NewDecoder(r)
		for {
			var v any
			if err := dec.Decode(&v); err == io.EOF {
				return core.ExitSuccess
			} else if err != nil {
				stdio.Errorf("jq: decode: %v\n", err)
				return core.ExitFailure
			}
			if code := runOne(v); code != core.ExitSuccess {
				return code
			}
		}
	}

	if len(files) == 0 {
		return processReader(stdio.In)
	}

	exit := core.ExitSuccess
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			exit = core.FileError(stdio, "jq", path, err)
			continue
		}
		if code := processReader(f); code != core.ExitSuccess {
			exit = code
		}
		f.Close()
	}
	return exit
}
