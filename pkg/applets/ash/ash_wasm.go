//go:build js || wasm || wasip1

// WASI-compatible ash shell implementation.
// Uses in-process applet dispatch via core.ExecCommand instead of os/exec.
// Supports: command execution, pipelines, redirections (>, >>, <, 2>),
// environment variables, builtins (cd, export, set, exit, etc.),
// command substitution ($(...)), and basic control flow.

package ash

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/rcarmo/go-busybox/pkg/core"
)

// Run executes the ash shell on WASI platforms.
func Run(stdio *core.Stdio, args []string) int {
	shell := &wasiShell{
		stdio:    stdio,
		vars:     map[string]string{},
		exported: map[string]bool{},
		lastExit: 0,
	}

	// Import environment
	for _, entry := range os.Environ() {
		if eq := strings.Index(entry, "="); eq > 0 {
			name := entry[:eq]
			val := entry[eq+1:]
			shell.vars[name] = val
			shell.exported[name] = true
		}
	}

	// Set defaults
	if shell.vars["PS1"] == "" {
		shell.vars["PS1"] = "$ "
	}
	if shell.vars["HOME"] == "" {
		shell.vars["HOME"] = "/"
	}
	shell.vars["?"] = "0"

	// Handle -c flag
	if len(args) >= 2 && args[0] == "-c" {
		return shell.execString(strings.Join(args[1:], " "))
	}

	// Handle script file
	if len(args) >= 1 && args[0] != "-c" && !strings.HasPrefix(args[0], "-") {
		data, err := os.ReadFile(args[0])
		if err != nil {
			stdio.Errorf("ash: %v\n", err)
			return core.ExitFailure
		}
		return shell.execString(string(data))
	}

	// Interactive mode
	return shell.interactive()
}

type wasiShell struct {
	stdio    *core.Stdio
	vars     map[string]string
	exported map[string]bool
	lastExit int
}

func (s *wasiShell) interactive() int {
	scanner := bufio.NewScanner(s.stdio.In)
	s.printPrompt()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			s.printPrompt()
			continue
		}
		s.execLine(line)
		s.printPrompt()
	}
	return s.lastExit
}

func (s *wasiShell) execString(script string) int {
	lines := strings.Split(script, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s.execLine(line)
	}
	return s.lastExit
}

func (s *wasiShell) printPrompt() {
	prompt := s.expandVars(s.vars["PS1"])
	fmt.Fprint(s.stdio.Out, prompt)
}

func (s *wasiShell) execLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}

	// Handle && and || chains
	if containsUnquoted(line, "&&") || containsUnquoted(line, "||") {
		s.execChain(line)
		return
	}

	// Handle ; separated commands
	if containsUnquoted(line, ";") {
		for _, part := range splitUnquoted(line, ';') {
			part = strings.TrimSpace(part)
			if part != "" {
				s.execSingle(part)
			}
		}
		return
	}

	s.execSingle(line)
}

func (s *wasiShell) execChain(line string) {
	parts := tokenizeChain(line)
	for _, part := range parts {
		cmd := strings.TrimSpace(part.cmd)
		if cmd == "" {
			continue
		}
		if part.op == "&&" && s.lastExit != 0 {
			continue
		}
		if part.op == "||" && s.lastExit == 0 {
			continue
		}
		s.execSingle(cmd)
	}
}

type chainPart struct {
	cmd string
	op  string
}

func tokenizeChain(line string) []chainPart {
	var parts []chainPart
	current := ""
	op := ""
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
		}
		if !inSingle && !inDouble && i+1 < len(line) {
			if line[i:i+2] == "&&" {
				parts = append(parts, chainPart{cmd: current, op: op})
				current = ""
				op = "&&"
				i++
				continue
			}
			if line[i:i+2] == "||" {
				parts = append(parts, chainPart{cmd: current, op: op})
				current = ""
				op = "||"
				i++
				continue
			}
		}
		current += string(ch)
	}
	parts = append(parts, chainPart{cmd: current, op: op})
	return parts
}

func (s *wasiShell) execSingle(line string) {
	// Expand variables
	line = s.expandVars(line)

	// Handle command substitution $(...) — simple single-level
	line = s.expandCommandSubstitution(line)

	// Check for pipeline
	if containsUnquoted(line, "|") {
		s.execPipeline(line)
		return
	}

	s.execSimple(line)
}

