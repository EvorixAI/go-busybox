// Package base64 implements the base64 command — encode/decode stdin
// or a file as standard base64 (RFC 4648).
package base64

import (
	"encoding/base64"
	"io"
	"os"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run encodes (default) or decodes (-d) input.
//
// Supported flags:
//
//	-d   Decode instead of encode
//	-w N Wrap encoded output at N columns (0 disables wrapping; default 76)
//
// If no FILE argument is given (or FILE is "-"), reads from stdin.
func Run(stdio *core.Stdio, args []string) int {
	decode := false
	wrap := 76
	startIdx := 0

	i := 0
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "-d", "--decode":
			decode = true
			i++
		case "-w", "--wrap":
			i++
			if i >= len(args) {
				return core.UsageError(stdio, "base64", "option -w requires an argument")
			}
			n, err := parseNonNegInt(args[i])
			if err != nil {
				return core.UsageError(stdio, "base64", "invalid -w value")
			}
			wrap = n
			i++
		default:
			startIdx = i
			i = len(args)
		}
	}

	var in io.Reader = stdio.In
	if startIdx < len(args) && args[startIdx] != "-" {
		f, err := os.Open(args[startIdx])
		if err != nil {
			return core.FileError(stdio, "base64", args[startIdx], err)
		}
		defer f.Close()
		in = f
	}

	if decode {
		// Strip whitespace before decode — the stdlib decoder rejects newlines.
		raw, err := io.ReadAll(in)
		if err != nil {
			stdio.Errorf("base64: %v\n", err)
			return core.ExitFailure
		}
		clean := stripWhitespace(raw)
		out, err := base64.StdEncoding.DecodeString(string(clean))
		if err != nil {
			stdio.Errorf("base64: invalid input\n")
			return core.ExitFailure
		}
		stdio.Out.Write(out)
		return core.ExitSuccess
	}

	raw, err := io.ReadAll(in)
	if err != nil {
		stdio.Errorf("base64: %v\n", err)
		return core.ExitFailure
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if wrap == 0 {
		stdio.Println(encoded)
	} else {
		for j := 0; j < len(encoded); j += wrap {
			end := j + wrap
			if end > len(encoded) {
				end = len(encoded)
			}
			stdio.Println(encoded[j:end])
		}
	}
	return core.ExitSuccess
}

func parseNonNegInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func stripWhitespace(b []byte) []byte {
	out := b[:0]
	for _, c := range b {
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		out = append(out, c)
	}
	return out
}
