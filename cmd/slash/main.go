// Command slash: simple and natural commands for the Linux shell.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/intro"
	"github.com/tobias-martens/slash-shell/internal/probe"
	"github.com/tobias-martens/slash-shell/internal/resolve"
	"github.com/tobias-martens/slash-shell/internal/setup"
	"github.com/tobias-martens/slash-shell/internal/sources"
	"github.com/tobias-martens/slash-shell/internal/takeover"
	"github.com/tobias-martens/slash-shell/shell"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "0.0.0-dev"

// Exit codes of `slash resolve`, read by the shell glue.
const (
	exitRun        = 0 // stdout: line to run
	exitNotHandled = 1 // leave the line alone
	exitUserError  = 2 // slash understood the verb but can't do it; message on stderr
	exitConfirm    = 3 // stdout: line to show, but let the user confirm
)

// app bundles everything slash talks to, so tests can replace it.
type app struct {
	stdout, stderr io.Writer
	env            sources.Env
	probe          catalog
	files          fileFinder
	services       serviceInfo
	procs          processes
	ask            func(question string) string // asks the user (on the terminal)
	state          takeover.State
	root           bool
	home           string
	getenv         func(string) string
	confirm        func(question string) bool // asks the user (on the terminal)
	execute        func(command string) int   // runs a command line
}

func main() {
	a := &app{
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		env:      sources.RealEnv(),
		probe:    probe.New(),
		files:    probe.NewFiles(),
		services: probe.NewServices(probe.ExecRunner(2 * time.Second)),
		procs:    realProcesses(),
		ask:      askTTY,
		state:    takeover.DefaultState(),
		root:     os.Geteuid() == 0,
		home:     homeDir(),
		getenv:   os.Getenv,
		confirm:  func(q string) bool { a := askTTY(q); return a == "y" || a == "yes" },
		execute:  runShell,
	}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stdout, intro.Render(sources.Detect(a.env), version))
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, intro.Render(sources.Detect(a.env), version))
		return 0
	case "version", "--version":
		fmt.Fprintf(a.stdout, "slash %s\n", version)
		return 0
	case "resolve":
		return a.resolveCmd(args[1:])
	case "enable":
		return a.enable(args[1:])
	case "disable":
		return a.disable()
	case "doctor":
		return a.doctor()
	case "complete":
		return a.complete(args[1:])
	case "init":
		if len(args) != 2 {
			fmt.Fprintf(a.stderr, "usage: slash init %s\n", strings.Join(shell.Supported, "|"))
			return exitUserError
		}
		script, err := shell.Script(args[1], takeover.Candidates(grammar.Verbs))
		if err != nil {
			fmt.Fprintf(a.stderr, "slash: %v\n", err)
			return exitUserError
		}
		fmt.Fprint(a.stdout, script)
		return 0
	}
	if _, ok := grammar.Verbs[args[0]]; ok {
		return a.direct(args)
	}
	fmt.Fprintf(a.stderr, "slash: I don't know %q. Run slash on its own to see what I can do.\n", args[0])
	return exitUserError
}

// resolveCmd implements
//
//	slash resolve --line LINE      (Enter hook: silent, whole typed line)
//	slash resolve --words [--history ENTRY] VERB …
//	                               (fallback functions: explains problems;
//	                                prints a rewritten history entry as a
//	                                second line when ENTRY contains the phrase)
func (a *app) resolveCmd(args []string) int {
	if len(args) == 2 && args[0] == "--line" {
		return a.resolveLine(args[1])
	}
	if len(args) >= 4 && args[0] == "--words" && args[1] == "--history" {
		return a.resolveWords(args[3:], args[2])
	}
	if len(args) >= 2 && args[0] == "--words" {
		return a.resolveWords(args[1:], "")
	}
	fmt.Fprintln(a.stderr, "usage: slash resolve --line LINE | --words [--history ENTRY] VERB [WORD…]")
	return exitUserError
}

func (a *app) resolveLine(line string) int {
	p, err := grammar.ParseLine(line)
	if err != nil {
		return exitNotHandled // grammar errors are explained by the fallback function
	}
	fields := strings.Fields(line)
	if !a.decider().Decide(fields[0], fields[1:]).Handle {
		return exitNotHandled
	}
	res, err := a.resolver().Resolve(p)
	if err != nil {
		return exitNotHandled
	}
	fmt.Fprintln(a.stdout, res.Line())
	if res.Confirm {
		return exitConfirm
	}
	return exitRun
}

func (a *app) resolveWords(words []string, history string) int {
	d := a.decider().Decide(words[0], words[1:])
	if !d.Handle {
		if d.Newer != "" && a.state.NotifyOnce(words[0]) {
			fmt.Fprintf(a.stderr, "slash: %q now runs %s, which was installed after slash.\n"+
				"       slash's own version stays available as: slash %s …\n", words[0], d.Newer, words[0])
		}
		return exitNotHandled
	}
	res, code := a.resolveOrExplain(words)
	if code != exitRun {
		return code
	}
	fmt.Fprintln(a.stdout, res.Line())
	if history != "" {
		if entry := res.HistoryEntry(history); entry != "" {
			fmt.Fprintln(a.stdout, entry)
		}
	}
	if res.Confirm {
		return exitConfirm
	}
	return exitRun
}

