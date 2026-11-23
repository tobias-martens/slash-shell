// Command slash: simple and natural commands for the Linux shell.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/intro"
	"github.com/tobias-martens/slash-shell/internal/probe"
	"github.com/tobias-martens/slash-shell/internal/resolve"
	"github.com/tobias-martens/slash-shell/internal/sources"
	"github.com/tobias-martens/slash-shell/internal/takeover"
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
	probe          resolve.Probe
	state          takeover.State
	root           bool
	confirm        func(question string) bool // asks the user (on the terminal)
	execute        func(command string) int   // runs a command line
}

func main() {
	a := &app{
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		env:     sources.RealEnv(),
		probe:   probe.New(),
		state:   takeover.DefaultState(),
		root:    os.Geteuid() == 0,
		confirm: askTTY,
		execute: runShell,
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
//	slash resolve --words VERB …   (fallback functions: explains problems)
func (a *app) resolveCmd(args []string) int {
	if len(args) == 2 && args[0] == "--line" {
		return a.resolveLine(args[1])
	}
	if len(args) >= 2 && args[0] == "--words" {
		return a.resolveWords(args[1:])
	}
	fmt.Fprintln(a.stderr, "usage: slash resolve --line LINE | --words VERB [WORD…]")
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

func (a *app) resolveWords(words []string) int {
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
	return &resolve.Resolver{Sys: sources.Detect(a.env), Root: a.root, Probe: a.probe, Templates: tmpl}
}

func (a *app) decider() takeover.Decider {
	return takeover.Decider{LookPath: a.env.LookPath, Exists: a.env.Exists, Recorded: a.state.Recorded()}
}

func askTTY(question string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fmt.Fprint(tty, question)
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
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
