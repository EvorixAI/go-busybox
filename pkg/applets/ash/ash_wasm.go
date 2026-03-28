//go:build js || wasm || wasip1

// WASI-compatible ash shell implementation.
// Uses in-process applet dispatch via core.ExecCommand instead of os/exec.
// Supports: command execution, pipelines, redirections (>, >>, <),
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
	if strings.Contains(line, "&&") || strings.Contains(line, "||") {
		s.execChain(line)
		return
	}

	// Handle ; separated commands
	if strings.Contains(line, ";") && !strings.Contains(line, "'") {
		for _, part := range splitOnSemicolon(line) {
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
	// Simple && / || chain handling
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
	op  string // "" for first, "&&" or "||"
}

func tokenizeChain(line string) []chainPart {
	var parts []chainPart
	current := ""
	op := ""
	for i := 0; i < len(line); i++ {
		if i+1 < len(line) && line[i:i+2] == "&&" {
			parts = append(parts, chainPart{cmd: current, op: op})
			current = ""
			op = "&&"
			i++
		} else if i+1 < len(line) && line[i:i+2] == "||" {
			parts = append(parts, chainPart{cmd: current, op: op})
			current = ""
			op = "||"
			i++
		} else {
			current += string(line[i])
		}
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
	if strings.Contains(line, "|") && !isQuoted(line, strings.Index(line, "|")) {
		s.execPipeline(line)
		return
	}

	// Parse redirections
	args, stdinFile, stdoutFile, stdoutAppend, stderrFile := parseRedirections(line)
	if len(args) == 0 {
		return
	}

	// Handle variable assignment: VAR=value
	if len(args) == 1 && strings.Contains(args[0], "=") && !strings.HasPrefix(args[0], "=") {
		eq := strings.Index(args[0], "=")
		name := args[0][:eq]
		val := args[0][eq+1:]
		s.vars[name] = val
		s.lastExit = 0
		s.vars["?"] = "0"
		return
	}

	// Set up I/O
	stdin := s.stdio.In
	stdout := s.stdio.Out
	stderr := s.stdio.Err

	if stdinFile != "" {
		f, err := os.Open(stdinFile)
		if err != nil {
			s.stdio.Errorf("ash: %v\n", err)
			s.setExit(1)
			return
		}
		defer f.Close()
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
			return
		}
		defer f.Close()
		stdout = f
	}

	if stderrFile != "" {
		f, err := os.OpenFile(stderrFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			s.stdio.Errorf("ash: %v\n", err)
			s.setExit(1)
			return
		}
		defer f.Close()
		stderr = f
	}

	// Try builtin first
	if s.tryBuiltin(args, stdin, stdout, stderr) {
		return
	}

	// Execute via core executor (applet registry)
	env := s.buildEnv()
	exitCode := core.ExecCommand(args[0], args[1:], stdin, stdout, stderr, env)
	s.setExit(exitCode)
}

func (s *wasiShell) execPipeline(line string) {
	segments := splitPipeline(line)
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		s.execSingle(segments[0])
		return
	}

	// Build pipeline with io.Pipe between each stage
	var prevReader io.Reader = s.stdio.In
	env := s.buildEnv()

	type pipeStage struct {
		done chan int
		pw   *io.PipeWriter
	}
	stages := make([]pipeStage, len(segments))

	for i, seg := range segments {
		seg = strings.TrimSpace(seg)
		args, _, _, _, _ := parseRedirections(s.expandVars(seg))
		if len(args) == 0 {
			continue
		}

		var stdout io.Writer
		var pw *io.PipeWriter
		if i < len(segments)-1 {
			pr, pipeW := io.Pipe()
			pw = pipeW
			stdout = pipeW
			// Next stage reads from this pipe
			stageReader := prevReader
			prevReader = pr
			_ = stageReader // used as stdin for this stage below
		} else {
			stdout = s.stdio.Out
		}

		stageStdin := prevReader
		if i > 0 {
			// For stages after the first, stdin comes from the previous pipe
			stageStdin = prevReader
		} else {
			stageStdin = s.stdio.In
		}

		// Fix: for stage i, stdin is the reader from previous stage
		if i == 0 {
			stageStdin = s.stdio.In
		}

		_, done := core.StartCommandAsync(args[0], args[1:], stageStdin, stdout, s.stdio.Err, env)
		stages[i] = pipeStage{done: done, pw: pw}

		if i < len(segments)-1 {
			// After starting this stage, the next stage's stdin is the pipe reader
			// prevReader was already set above
		}
	}

	// Wait for all stages, close pipes as each completes
	for i, stage := range stages {
		if stage.done != nil {
			exitCode := <-stage.done
			if i == len(segments)-1 {
				s.setExit(exitCode)
			}
		}
		if stage.pw != nil {
			stage.pw.Close()
		}
	}
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
	startIdx := 0
	if len(args) > 0 && args[0] == "-n" {
		noNewline = true
		startIdx = 1
	}
	output := strings.Join(args[startIdx:], " ")
	// Handle \n, \t escape sequences
	output = strings.ReplaceAll(output, "\\n", "\n")
	output = strings.ReplaceAll(output, "\\t", "\t")
	fmt.Fprint(stdout, output)
	if !noNewline {
		fmt.Fprintln(stdout)
	}
	s.setExit(0)
}

