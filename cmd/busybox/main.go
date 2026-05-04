// Command busybox is a multi-call binary that combines many common UNIX
// utilities into a single executable. The applet to run is selected by
// the name used to invoke the binary (via symlink) or by passing the
// applet name as the first argument.
package main

import (
	"os"
	"path/filepath"

	"github.com/rcarmo/go-busybox/pkg/applets/ash"
	"github.com/rcarmo/go-busybox/pkg/applets/awk"
	"github.com/rcarmo/go-busybox/pkg/applets/base64"
	"github.com/rcarmo/go-busybox/pkg/applets/basename"
	"github.com/rcarmo/go-busybox/pkg/applets/cat"
	"github.com/rcarmo/go-busybox/pkg/applets/chmod"
	"github.com/rcarmo/go-busybox/pkg/applets/cp"
	"github.com/rcarmo/go-busybox/pkg/applets/cut"
	"github.com/rcarmo/go-busybox/pkg/applets/diff"
	"github.com/rcarmo/go-busybox/pkg/applets/dig"
	"github.com/rcarmo/go-busybox/pkg/applets/dirname"
	"github.com/rcarmo/go-busybox/pkg/applets/echo"
	"github.com/rcarmo/go-busybox/pkg/applets/find"
	"github.com/rcarmo/go-busybox/pkg/applets/free"
	"github.com/rcarmo/go-busybox/pkg/applets/grep"
	"github.com/rcarmo/go-busybox/pkg/applets/gunzip"
	"github.com/rcarmo/go-busybox/pkg/applets/gzip"
	"github.com/rcarmo/go-busybox/pkg/applets/head"
	"github.com/rcarmo/go-busybox/pkg/applets/ionice"
	"github.com/rcarmo/go-busybox/pkg/applets/jq"
	"github.com/rcarmo/go-busybox/pkg/applets/kill"
	"github.com/rcarmo/go-busybox/pkg/applets/killall"
	"github.com/rcarmo/go-busybox/pkg/applets/logname"
	"github.com/rcarmo/go-busybox/pkg/applets/ls"
	"github.com/rcarmo/go-busybox/pkg/applets/lua"
	"github.com/rcarmo/go-busybox/pkg/applets/md5sum"
	"github.com/rcarmo/go-busybox/pkg/applets/mkdir"
	"github.com/rcarmo/go-busybox/pkg/applets/mv"
	"github.com/rcarmo/go-busybox/pkg/applets/nc"
	"github.com/rcarmo/go-busybox/pkg/applets/nice"
	"github.com/rcarmo/go-busybox/pkg/applets/nohup"
	"github.com/rcarmo/go-busybox/pkg/applets/nproc"
	"github.com/rcarmo/go-busybox/pkg/applets/pgrep"
	"github.com/rcarmo/go-busybox/pkg/applets/pidof"
	"github.com/rcarmo/go-busybox/pkg/applets/printf"
	"github.com/rcarmo/go-busybox/pkg/applets/pkill"
	"github.com/rcarmo/go-busybox/pkg/applets/ps"
	"github.com/rcarmo/go-busybox/pkg/applets/pwd"
	"github.com/rcarmo/go-busybox/pkg/applets/qjs"
	"github.com/rcarmo/go-busybox/pkg/applets/renice"
	"github.com/rcarmo/go-busybox/pkg/applets/rm"
	"github.com/rcarmo/go-busybox/pkg/applets/rmdir"
	"github.com/rcarmo/go-busybox/pkg/applets/sed"
	"github.com/rcarmo/go-busybox/pkg/applets/seq"
	"github.com/rcarmo/go-busybox/pkg/applets/setsid"
	"github.com/rcarmo/go-busybox/pkg/applets/sha256sum"
	"github.com/rcarmo/go-busybox/pkg/applets/sleep"
	"github.com/rcarmo/go-busybox/pkg/applets/sort"
	"github.com/rcarmo/go-busybox/pkg/applets/ss"
	"github.com/rcarmo/go-busybox/pkg/applets/startstopdaemon"
	"github.com/rcarmo/go-busybox/pkg/applets/tail"
	"github.com/rcarmo/go-busybox/pkg/applets/tar"
	"github.com/rcarmo/go-busybox/pkg/applets/taskset"
	"github.com/rcarmo/go-busybox/pkg/applets/tee"
	"github.com/rcarmo/go-busybox/pkg/applets/time"
	"github.com/rcarmo/go-busybox/pkg/applets/timeout"
	"github.com/rcarmo/go-busybox/pkg/applets/top"
	"github.com/rcarmo/go-busybox/pkg/applets/touch"
	"github.com/rcarmo/go-busybox/pkg/applets/tr"
	"github.com/rcarmo/go-busybox/pkg/applets/uniq"
	"github.com/rcarmo/go-busybox/pkg/applets/uptime"
	"github.com/rcarmo/go-busybox/pkg/applets/users"
	"github.com/rcarmo/go-busybox/pkg/applets/w"
	"github.com/rcarmo/go-busybox/pkg/applets/watch"
	"github.com/rcarmo/go-busybox/pkg/applets/wc"
	"github.com/rcarmo/go-busybox/pkg/applets/wget"
	"github.com/rcarmo/go-busybox/pkg/applets/who"
	"github.com/rcarmo/go-busybox/pkg/applets/whoami"
	"github.com/rcarmo/go-busybox/pkg/applets/xargs"
	"github.com/rcarmo/go-busybox/pkg/applets/yes"
	"github.com/rcarmo/go-busybox/pkg/applets/yq"
	"github.com/rcarmo/go-busybox/pkg/core"
)