// execSimple runs a single command (no pipes).
func (s *wasiShell) execSimple(line string) {
	// Parse redirections
	args, stdinFile, stdoutFile, stdoutAppend, stderrRedir := parseRedirections(line)
	if len(args) == 0 {
		return
	}

	// Handle variable assignment: VAR=value
	if len(args) == 1 && strings.Contains(args[0], "=") && !strings.HasPrefix(args[0], "=") {
		eq := strings.Index(args[0], "=")
		s.vars[args[0][:eq]] = args[0][eq+1:]
		s.setExit(0)
		return
	}

	// Set up I/O
	stdin := s.stdio.In
	stdout := s.stdio.Out
	stderr := s.stdio.Err

	var closers []io.Closer

	if stdinFile != "" {
		f, err := os.Open(stdinFile)
		if err != nil {
			s.stdio.Errorf("ash: %v\n", err)
			s.setExit(1)
			return
		}
		closers = append(closers, f)
		stdin = f
	}

	if stdoutFile != "" {
		flag := os.O_WRONLY | os.O_CREATE
		if stdoutAppend {
			flag |= os.O_APPEND
		} else {
			flag |= os.O_TRUNC
		}
		f, err := os.OpenFile(stdoutFile, flag, 0644)
		if err != nil {
			s.stdio.Errorf("ash: %v\n", err)
			s.setExit(1)
			for _, c := range closers {
				c.Close()
			}
			return
		}
		closers = append(closers, f)
		stdout = f
		if stderrRedir == "&1" {
			stderr = f
		}
	}

	if stderrRedir != "" && stderrRedir != "&1" {
		f, err := os.OpenFile(stderrRedir, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			s.stdio.Errorf("ash: %v\n", err)
			s.setExit(1)
			for _, c := range closers {
				c.Close()
			}
			return
		}
		closers = append(closers, f)
		stderr = f
	}

	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()

	// Try builtin first
	if s.tryBuiltin(args, stdin, stdout, stderr) {
		return
	}

	// Execute via core executor (applet registry)
	exitCode := core.ExecCommand(args[0], args[1:], stdin, stdout, stderr, s.buildEnv())
	s.setExit(exitCode)
}

// runCommand runs a single command (possibly a builtin) with given I/O.
// Used by pipeline stages.
func (s *wasiShell) runCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return 0
	}

	// Check builtins (only the ones that make sense in a pipeline)
	switch args[0] {
	case "echo":
		s.builtinEcho(args[1:], stdout)
		return s.lastExit
	case "set":
		s.builtinSet(stdout)
		return s.lastExit
	case "env":
		for _, e := range s.buildEnv() {
			fmt.Fprintln(stdout, e)
		}
		return 0
	case "pwd":
		dir, _ := os.Getwd()
		fmt.Fprintln(stdout, dir)
		return 0
	case "true":
		return 0
	case "false":
		return 1
	case "type":
		s.builtinType(args[1:], stdout)
		return s.lastExit
	case "read":
		s.builtinRead(args[1:], stdin)
		return s.lastExit
	case "cat":
		// cat without args reads from stdin (useful in pipes)
		if len(args) == 1 {
			io.Copy(stdout, stdin)
			return 0
		}
	}

	// Execute via core executor
	return core.ExecCommand(args[0], args[1:], stdin, stdout, stderr, s.buildEnv())
}

func (s *wasiShell) execPipeline(line string) {
	segments := splitUnquoted(line, '|')
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		s.execSimple(segments[0])
		return
	}

	// Parse all stages
	type stage struct {
		args []string
	}
	var stages []stage
	for _, seg := range segments {
		seg = strings.TrimSpace(s.expandVars(seg))
		args, _, _, _, _ := parseRedirections(seg)
		stages = append(stages, stage{args: args})
	}

	// Execute pipeline: each stage runs in a goroutine.
	// io.Pipe connects them. We close write ends promptly.
	n := len(stages)
	pipes := make([]*io.PipeWriter, n-1)
	readers := make([]*io.PipeReader, n-1)
	for i := 0; i < n-1; i++ {
		r, w := io.Pipe()
		readers[i] = r
		pipes[i] = w
	}

	var wg sync.WaitGroup
	exitCodes := make([]int, n)

	for i, st := range stages {
		var stageIn io.Reader
		var stageOut io.Writer

		if i == 0 {
			stageIn = s.stdio.In
		} else {
			stageIn = readers[i-1]
		}

		if i == n-1 {
			stageOut = s.stdio.Out
		} else {
			stageOut = pipes[i]
		}

		wg.Add(1)
		go func(idx int, args []string, in io.Reader, out io.Writer) {
			defer wg.Done()
			exitCodes[idx] = s.runCommand(args, in, out, s.stdio.Err)
			// Close the write end of our pipe so the next stage gets EOF
			if idx < n-1 {
				pipes[idx].Close()
			}
			// Close the read end we consumed
			if idx > 0 {
				readers[idx-1].Close()
			}
		}(i, st.args, stageIn, stageOut)
	}

	wg.Wait()
	s.setExit(exitCodes[n-1])
}

