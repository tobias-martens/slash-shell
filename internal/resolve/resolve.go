// Package resolve turns a parsed phrase into the real command line for this
// system. It never runs anything that changes the system: it only looks at
// local package data (through a Probe) and returns text.
package resolve

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

// Probe answers questions about packages from local data only.
type Probe interface {
	// Available reports whether pkg can be installed from source, and the
	// name to use for it there (e.g. a flatpak app ID).
	Available(source, pkg string) (name string, ok bool)
	// Installed reports whether pkg is installed from source, and its name there.
	Installed(source, pkg string) (name string, ok bool)
}

// Result is a resolved command.
type Result struct {
	Command string // the real command line
	Confirm bool   // slash had to choose: show it, but let the user confirm
	Reason  string // why it had to choose (shown in the comment)
	Phrase  grammar.Phrase
	// Own: the command is slash itself (search, about), which shows the
	// real commands it runs. No "#slash:" comment, it would only repeat it.
	Own bool
}

// Line is what goes onto the command line and into history.
func (r Result) Line() string {
	if r.Own {
		return r.Command
	}
	return r.Command + "  " + r.comment()
}

func (r Result) comment() string {
	c := "#slash: " + r.Phrase.Text
	if r.Reason != "" {
		c += " (" + r.Reason + ")"
	}
	return c
}

// HistoryEntry rewrites a history entry that contains the phrase, so history
// keeps a real, replayable command line:
//
//	update && reboot  →  { sudo apt update && sudo apt upgrade; } && reboot  #slash: update
//
// It returns "" (leave history alone) unless the phrase occurs exactly once
// as a whole command in a single-line entry.
func (r Result) HistoryEntry(entry string) string {
	phrase := r.Phrase.Text
	if strings.TrimSpace(entry) == phrase {
		return r.Line()
	}
	if strings.Contains(entry, "\n") {
		return ""
	}
	at := -1
	for i := 0; i+len(phrase) <= len(entry); i++ {
		if entry[i:i+len(phrase)] != phrase {
			continue
		}
		if !startsCommand(entry, i) || !endsCommand(entry, i+len(phrase)) {
			continue
		}
		if at >= 0 {
			return "" // more than once: ambiguous
		}
		at = i
	}
	if at < 0 {
		return ""
	}
	cmd := r.Command
	if strings.ContainsAny(cmd, ";&|") {
		cmd = "{ " + cmd + "; }" // keep the meaning inside a larger line
	}
	if r.Own {
		return entry[:at] + cmd + entry[at+len(phrase):]
	}
	return entry[:at] + cmd + entry[at+len(phrase):] + "  " + r.comment()
}

// startsCommand reports whether position i in s is where a command starts:
// the line start, or after ; & | ( with optional blanks in between.
func startsCommand(s string, i int) bool {
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	return i == 0 || strings.IndexByte(";&|(", s[i-1]) >= 0
}

// endsCommand reports whether a command ends at position i in s: the line
// end, or ; & | ) # with optional blanks in between.
func endsCommand(s string, i int) bool {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i == len(s) || strings.IndexByte(";&|)#", s[i]) >= 0
}

// Resolver resolves phrases for one system.
type Resolver struct {
	Sys       sources.System
	Root      bool
	Probe     Probe
	Templates data.Templates
	Show      []data.ShowEntry
	Services  Services
	LookPath  func(string) (string, error)
	Archives  []data.Archive
	Exists    func(path string) bool // relative to the current folder
}

// UserError is a problem the user should be told about in plain words.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(format string, a ...any) error { return &UserError{fmt.Sprintf(format, a...)} }

