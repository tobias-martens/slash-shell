package resolve

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
)

// Services answers questions about services and running programs, locally.
type Services interface {
	// Unit reports whether a service called name exists, and whether it is
	// a "system" or "user" service.
	Unit(name string) (scope string, ok bool)
	// Active reports whether the service is running.
	Active(scope, name string) bool
	// Program reports whether the user runs a program called name.
	Program(name string) bool
}

// ShowList describes what show can show, e.g. "disk space, size of <folder>, …".
func ShowList(entries []data.ShowEntry) string {
	var items []string
	for _, e := range entries {
		item := e.Phrases[0]
		switch e.Target {
		case "folder":
			item += " <folder>"
		case "port":
			item += " <number>"
		case "service":
			item += " <service>"
		}
		items = append(items, item)
	}
	return strings.Join(items, ", ")
}

func (r *Resolver) show(p grammar.Phrase) (Result, error) {
	if len(p.Things) == 0 {
		return Result{}, userErr("show what? I can show: %s", ShowList(r.Show))
	}
	// The longest phrase that starts the words wins ("size of" over "size").
	var entry data.ShowEntry
	n := 0
	for _, e := range r.Show {
		for _, ph := range e.Phrases {
			pw := strings.Fields(ph)
			if len(pw) > n && len(pw) <= len(p.Things) && equalFold(pw, p.Things[:len(pw)]) {
				entry, n = e, len(pw)
			}
		}
	}
	if n == 0 {
		return Result{}, userErr("I can't show %q. I can show: %s", strings.Join(p.Things, " "), ShowList(r.Show))
	}
	rest := p.Things[n:]
	if len(rest) > 0 && entry.Target != "none" {
		switch strings.ToLower(rest[0]) {
		case "of", "in", "for", "on":
			rest = rest[1:]
		}
	}
	what := entry.Phrases[0]
	cmd := entry.Command
	switch entry.Target {
	case "none":
		if len(rest) > 0 {
			return Result{}, userErr("show %s works without anything after it", what)
		}
	case "folder":
		folder := "."
		switch len(rest) {
		case 0:
		case 1:
			folder = rest[0]
		default:
			return Result{}, userErr("one folder at a time; put quotes around names with spaces")
		}
		cmd = strings.ReplaceAll(cmd, "{folder}", Quote(folder))
	case "port":
		port := 0
		if len(rest) == 1 {
			port, _ = strconv.Atoi(rest[0])
		}
		if port < 1 || port > 65535 {
			return Result{}, userErr("which port? For example: show what uses port 8080")
		}
		cmd = strings.ReplaceAll(cmd, "{port}", strconv.Itoa(port))
	case "service":
		if len(rest) != 1 {
			return Result{}, userErr("which service? For example: show logs of ssh")
		}
		name, err := r.serviceName(rest[0])
		if err != nil {
			return Result{}, err
		}
		scope, ok := r.unit(name)
		if !ok {
			return Result{}, userErr("I found no service called %q. Running services: show services", name)
		}
		unit := "-u " + name
		if scope == "user" {
			unit = "--user-unit " + name
		}
		cmd = strings.ReplaceAll(cmd, "{unit}", unit)
	}
	for _, need := range entry.Needs {
		if r.LookPath == nil {
			break
		}
		if _, err := r.LookPath(need); err != nil {
			return Result{}, userErr("show %s needs %s, which isn't installed", what, need)
		}
	}
	return Result{Command: cmd, Phrase: p, Confirm: entry.Internet != "", Reason: entry.Internet}, nil
}

var serviceRe = regexp.MustCompile(`^[A-Za-z0-9@._:-]+$`)

func (r *Resolver) serviceName(word string) (string, error) {
	name := strings.TrimSuffix(word, ".service")
	if !serviceRe.MatchString(name) {
		return "", userErr("%q isn't a service name", word)
	}
	return name, nil
}

func (r *Resolver) unit(name string) (string, bool) {
	if !r.Sys.Systemd || r.Services == nil {
		return "", false
	}
	return r.Services.Unit(name)
}

// services handles start, stop and restart. Services become systemctl
// commands; stopping a program is slash's own job (it waits and asks before
// forcing), so then the whole phrase goes to `slash stop`.
func (r *Resolver) services(p grammar.Phrase) (Result, error) {
	if len(p.Things) == 0 {
		return r.own(p) // bare stop/restart: slash lists what's running
	}
	if !r.Sys.Systemd && p.Verb != grammar.Stop {
		return Result{}, userErr("I only know systemd services so far, and this system doesn't use systemd")
	}
	var system, user []string
	for _, word := range p.Things {
		name, err := r.serviceName(word)
		if err != nil {
			return Result{}, err
		}
		scope, isUnit := r.unit(name)
		isProgram := r.Services != nil && r.Services.Program(name)
		switch p.Verb {
		case grammar.Start:
			if !isUnit {
				if _, err := r.lookPath(name); err == nil {
					return Result{}, userErr("%s is a program, not a service. To start it, type: %s", name, name)
				}
				return Result{}, userErr("I found no service called %q", name)
			}
		case grammar.Restart:
			if !isUnit {
				if isProgram {
					return Result{}, userErr("%s is a program, not a service: stop %s, then start it again by typing %s", name, name, name)
				}
				return Result{}, userErr("I found no service called %q. Running services: restart", name)
			}
		case grammar.Stop:
			if isProgram {
				return r.own(p) // slash stops programs itself (and asks if it's also a service)
			}
			if !isUnit {
				return Result{}, userErr("nothing called %q is running. To see what is: stop", name)
			}
			if !r.Services.Active(scope, name) {
				return Result{}, userErr("the service %s isn't running", name)
			}
		}
		if scope == "user" {
			user = append(user, name)
		} else {
			system = append(system, name)
		}
	}
	return Result{Command: Systemctl(p.Verb, r.Root, system, user), Phrase: p}, nil
}

// Systemctl returns the command that applies verb (start, stop, restart) to
// system services (with sudo unless root) and user services.
func Systemctl(verb string, root bool, system, user []string) string {
	var parts []string
	if len(system) > 0 {
		sudo := "sudo "
		if root {
			sudo = ""
		}
		parts = append(parts, sudo+"systemctl "+verb+" "+strings.Join(system, " "))
	}
	if len(user) > 0 {
		parts = append(parts, "systemctl --user "+verb+" "+strings.Join(user, " "))
	}
	return strings.Join(parts, "; ")
}

func (r *Resolver) lookPath(name string) (string, error) {
	if r.LookPath == nil {
		return "", errNoLookPath
	}
	return r.LookPath(name)
}

var errNoLookPath = &UserError{"no LookPath"}

func equalFold(a, b []string) bool {
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}