// direct runs `slash <verb> …`: shows the real command, asks if slash had to
// choose, then runs it.
func (a *app) direct(words []string) int {
	res, code := a.resolveOrExplain(words)
	if code != exitRun {
		return code
	}
	if res.Own {
		return a.lookup(res.Phrase)
	}
	fmt.Fprintf(a.stderr, "→ %s\n", res.Command)
	if res.Confirm {
		fmt.Fprintf(a.stderr, "  (%s)\n", res.Reason)
		if !a.confirm("Run it? [y/N] ") {
			return exitNotHandled
		}
	}
	return a.execute(res.Command)
}

func (a *app) resolveOrExplain(words []string) (resolve.Result, int) {
	p, err := grammar.Parse(words)
	if errors.Is(err, grammar.ErrNotAPhrase) {
		return resolve.Result{}, exitNotHandled
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "slash: %v\n", err)
		return resolve.Result{}, exitUserError
	}
	res, err := a.resolver().Resolve(p)
	if err != nil {
		fmt.Fprintf(a.stderr, "slash: %v\n", err)
		return resolve.Result{}, exitUserError
	}
	return res, exitRun
}

func (a *app) resolver() *resolve.Resolver {
	tmpl, err := data.Load()
	if err != nil {
		panic(err) // embedded data is checked by tests
	}
	show, err := data.LoadShow()
	if err != nil {
		panic(err)
	}
	return &resolve.Resolver{Sys: sources.Detect(a.env), Root: a.root, Probe: a.probe, Templates: tmpl,
		Show: show, Services: a.services, LookPath: a.env.LookPath}
}

func (a *app) decider() takeover.Decider {
	return takeover.Decider{LookPath: a.env.LookPath, Exists: a.env.Exists, Recorded: a.state.Recorded()}
}

// askTTY asks on the terminal and returns the answer in lower case ("" if
// there is no terminal).
func askTTY(question string) string {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return ""
	}
	defer tty.Close()
	fmt.Fprint(tty, question)
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(answer))
}

func runShell(command string) int {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "slash: %v\n", err)
		return 127
	}
	return 0
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// enable hooks slash into the given shells (default: the login shell) and
// records which verb names already exist, so slash takes those over.
func (a *app) enable(shells []string) int {
	if len(shells) == 0 {
		sh := filepath.Base(a.getenv("SHELL"))
		if sh == "" || sh == "." {
			fmt.Fprintln(a.stderr, "slash: which shell? For example: slash enable bash")
			return exitUserError
		}
		shells = []string{sh}
	}
	store := setup.Store{Dir: a.state.Dir}
	for _, sh := range shells {
		t, err := setup.TargetFor(sh, a.home, a.getenv)
		if err != nil {
			fmt.Fprintf(a.stderr, "slash: %v\n", err)
			return exitUserError
		}
		changed, err := store.Enable(t)
		if err != nil {
			fmt.Fprintf(a.stderr, "slash: %v\n", err)
			return 1
		}
		if changed {
			fmt.Fprintf(a.stdout, "slash is now enabled for %s (added to %s).\n", sh, tildePath(t.Path, a.home))
		} else {
			fmt.Fprintf(a.stdout, "slash was already enabled for %s.\n", sh)
		}
	}

	var taken []string
	for _, name := range takeover.Candidates(grammar.Verbs) {
		if _, err := a.env.LookPath(name); err == nil {
			taken = append(taken, name)
		}
	}
	if err := a.state.Record(taken); err != nil {
		fmt.Fprintf(a.stderr, "slash: %v\n", err)
		return 1
	}
	if len(taken) > 0 {
		fmt.Fprintln(a.stdout, "\nThese names already exist on this system; slash takes them over:")
		for _, name := range taken {
			path, _ := a.env.LookPath(name)
			if name == "install" {
				fmt.Fprintf(a.stdout, "  install  %s keeps working for files, e.g. install -m 755 script ~/bin/\n", path)
				continue
			}
			fmt.Fprintf(a.stdout, "  %-8s %s, still available as: command %s\n", name, path, name)
		}
	}
	fmt.Fprintln(a.stdout, "\nOpen a new terminal to start. Undo everything with: slash disable")
	return 0
}

// disable undoes enable exactly and removes slash's state and cache.
func (a *app) disable() int {
	notes, err := setup.Store{Dir: a.state.Dir}.Disable()
	for _, n := range notes {
		fmt.Fprintf(a.stderr, "slash: note: %s\n", n)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(a.stderr, "slash: %v\n", err)
		return 1
	}
	os.RemoveAll(a.state.Dir)
	if cache, err := os.UserCacheDir(); err == nil && a.home != "" && strings.HasPrefix(cache, a.home) {
		os.RemoveAll(filepath.Join(cache, "slash"))
	}
	fmt.Fprintln(a.stdout, "slash is disabled: new terminals start without it, open ones keep working as before.")
	if self, err := os.Executable(); err == nil {
		fmt.Fprintf(a.stdout, "To remove the program itself: rm %s\n", tildePath(self, a.home))
	}
	return 0
}

func tildePath(p, home string) string {
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}