// Resolve returns the command for p.
func (r *Resolver) Resolve(p grammar.Phrase) (Result, error) {
	switch p.Verb {
	case grammar.Update:
		return r.update(p)
	case grammar.Install:
		return r.install(p)
	case grammar.Remove:
		return r.remove(p)
	case grammar.Search, grammar.About:
		return r.own(p)
	case grammar.Show:
		return r.show(p)
	case grammar.Start, grammar.Stop, grammar.Restart:
		return r.services(p)
	case grammar.Clean:
		return r.clean(p)
	case grammar.Unpack:
		return r.unpack(p)
	case grammar.Pack:
		return r.pack(p)
	case grammar.Delete:
		return r.delete(p)
	}
	return Result{}, userErr("I don't know how to %s yet", p.Word)
}

func (r *Resolver) update(p grammar.Phrase) (Result, error) {
	var list []string
	if p.Source != "" {
		src, err := r.source(p.Source)
		if err != nil {
			return Result{}, err
		}
		if src == r.Sys.Manager && r.Sys.Immutable {
			return Result{}, userErr("this system's base is updated by its own updater, not by %s", src)
		}
		list = []string{src}
	} else {
		if r.Sys.Manager != "" && !r.Sys.Immutable {
			list = append(list, r.Sys.Manager)
		}
		if r.Sys.Flatpak {
			list = append(list, "flatpak")
		}
		if r.Sys.Snap {
			list = append(list, "snap")
		}
		if len(list) == 0 {
			return Result{}, userErr("I found nothing to update on this system")
		}
	}
	var cmds []string
	for _, src := range list {
		c, err := r.expand(src, "update", nil)
		if err != nil {
			return Result{}, err
		}
		cmds = append(cmds, c)
	}
	return Result{Command: strings.Join(cmds, "; "), Phrase: p}, nil
}

func (r *Resolver) install(p grammar.Phrase) (Result, error) {
	if p.Source != "" {
		src, err := r.source(p.Source)
		if err != nil {
			return Result{}, err
		}
		names := make([]string, len(p.Things))
		for i, pkg := range p.Things {
			names[i] = pkg
			if n, ok := r.Probe.Available(src, pkg); ok {
				names[i] = n
			}
		}
		c, err := r.expand(src, "install", names)
		return Result{Command: c, Phrase: p}, err
	}

	def := r.Sys.InstallDefault()
	if def == "" {
		return Result{}, userErr("I found no package manager on this system")
	}
	var g grouping
	var reasons []string
	for _, pkg := range p.Things {
		if n, ok := r.Probe.Available(def, pkg); ok {
			g.add(def, n)
			continue
		}
		found := false
		for _, alt := range r.alternatives(def) {
			if n, ok := r.Probe.Available(alt, pkg); ok {
				g.add(alt, n)
				reasons = append(reasons, fmt.Sprintf("%s: not in %s, found in %s", pkg, def, alt))
				found = true
				break
			}
		}
		if !found {
			// Let the package manager itself report it; its lists may be newer.
			g.add(def, pkg)
		}
	}
	return r.build(p, "install", g, reasons)
}

func (r *Resolver) remove(p grammar.Phrase) (Result, error) {
	if p.Source == "" {
		if res, isFile, err := r.removeFiles(p); isFile {
			return res, err
		}
	}
	if p.Source != "" {
		src, err := r.source(p.Source)
		if err != nil {
			return Result{}, err
		}
		names := make([]string, len(p.Things))
		for i, pkg := range p.Things {
			names[i] = pkg
			if n, ok := r.Probe.Installed(src, pkg); ok {
				names[i] = n
			}
		}
		c, err := r.expand(src, "remove", names)
		return Result{Command: c, Phrase: p}, err
	}

	var g grouping
	var reasons []string
	for _, pkg := range p.Things {
		var where []string
		var name string
		for _, src := range r.present() {
			if n, ok := r.Probe.Installed(src, pkg); ok {
				if len(where) == 0 {
					name = n
				}
				where = append(where, src)
			}
		}
		switch len(where) {
		case 0:
			g.add(r.Sys.InstallDefault(), pkg) // let the package manager report it
		case 1:
			g.add(where[0], name)
		default:
			g.add(where[0], name)
			reasons = append(reasons, fmt.Sprintf("%s is also installed from %s", pkg, strings.Join(where[1:], ", ")))
		}
	}
	return r.build(p, "remove", g, reasons)
}