var applets = map[string]core.AppletFunc{
	"echo":              echo.Run,
	"ash":               ash.Run,
	"sh":                ash.Run,
	"awk":               awk.Run,
	"cat":               cat.Run,
	"ls":                ls.Run,
	"cp":                cp.Run,
	"mv":                mv.Run,
	"free":              free.Run,
	"pidof":             pidof.Run,
	"printf":            printf.Run,
	"pgrep":             pgrep.Run,
	"pkill":             pkill.Run,
	"logname":           logname.Run,
	"nice":              nice.Run,
	"nproc":             nproc.Run,
	"rm":                rm.Run,
	"rmdir":             rmdir.Run,
	"head":              head.Run,
	"kill":              kill.Run,
	"killall":           killall.Run,
	"tail":              tail.Run,
	"wc":                wc.Run,
	"find":              find.Run,
	"sort":              sort.Run,
	"mkdir":             mkdir.Run,
	"pwd":               pwd.Run,
	"renice":            renice.Run,
	"uniq":              uniq.Run,
	"cut":               cut.Run,
	"grep":              grep.Run,
	"sed":               sed.Run,
	"tr":                tr.Run,
	"diff":              diff.Run,
	"ps":                ps.Run,
	"ss":                ss.Run,
	"dig":               dig.Run,
	"gzip":              gzip.Run,
	"gunzip":            gunzip.Run,
	"tar":               tar.Run,
	"sleep":             sleep.Run,
	"uptime":            uptime.Run,
	"whoami":            whoami.Run,
	"who":               who.Run,
	"users":             users.Run,
	"w":                 w.Run,
	"top":               top.Run,
	"time":              time.Run,
	"timeout":           timeout.Run,
	"setsid":            setsid.Run,
	"nohup":             nohup.Run,
	"watch":             watch.Run,
	"taskset":           taskset.Run,
	"ionice":            ionice.Run,
	"xargs":             xargs.Run,
	"start-stop-daemon": startstopdaemon.Run,
	"wget":              wget.Run,
	"nc":                nc.Run,
	// Tier 1 gap fill — Evorix additions for the WASI agent scripting
	// toolkit, 2026-05-04. Pure-Go stdlib-only applets (no new deps).
	"touch":     touch.Run,
	"chmod":     chmod.Run,
	"base64":    base64.Run,
	"md5sum":    md5sum.Run,
	"sha256sum": sha256sum.Run,
	"basename":  basename.Run,
	"dirname":   dirname.Run,
	"tee":       tee.Run,
	"seq":       seq.Run,
	"yes":       yes.Run,
	// Tier 2 — Evorix additions for the WASI agent scripting toolkit,
	// 2026-05-04. Pure-Go wrappers around upstream libraries:
	//   jq  → github.com/itchyny/gojq
	//   qjs → github.com/dop251/goja (ES2020 interpreter)
	//   lua → github.com/yuin/gopher-lua (Lua 5.1)
	//   yq  → gopkg.in/yaml.v3 + gojq (jq-syntax over YAML)
	//
	// sqlite3 deferred: modernc.org/sqlite doesn't compile for wasip1
	// (libc shim excludes pthread/signal/stdio/time/unistd). Pure-Go
	// SQLite with WASI support doesn't exist today. Revisit when
	// either modernc/sqlite gains WASI or an alternative emerges.
	"jq":  jq.Run,
	"qjs": qjs.Run,
	"lua": lua.Run,
	"yq":  yq.Run,
}

func init() {
	// Register all applets in the core registry so ash can dispatch them in-process.
	for name, run := range applets {
		core.RegisterApplet(name, run)
	}
}

func main() {
	stdio := core.DefaultStdio()

	applet, args := resolveApplet(os.Args)
	if applet == "" {
		printAppletList(stdio)
		os.Exit(core.ExitUsage)
	}

	run, ok := applets[applet]
	if !ok {
		stdio.Errorf("busybox: applet not found: %s\n", applet)
		printAppletList(stdio)
		os.Exit(core.ExitUsage)
	}

	// Applets expect args without the applet name.
	os.Exit(run(stdio, args))
}

func resolveApplet(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}

	// If invoked as "busybox applet ..."
	if len(args) > 1 && filepath.Base(args[0]) == "busybox" {
		return args[1], args[2:]
	}

	// If invoked as a symlink named after the applet
	applet := filepath.Base(args[0])
	return applet, args[1:]
}

func usage(stdio *core.Stdio) {
	printAppletList(stdio)
}

func printAppletList(stdio *core.Stdio) {
	stdio.Println("Currently defined functions:")
	for name := range applets {
		stdio.Print(" ", name)
	}
	stdio.Println()
}
