// Package sha256sum implements the sha256sum command — print SHA-256
// checksums of stdin or named files.
package sha256sum

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run prints `<hex-digest>  <filename>` for each input. With no files
// it reads stdin and prints `<hex-digest>  -`.
func Run(stdio *core.Stdio, args []string) int {
	if len(args) == 0 {
		sum, err := hashReader(stdio.In)
		if err != nil {
			stdio.Errorf("sha256sum: %v\n", err)
			return core.ExitFailure
		}
		stdio.Printf("%s  -\n", sum)
		return core.ExitSuccess
	}

	exit := core.ExitSuccess
	for _, path := range args {
		f, err := os.Open(path)
		if err != nil {
			exit = core.FileError(stdio, "sha256sum", path, err)
			continue
		}
		sum, err := hashReader(f)
		f.Close()
		if err != nil {
			exit = core.FileError(stdio, "sha256sum", path, err)
			continue
		}
		stdio.Printf("%s  %s\n", sum, path)
	}
	return exit
}

func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