func (r *Resolver) build(p grammar.Phrase, key string, g grouping, reasons []string) (Result, error) {
	var cmds []string
	for _, src := range g.order {
		c, err := r.expand(src, key, g.pkgs[src])
		if err != nil {
			return Result{}, err
		}
		cmds = append(cmds, c)
	}
	return Result{
		Command: strings.Join(cmds, "; "),
		Confirm: len(reasons) > 0,
		Reason:  strings.Join(reasons, "; "),
		Phrase:  p,
	}, nil
}

// own hands the phrase to slash itself, after checking the source exists.
func (r *Resolver) own(p grammar.Phrase) (Result, error) {
	if p.Source != "" && p.Source != "files" && p.Source != "packages" {
		if _, err := r.source(p.Source); err != nil {
			return Result{}, err
		}
	}
	words := make([]string, len(p.Words))
	for i, w := range p.Words {
		words[i] = Quote(w)
	}
	return Result{Command: "slash " + strings.Join(words, " "), Phrase: p, Own: true}, nil
}

// source maps a canonical source word to a source present on this system.
func (r *Resolver) source(word string) (string, error) {
	switch word {
	case "system":
		if r.Sys.Manager == "" {
			return "", userErr("I found no system package manager here")
		}
		return r.Sys.Manager, nil
	case "flatpak":
		if !r.Sys.Flatpak {
			return "", userErr("flatpak isn't installed here")
		}
		return word, nil
	case "snap":
		if !r.Sys.Snap {
			return "", userErr("snap isn't installed here")
		}
		return word, nil
	}
	if word != r.Sys.Manager {
		if r.Sys.Manager == "" {
			return "", userErr("%s isn't available here", word)
		}
		return "", userErr("%s isn't available here; this system uses %s", word, r.Sys.Manager)
	}
	return word, nil
}

// present lists the sources on this system, system package manager first.
func (r *Resolver) present() []string {
	var list []string
	if r.Sys.Manager != "" {
		list = append(list, r.Sys.Manager)
	}
	if r.Sys.Flatpak {
		list = append(list, "flatpak")
	}
	if r.Sys.Snap {
		list = append(list, "snap")
	}
	return list
}

// alternatives lists where to look when def doesn't have a package. The base
// of an immutable system is never an alternative.
func (r *Resolver) alternatives(def string) []string {
	var list []string
	for _, s := range r.present() {
		if s == def || (s == r.Sys.Manager && r.Sys.Immutable) {
			continue
		}
		list = append(list, s)
	}
	return list
}

func (r *Resolver) expand(source, key string, pkgs []string) (string, error) {
	tmpl, ok := r.Templates.Lookup(source, key, r.Sys.OSID)
	if !ok {
		return "", userErr("I don't know how to %s with %s", key, source)
	}
	sudo := "sudo "
	if r.Root {
		sudo = ""
	}
	quoted := make([]string, len(pkgs))
	for i, p := range pkgs {
		quoted[i] = Quote(p)
	}
	s := strings.ReplaceAll(tmpl, "{sudo}", sudo)
	return strings.ReplaceAll(s, "{pkgs}", strings.Join(quoted, " ")), nil
}

var plain = regexp.MustCompile(`^[A-Za-z0-9._+:@/%-]+$`)

// Quote shell-quotes s unless it is plainly safe.
func Quote(s string) string {
	if plain.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// grouping collects packages per source, keeping first-seen source order.
type grouping struct {
	order []string
	pkgs  map[string][]string
}

func (g *grouping) add(src, pkg string) {
	if g.pkgs == nil {
		g.pkgs = map[string][]string{}
	}
	if _, ok := g.pkgs[src]; !ok {
		g.order = append(g.order, src)
	}
	g.pkgs[src] = append(g.pkgs[src], pkg)
}
