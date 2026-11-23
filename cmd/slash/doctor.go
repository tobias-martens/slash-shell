package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/setup"
	"github.com/tobias-martens/slash-shell/internal/sources"
	"github.com/tobias-martens/slash-shell/internal/takeover"
	"github.com/tobias-martens/slash-shell/shell"
)

// doctor explains what slash sees on this system: sources, shells, and
// which verb names it can use. It only reads, never changes anything.
func (a *app) doctor() int {
	sys := sources.Detect(a.env)
	w := a.stdout
	fmt.Fprintf(w, "slash %s\n\n", version)

	var srcs []string
	if sys.Manager != "" {
		srcs = append(srcs, sys.Manager+" (system)")
	}
	if sys.Flatpak {
		srcs = append(srcs, "flatpak")
	}
	if sys.Snap {
		srcs = append(srcs, "snap")
	}
	if len(srcs) == 0 {
		srcs = []string{"none found"}
	}
	fmt.Fprintf(w, "Sources:   %s\n", strings.Join(srcs, ", "))
	if def := sys.InstallDefault(); def != "" {
		fmt.Fprintf(w, "Installs:  from %s first", def)
		if sys.Immutable {
			fmt.Fprint(w, " (read-only system: the base system updates itself)")
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "\nShells:")
	enabled := setup.Store{Dir: a.state.Dir}.Paths()
	for _, sh := range shell.Supported {
		t, _ := setup.TargetFor(sh, a.home, a.getenv)
		if slices.Contains(enabled, t.Path) {
			fmt.Fprintf(w, "  %-5s enabled in %s\n", sh, tildePath(t.Path, a.home))
		} else {
			fmt.Fprintf(w, "  %-5s not enabled (turn on with: slash enable %s)\n", sh, sh)
		}
	}

	fmt.Fprintln(w, "\nWords:")
	d := a.decider()
	var ok []string
	for _, name := range takeover.Candidates(grammar.Verbs) {
		path, err := a.env.LookPath(name)
		switch {
		case name == "install" && err == nil:
			fmt.Fprintf(w, "  install   slash's; %s still handles files (install -m 755 a b)\n", path)
		case !d.Decide(name, []string{"x"}).Handle:
			fmt.Fprintf(w, "  %-9s %s was installed after slash and wins; slash's: slash %s …\n", name, path, name)
		case err == nil:
			fmt.Fprintf(w, "  %-9s slash's; the program %s stays available as: command %s\n", name, path, name)
		default:
			ok = append(ok, name)
		}
	}
	if len(ok) > 0 {
		fmt.Fprintf(w, "  %s: slash's, no other program uses them\n", strings.Join(ok, ", "))
	}
	return 0
}