// MARK: - Builtins

func (s *wasiShell) tryBuiltin(args []string, stdin io.Reader, stdout, stderr io.Writer) bool {
	switch args[0] {
	case "cd":
		s.builtinCd(args[1:])
	case "pwd":
		dir, _ := os.Getwd()
		fmt.Fprintln(stdout, dir)
		s.setExit(0)
	case "export":
		s.builtinExport(args[1:])
	case "unset":
		for _, name := range args[1:] {
			delete(s.vars, name)
			delete(s.exported, name)
		}
		s.setExit(0)
	case "set":
		s.builtinSet(stdout)
	case "env":
		for _, e := range s.buildEnv() {
			fmt.Fprintln(stdout, e)
		}
		s.setExit(0)
	case "exit":
		code := 0
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &code)
		}
		os.Exit(code)
	case "true":
		s.setExit(0)
	case "false":
		s.setExit(1)
	case "echo":
		s.builtinEcho(args[1:], stdout)
	case "test", "[":
		s.setExit(s.builtinTest(args))
	case "type":
		s.builtinType(args[1:], stdout)
	case "read":
		s.builtinRead(args[1:], stdin)
	case "source", ".":
		if len(args) < 2 {
			s.stdio.Errorf("ash: source: missing filename\n")
			s.setExit(1)
		} else {
			data, err := os.ReadFile(args[1])
			if err != nil {
				s.stdio.Errorf("ash: source: %v\n", err)
				s.setExit(1)
			} else {
				s.execString(string(data))
			}
		}
	default:
		return false
	}
	return true
}

func (s *wasiShell) builtinCd(args []string) {
	dir := s.vars["HOME"]
	if len(args) > 0 {
		dir = args[0]
		if dir == "-" {
			dir = s.vars["OLDPWD"]
			if dir == "" {
				s.stdio.Errorf("ash: cd: OLDPWD not set\n")
				s.setExit(1)
				return
			}
		}
	}
	oldDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		s.stdio.Errorf("ash: cd: %v\n", err)
		s.setExit(1)
		return
	}
	s.vars["OLDPWD"] = oldDir
	newDir, _ := os.Getwd()
	s.vars["PWD"] = newDir
	s.setExit(0)
}

func (s *wasiShell) builtinExport(args []string) {
	for _, arg := range args {
		if eq := strings.Index(arg, "="); eq > 0 {
			name := arg[:eq]
			val := arg[eq+1:]
			s.vars[name] = val
			s.exported[name] = true
			os.Setenv(name, val)
		} else {
			s.exported[arg] = true
			if val, ok := s.vars[arg]; ok {
				os.Setenv(arg, val)
			}
		}
	}
	s.setExit(0)
}

func (s *wasiShell) builtinSet(stdout io.Writer) {
	keys := make([]string, 0, len(s.vars))
	for k := range s.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(stdout, "%s='%s'\n", k, s.vars[k])
	}
	s.setExit(0)
}

func (s *wasiShell) builtinEcho(args []string, stdout io.Writer) {
	noNewline := false
	interpretEscapes := false
	startIdx := 0
	for startIdx < len(args) {
		if args[startIdx] == "-n" {
			noNewline = true
			startIdx++
		} else if args[startIdx] == "-e" {
			interpretEscapes = true
			startIdx++
		} else if args[startIdx] == "-ne" || args[startIdx] == "-en" {
			noNewline = true
			interpretEscapes = true
			startIdx++
		} else {
			break
		}
	}
	output := strings.Join(args[startIdx:], " ")
	if interpretEscapes {
		output = interpretEscapeSequences(output)
	}
	fmt.Fprint(stdout, output)
	if !noNewline {
		fmt.Fprintln(stdout)
	}
	s.setExit(0)
}

func interpretEscapeSequences(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			case '\\':
				buf.WriteByte('\\')
				i += 2
			case '0':
				buf.WriteByte(0)
				i += 2
			default:
				buf.WriteByte(s[i])
				i++
			}
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}
	return buf.String()
}

