// Package yq wraps gopkg.in/yaml.v3 + github.com/itchyny/gojq to
// provide a yq-shaped YAML processor. Implementation note: rather
// than vendoring the full mikefarah/yq CLI (which pulls in cobra and
// viper, doubling binary size), we run the same jq filter against
// YAML-decoded-to-Go values, then emit the result as YAML.
//
// Supported usage:
//
//	yq FILTER                  → read YAML from stdin
//	yq FILTER FILE [FILE...]   → read YAML from each file
//	yq -r FILTER               → raw output (strings unquoted)
//
// Sufficient for the common agent recipes (path access, key/value
// extraction). For richer yq features (eval-all, in-place edits,
// merge ops), reach for a follow-up wrapper around mikefarah/yq.
package yq

import (
	"io"
	"os"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/rcarmo/go-busybox/pkg/core"
	"gopkg.in/yaml.v3"
)

// Run applies a jq-syntax filter to YAML input.
func Run(stdio *core.Stdio, args []string) int {
	rawOutput := false
	startIdx := 0
flagloop:
	for i, arg := range args {
		switch arg {
		case "-r", "--raw-output":
			rawOutput = true
		case "--":
			startIdx = i + 1
			break flagloop
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return core.UsageError(stdio, "yq", "unknown option: "+arg)
			}
			startIdx = i
			break flagloop
		}
		startIdx = i + 1
	}

	if startIdx >= len(args) {
		return core.UsageError(stdio, "yq", "missing filter argument")
	}
	filterSrc := args[startIdx]
	files := args[startIdx+1:]

	query, err := gojq.Parse(filterSrc)
	if err != nil {
		stdio.Errorf("yq: parse error: %v\n", err)
		return core.ExitFailure
	}

	emit := func(v any) error {
		if rawOutput {
			if s, ok := v.(string); ok {
				_, err := io.WriteString(stdio.Out, s+"\n")
				return err
			}
		}
		enc := yaml.NewEncoder(stdio.Out)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	}

	processReader := func(r io.Reader) int {
		dec := yaml.NewDecoder(r)
		for {
			var v any
			if err := dec.Decode(&v); err == io.EOF {
				return core.ExitSuccess
			} else if err != nil {
				stdio.Errorf("yq: decode: %v\n", err)
				return core.ExitFailure
			}
			normalised := normaliseYaml(v)
			iter := query.Run(normalised)
			for {
				out, ok := iter.Next()
				if !ok {
					break
				}
				if e, isErr := out.(error); isErr {
					stdio.Errorf("yq: %v\n", e)
					return core.ExitFailure
				}
				if err := emit(out); err != nil {
					stdio.Errorf("yq: write: %v\n", err)
					return core.ExitFailure
				}
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
			exit = core.FileError(stdio, "yq", path, err)
			continue
		}
		if code := processReader(f); code != core.ExitSuccess {
			exit = code
		}
		f.Close()
	}
	return exit
}

// normaliseYaml converts YAML-decoded values into the shape gojq
// expects — map[string]any keys (not interface{}-keyed maps) and
// []any slices.
func normaliseYaml(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normaliseYaml(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[stringifyKey(k)] = normaliseYaml(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normaliseYaml(val)
		}
		return out
	default:
		return v
	}
}

func stringifyKey(k any) string {
	if s, ok := k.(string); ok {
		return s
	}
	return strings.TrimSpace(stringify(k))
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		buf := &strings.Builder{}
		_ = yaml.NewEncoder(buf).Encode(v)
		return strings.TrimRight(buf.String(), "\n")
	}
}