func (s *wasiShell) builtinTest(args []string) int {
	// Minimal test/[ implementation
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
		case "-e":
			_, err := os.Stat(args[1])
			if err == nil {
				return 0
			}
			return 1
		case "-r", "-w", "-x":
			_, err := os.Stat(args[1])
			if err == nil {
				return 0
			}
			return 1
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
		}
	}

	// Single arg: true if non-empty
	if len(args) == 1 && args[0] != "" {
		return 0
	}
	return 1
}

func (s *wasiShell) builtinType(args []string, stdout io.Writer) {
	for _, name := range args {
		switch name {
		case "cd", "pwd", "export", "unset", "set", "env", "exit", "true", "false",
			"echo", "test", "[", "type", "read", "source", ".":
			fmt.Fprintf(stdout, "%s is a shell builtin\n", name)
		default:
			if core.IsExecutable(name) {
				fmt.Fprintf(stdout, "%s is a busybox applet\n", name)
			} else {
				fmt.Fprintf(stdout, "ash: type: %s: not found\n", name)
				s.setExit(1)
				return
			}
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
					// Last var gets remainder
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
			// Single-quoted: no expansion
			end := strings.Index(input[i+1:], "'")
			if end < 0 {
				result.WriteString(input[i:])
				break
			}
			result.WriteString(input[i : i+2+end])
			i += 2 + end
		} else if input[i] == '$' {
			if i+1 < len(input) && input[i+1] == '{' {
				// ${VAR} or ${VAR:-default}
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
				// $(...) — handled separately in expandCommandSubstitution
				result.WriteByte(input[i])
				i++
			} else if i+1 < len(input) && (input[i+1] == '?' || isVarChar(input[i+1])) {
				// $VAR or $?
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

func parseRedirections(line string) (args []string, stdinFile, stdoutFile string, stdoutAppend bool, stderrFile string) {
	tokens := shellSplit(line)
	var cleanArgs []string
	for i := 0; i < len(tokens); i++ {
		switch {
		case tokens[i] == "<" && i+1 < len(tokens):
			stdinFile = tokens[i+1]
			i++
		case tokens[i] == ">" && i+1 < len(tokens):
			stdoutFile = tokens[i+1]
			stdoutAppend = false
			i++
		case tokens[i] == ">>" && i+1 < len(tokens):
			stdoutFile = tokens[i+1]
			stdoutAppend = true
			i++
		case tokens[i] == "2>" && i+1 < len(tokens):
			stderrFile = tokens[i+1]
			i++
		case strings.HasPrefix(tokens[i], ">"):
			stdoutFile = tokens[i][1:]
			stdoutAppend = false
		case strings.HasPrefix(tokens[i], ">>"):
			stdoutFile = tokens[i][2:]
			stdoutAppend = true
		default:
			cleanArgs = append(cleanArgs, tokens[i])
		}
	}
	return cleanArgs, stdinFile, stdoutFile, stdoutAppend, stderrFile
}

func splitPipeline(line string) []string {
	var segments []string
	current := ""
	inSingle := false
	inDouble := false
	for _, ch := range line {
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
		}
		if ch == '|' && !inSingle && !inDouble {
			segments = append(segments, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	segments = append(segments, current)
	return segments
}

func splitOnSemicolon(line string) []string {
	var parts []string
	current := ""
	inSingle := false
	inDouble := false
	for _, ch := range line {
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
		}
		if ch == ';' && !inSingle && !inDouble {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	parts = append(parts, current)
	return parts
}

// shellSplit splits a command line into tokens, respecting quotes.
func shellSplit(line string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
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

func isQuoted(line string, pos int) bool {
	inSingle := false
	inDouble := false
	for i := 0; i < pos && i < len(line); i++ {
		if line[i] == '\'' && !inDouble {
			inSingle = !inSingle
		} else if line[i] == '"' && !inSingle {
			inDouble = !inDouble
		}
	}
	return inSingle || inDouble
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