func (s *wasiShell) builtinTest(args []string) int {
	if args[0] == "[" {
		if len(args) < 2 || args[len(args)-1] != "]" {
			return 2
		}
		args = args[1 : len(args)-1]
	} else {
		args = args[1:]
	}

	if len(args) == 0 {
		return 1
	}

	// Unary tests
	if len(args) == 2 {
		switch args[0] {
		case "-n":
			if args[1] != "" {
				return 0
			}
			return 1
		case "-z":
			if args[1] == "" {
				return 0
			}
			return 1
		case "-f":
			info, err := os.Stat(args[1])
			if err == nil && !info.IsDir() {
				return 0
			}
			return 1
		case "-d":
			info, err := os.Stat(args[1])
			if err == nil && info.IsDir() {
				return 0
			}
			return 1
		case "-e", "-r", "-w", "-x":
			_, err := os.Stat(args[1])
			if err == nil {
				return 0
			}
			return 1
		case "-s":
			info, err := os.Stat(args[1])
			if err == nil && info.Size() > 0 {
				return 0
			}
			return 1
		case "!":
			// Negate single test
			sub := s.builtinTest(append([]string{"test"}, args[1:]...))
			if sub == 0 {
				return 1
			}
			return 0
		}
	}

	// Binary tests
	if len(args) == 3 {
		switch args[1] {
		case "=", "==":
			if args[0] == args[2] {
				return 0
			}
			return 1
		case "!=":
			if args[0] != args[2] {
				return 0
			}
			return 1
		case "-eq":
			if args[0] == args[2] {
				return 0
			}
			return 1
		case "-ne":
			if args[0] != args[2] {
				return 0
			}
			return 1
		}
	}

	// Single arg: true if non-empty
	if len(args) == 1 && args[0] != "" {
		return 0
	}
	return 1
}

func (s *wasiShell) builtinType(args []string, stdout io.Writer) {
	builtins := map[string]bool{
		"cd": true, "pwd": true, "export": true, "unset": true, "set": true,
		"env": true, "exit": true, "true": true, "false": true, "echo": true,
		"test": true, "[": true, "type": true, "read": true, "source": true, ".": true,
	}
	for _, name := range args {
		if builtins[name] {
			fmt.Fprintf(stdout, "%s is a shell builtin\n", name)
		} else if core.IsExecutable(name) {
			fmt.Fprintf(stdout, "%s is a busybox applet\n", name)
		} else {
			fmt.Fprintf(stdout, "ash: type: %s: not found\n", name)
			s.setExit(1)
			return
		}
	}
	s.setExit(0)
}

func (s *wasiShell) builtinRead(args []string, stdin io.Reader) {
	if len(args) == 0 {
		args = []string{"REPLY"}
	}
	reader := bufio.NewReader(stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, "\n\r")

	if len(args) == 1 {
		s.vars[args[0]] = line
	} else {
		fields := strings.Fields(line)
		for i, name := range args {
			if i < len(fields) {
				if i == len(args)-1 {
					s.vars[name] = strings.Join(fields[i:], " ")
				} else {
					s.vars[name] = fields[i]
				}
			} else {
				s.vars[name] = ""
			}
		}
	}
	s.setExit(0)
}

// MARK: - Variable Expansion

func (s *wasiShell) expandVars(input string) string {
	var result strings.Builder
	i := 0
	for i < len(input) {
		if input[i] == '\'' {
			end := strings.Index(input[i+1:], "'")
			if end < 0 {
				result.WriteString(input[i:])
				break
			}
			result.WriteString(input[i : i+2+end])
			i += 2 + end
		} else if input[i] == '$' {
			if i+1 < len(input) && input[i+1] == '{' {
				end := strings.Index(input[i:], "}")
				if end < 0 {
					result.WriteByte(input[i])
					i++
					continue
				}
				expr := input[i+2 : i+end]
				result.WriteString(s.expandParameter(expr))
				i += end + 1
			} else if i+1 < len(input) && input[i+1] == '(' {
				// $(...) — handled by expandCommandSubstitution
				result.WriteByte(input[i])
				i++
			} else if i+1 < len(input) && (input[i+1] == '?' || isVarChar(input[i+1])) {
				j := i + 1
				if input[j] == '?' {
					j++
				} else {
					for j < len(input) && isVarChar(input[j]) {
						j++
					}
				}
				name := input[i+1 : j]
				result.WriteString(s.vars[name])
				i = j
			} else {
				result.WriteByte(input[i])
				i++
			}
		} else if input[i] == '~' && (i == 0 || input[i-1] == ' ' || input[i-1] == '=') {
			if i+1 >= len(input) || input[i+1] == '/' || input[i+1] == ' ' {
				result.WriteString(s.vars["HOME"])
				i++
			} else {
				result.WriteByte(input[i])
				i++
			}
		} else {
			result.WriteByte(input[i])
			i++
		}
	}
	return result.String()
}

