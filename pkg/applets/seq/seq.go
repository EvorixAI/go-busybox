// Package seq implements the seq command — print a sequence of
// numbers, one per line.
package seq

import (
	"strconv"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run prints integers from FIRST to LAST stepping by INCREMENT.
//
// Forms supported:
//
//	seq LAST                  → 1 .. LAST step 1
//	seq FIRST LAST            → FIRST .. LAST step 1
//	seq FIRST INCREMENT LAST  → FIRST .. LAST step INCREMENT
//
// Float arguments and -s/-w/-f flags are not supported in this minimal
// implementation; reach for awk if floats matter.
func Run(stdio *core.Stdio, args []string) int {
	first := 1
	step := 1
	last := 0

	switch len(args) {
	case 1:
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return core.UsageError(stdio, "seq", "argument must be an integer")
		}
		last = n
	case 2:
		a, err1 := strconv.Atoi(args[0])
		b, err2 := strconv.Atoi(args[1])
		if err1 != nil || err2 != nil {
			return core.UsageError(stdio, "seq", "arguments must be integers")
		}
		first, last = a, b
	case 3:
		a, err1 := strconv.Atoi(args[0])
		s, err2 := strconv.Atoi(args[1])
		b, err3 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return core.UsageError(stdio, "seq", "arguments must be integers")
		}
		if s == 0 {
			return core.UsageError(stdio, "seq", "increment must be non-zero")
		}
		first, step, last = a, s, b
	default:
		return core.UsageError(stdio, "seq", "expected 1, 2, or 3 arguments")
	}

	if step > 0 {
		for i := first; i <= last; i += step {
			stdio.Println(i)
		}
	} else {
		for i := first; i >= last; i += step {
			stdio.Println(i)
		}
	}
	return core.ExitSuccess
}
