package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/probe"
	"github.com/tobias-martens/slash-shell/internal/resolve"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

// serviceInfo is what slash needs to know about services and programs.
type serviceInfo interface {
	resolve.Services
	Programs() []probe.Program
	Running(scope string) []string
	All(scope string) []string
	Scopes() []string
}

// processes stops processes; replaced in tests.
type processes struct {
	kill  func(pid int, sig syscall.Signal) error
	alive func(pid int) bool
	sleep func(time.Duration)
}

func realProcesses() processes {
	return processes{
		kill:  func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		alive: func(pid int) bool { return probe.Alive("/proc", pid) },
		sleep: time.Sleep,
	}
}

const gracePeriod = 5 * time.Second

// stop implements `slash stop …` and bare `slash stop`/`slash restart`.
// Programs are stopped politely (TERM), and only forced (KILL) after asking.
func (a *app) stop(p grammar.Phrase) int {
	if len(p.Things) == 0 {
		return a.listRunning(p.Verb)
	}
	sys := sources.Detect(a.env)
	progs := a.services.Programs()
	var system, user []string
	code := 0
	for _, name := range p.Things {
		name = strings.TrimSuffix(name, ".service")
		prog, isProg := probe.FindProgram(progs, name)
		scope, isUnit := "", false
		if sys.Systemd {
			scope, isUnit = a.services.Unit(name)
		}
		active := isUnit && a.services.Active(scope, name)
		if isProg && active {
			answer := a.ask(fmt.Sprintf("%s is a service and a program. Stop the [s]ervice or the [p]rogram? ", name))
			switch {
			case strings.HasPrefix(answer, "s"):
				isProg = false
			case strings.HasPrefix(answer, "p"):
				active = false
			default:
				continue
			}
		}
		switch {
		case isProg:
			if c := a.stopProgram(prog); c != 0 {
				code = c
			}
		case active && scope == "user":
			user = append(user, name)
		case active:
			system = append(system, name)
		case isUnit:
			fmt.Fprintf(a.stderr, "slash: the service %s isn't running\n", name)
			code = exitUserError
		default:
			fmt.Fprintf(a.stderr, "slash: nothing called %q is running. To see what is: stop\n", name)
			code = exitUserError
		}
	}
	if cmd := resolve.Systemctl("stop", a.root, system, user); cmd != "" {
		fmt.Fprintf(a.stderr, "→ %s\n", cmd)
		if c := a.execute(cmd); c != 0 {
			code = c
		}
	}
	return code
}

func (a *app) stopProgram(prog probe.Program) int {
	fmt.Fprintf(a.stderr, "→ kill %s\n", pidList(prog.PIDs))
	for _, pid := range prog.PIDs {
		a.procs.kill(pid, syscall.SIGTERM)
	}
	left := a.waitGone(prog.PIDs, gracePeriod)
	if len(left) == 0 {
		return 0
	}
	if !a.confirm(fmt.Sprintf("%s is still running. Force it to stop? [y/N] ", prog.Name)) {
		fmt.Fprintf(a.stderr, "slash: %s is still running; to force it: kill -KILL %s\n", prog.Name, pidList(left))
		return 1
	}
	fmt.Fprintf(a.stderr, "→ kill -KILL %s\n", pidList(left))
	for _, pid := range left {
		a.procs.kill(pid, syscall.SIGKILL)
	}
	if len(a.waitGone(left, time.Second)) > 0 {
		fmt.Fprintf(a.stderr, "slash: %s didn't stop\n", prog.Name)
		return 1
	}
	return 0
}

// waitGone waits up to d for pids to end and returns those still running.
func (a *app) waitGone(pids []int, d time.Duration) []int {
	const step = 100 * time.Millisecond
	for waited := time.Duration(0); ; waited += step {
		var left []int
		for _, pid := range pids {
			if a.procs.alive(pid) {
				left = append(left, pid)
			}
		}
		if len(left) == 0 || waited >= d {
			return left
		}
		a.procs.sleep(step)
	}
}

func pidList(pids []int) string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, " ")
}

// listRunning answers bare `stop` / `restart`: what could be stopped or
// restarted.
func (a *app) listRunning(verb string) int {
	sys := sources.Detect(a.env)
	if verb == grammar.Stop {
		var names []string
		for _, p := range a.services.Programs() {
			names = append(names, p.Name)
		}
		a.printList("Programs", names)
	}
	if sys.Systemd {
		for _, scope := range a.services.Scopes() {
			title := "Services"
			if scope == "user" {
				title = "Your services"
			}
			a.printList(title, a.services.Running(scope))
		}
	}
	fmt.Fprintf(a.stdout, "\n%s one with: %s <name>\n", strings.ToUpper(verb[:1])+verb[1:], verb)
	return 0
}

// printList prints "Title:  a, b, c" wrapped to the terminal width.
func (a *app) printList(title string, names []string) {
	if len(names) == 0 {
		return
	}
	const indent = "  "
	width := a.columns()
	fmt.Fprintf(a.stdout, "%s:\n", title)
	line := indent
	for i, n := range names {
		item := n
		if i < len(names)-1 {
			item += ","
		}
		if len(line)+len(item)+1 > width && line != indent {
			fmt.Fprintln(a.stdout, strings.TrimRight(line, " "))
			line = indent
		}
		line += item + " "
	}
	fmt.Fprintln(a.stdout, strings.TrimRight(line, " "))
}

// complete prints Tab-completion candidates for start, stop and restart.
func (a *app) complete(args []string) int {
	if len(args) == 0 {
		return exitUserError
	}
	sys := sources.Detect(a.env)
	var names []string
	switch args[0] {
	case grammar.Stop:
		for _, p := range a.services.Programs() {
			names = append(names, p.Name)
		}
		fallthrough
	case grammar.Restart:
		if sys.Systemd {
			for _, s := range a.services.Scopes() {
				names = append(names, a.services.Running(s)...)
			}
		}
	case grammar.Start:
		if sys.Systemd {
			for _, s := range a.services.Scopes() {
				names = append(names, a.services.All(s)...)
			}
		}
	default:
		return exitUserError
	}
	slices.Sort(names)
	for _, n := range slices.Compact(names) {
		fmt.Fprintln(a.stdout, n)
	}
	return 0
}