func (s *wasiShell) expandParameter(expr string) string {
	if idx := strings.Index(expr, ":-"); idx > 0 {
		name := expr[:idx]
		def := expr[idx+2:]
		if val, ok := s.vars[name]; ok && val != "" {
			return val
		}
		return def
	}
	if idx := strings.Index(expr, ":="); idx > 0 {
		name := expr[:idx]
		def := expr[idx+2:]
		if val, ok := s.vars[name]; ok && val != "" {
			return val
		}
		s.vars[name] = def
		return def
	}
	return s.vars[expr]
}

func (s *wasiShell) expandCommandSubstitution(input string) string {
	for {
		start := strings.Index(input, "$(")
		if start < 0 {
			break
		}
		depth := 1
		end := start + 2
		for end < len(input) && depth > 0 {
			if input[end] == '(' {
				depth++
			} else if input[end] == ')' {
				depth--
			}
			end++
		}
		if depth != 0 {
			break
		}
		cmd := input[start+2 : end-1]
		var buf bytes.Buffer
		subStdio := &core.Stdio{In: s.stdio.In, Out: &buf, Err: s.stdio.Err}
		subShell := &wasiShell{stdio: subStdio, vars: s.vars, exported: s.exported}
		subShell.execLine(cmd)
		output := strings.TrimRight(buf.String(), "\n")
		input = input[:start] + output + input[end:]
	}
	return input
}

// MARK: - Parsing Helpers

func parseRedirections(line string) (args []string, stdinFile, stdoutFile string, stdoutAppend bool, stderrRedir string) {
	tokens := shellSplit(line)
	var cleanArgs []string
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok == "<" && i+1 < len(tokens):
			stdinFile = tokens[i+1]
			i++
		case tok == ">>" && i+1 < len(tokens):
			stdoutFile = tokens[i+1]
			stdoutAppend = true
			i++
		case tok == ">" && i+1 < len(tokens):
			stdoutFile = tokens[i+1]
			stdoutAppend = false
			i++
		case tok == "2>&1":
			stderrRedir = "&1"
		case tok == "2>" && i+1 < len(tokens):
			stderrRedir = tokens[i+1]
			i++
		case strings.HasPrefix(tok, ">>"):
			stdoutFile = tok[2:]
			stdoutAppend = true
		case strings.HasPrefix(tok, ">") && len(tok) > 1:
			stdoutFile = tok[1:]
			stdoutAppend = false
		default:
			cleanArgs = append(cleanArgs, tok)
		}
	}
	return cleanArgs, stdinFile, stdoutFile, stdoutAppend, stderrRedir
}

// shellSplit splits a command line into tokens, respecting quotes.
func shellSplit(line string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := rune(line[i])
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			if inDouble {
				// In double quotes, only \\ \" \$ \` are special
				if i+1 < len(line) {
					next := line[i+1]
					if next == '\\' || next == '"' || next == '$' || next == '`' {
						escaped = true
						continue
					}
				}
				// Preserve the backslash for other sequences (e.g. \n \t)
				current.WriteRune(ch)
				continue
			}
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if ch == ' ' && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	// Expand globs
	var expanded []string
	for _, tok := range tokens {
		if strings.ContainsAny(tok, "*?") {
			matches, err := filepath.Glob(tok)
			if err == nil && len(matches) > 0 {
				expanded = append(expanded, matches...)
				continue
			}
		}
		expanded = append(expanded, tok)
	}
	return expanded
}

// containsUnquoted checks if `sub` appears outside of quotes in `line`.
func containsUnquoted(line, sub string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		if line[i] == '\'' && !inDouble {
			inSingle = !inSingle
		} else if line[i] == '"' && !inSingle {
			inDouble = !inDouble
		}
		if !inSingle && !inDouble && i+len(sub) <= len(line) && line[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// splitUnquoted splits on a delimiter character, respecting quotes.
func splitUnquoted(line string, delim byte) []string {
	var parts []string
	var current strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
		}
		if ch == delim && !inSingle && !inDouble {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func isVarChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func (s *wasiShell) setExit(code int) {
	s.lastExit = code
	s.vars["?"] = fmt.Sprintf("%d", code)
}

func (s *wasiShell) buildEnv() []string {
	var env []string
	for name := range s.exported {
		if val, ok := s.vars[name]; ok {
			env = append(env, name+"="+val)
		}
	}
	return env
}
