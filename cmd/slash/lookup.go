package main

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/probe"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

// catalog answers package questions from local data only.
type catalog interface {
	Available(source, pkg string) (string, bool)
	Installed(source, pkg string) (string, bool)
	Choices(source, pkg string, installed bool) []string
	Search(source string, words []string, limit int) ([]probe.Match, int, string, error)
	About(source, pkg string) (probe.Info, bool)
}

// fileFinder finds files by name, offline.
type fileFinder interface {
	Find(words []string, limit int) ([]string, int, string)
}

// lookup runs slash's own verbs (search, about), which print results
// instead of running one command.
func (a *app) lookup(p grammar.Phrase) int {
	switch p.Verb {
	case grammar.Search:
		return a.search(p)
	case grammar.About:
		return a.about(p)
	case grammar.Stop, grammar.Restart:
		return a.stop(p)
	case grammar.Delete:
		return a.trash(p.Things)
	}
	return exitNotHandled
}

// packageSources lists the package sources to look in for phrase source src.
func packageSources(sys sources.System, src string) []string {
	var all []string
	if sys.Manager != "" {
		all = append(all, sys.Manager)
	}
	if sys.Flatpak {
		all = append(all, "flatpak")
	}
	if sys.Snap {
		all = append(all, "snap")
	}
	switch src {
	case "", "packages":
		return all
	case "files":
		return nil
	case "system":
		return all[:min(1, len(all))]
	}
	return []string{src}
}

type section struct {
	title, how string
	lines      []string
	total      int
	shown      int
	err        error
}

func (a *app) search(p grammar.Phrase) int {
	sys := sources.Detect(a.env)
	words := p.Things
	query := strings.Join(words, " ")
	limit := 5
	if p.Source != "" {
		limit = 30 // narrowed down: show more
	}
	withFiles := p.Source == "" || p.Source == "files"
	srcs := packageSources(sys, p.Source)

	sections := make([]section, len(srcs)+1)
	var wg sync.WaitGroup
	if withFiles {
		wg.Go(func() {
			paths, total, how := a.files.Find(words, limit)
			s := section{title: "Files", how: tildeAll(how, a.home), total: total, shown: len(paths)}
			for _, path := range paths {
				s.lines = append(s.lines, tildeAll(path, a.home))
			}
			sections[0] = s
		})
	}
	for i, src := range srcs {
		wg.Go(func() {
			ms, total, how, err := a.probe.Search(src, words, limit)
			sections[i+1] = section{title: src, how: tildeAll(how, a.home), total: total, shown: len(ms),
				lines: a.packageLines(src, ms), err: err}
		})
	}
	wg.Wait()

	var nothing, noData []string
	first := true
	for i, s := range sections {
		if i == 0 && !withFiles {
			continue
		}
		switch {
		case errors.Is(s.err, probe.ErrNoData):
			noData = append(noData, s.title)
			continue
		case s.err != nil:
			fmt.Fprintf(a.stderr, "slash: %s: %v\n", s.title, s.err)
			continue
		case s.total == 0:
			nothing = append(nothing, strings.ToLower(s.title))
			continue
		}
		if !first {
			fmt.Fprintln(a.stdout)
		}
		first = false
		fmt.Fprintf(a.stdout, "%s  (%s)\n", s.title, s.how)
		for _, l := range s.lines {
			fmt.Fprintf(a.stdout, "  %s\n", l)
		}
		if more := s.total - s.shown; more > 0 {
			where := "files"
			if i > 0 {
				where = s.title
			}
			fmt.Fprintf(a.stdout, "  … %d more: search %s in %s\n", more, query, where)
		}
	}
	if !first && (len(nothing) > 0 || len(noData) > 0) {
		fmt.Fprintln(a.stdout)
	}
	if len(nothing) > 0 {
		fmt.Fprintf(a.stdout, "Nothing found in %s.\n", joinAnd(nothing))
	}
	if len(noData) > 0 {
		fmt.Fprintf(a.stdout, "No package lists downloaded yet for %s; run: update\n", joinAnd(noData))
	}
	if first {
		return 1
	}
	return 0
}

// packageLines formats matches as aligned "name  summary  [installed]" lines.
func (a *app) packageLines(src string, ms []probe.Match) []string {
	width := 0
	for _, m := range ms {
		width = max(width, len(displayName(m)))
	}
	width = min(width, 32)
	cols := a.columns()
	var out []string
	for _, m := range ms {
		summary := m.Summary
		if src == "flatpak" && m.Name != "" {
			summary = strings.TrimSuffix(m.Name+": "+summary, ": ")
		}
		line := fmt.Sprintf("%-*s  %s", width, displayName(m), summary)
		mark := ""
		if m.Installed {
			mark = "  [installed]"
		}
		out = append(out, strings.TrimRight(truncate(line, cols-2-len(mark)), " ")+mark)
	}
	return out
}

// displayName is what the user types to install it.
func displayName(m probe.Match) string {
	if m.ID != "" {
		return m.ID
	}
	return m.Name
}

func (a *app) about(p grammar.Phrase) int {
	sys := sources.Detect(a.env)
	srcs := packageSources(sys, p.Source)
	code := 0
	for n, pkg := range p.Things {
		if n > 0 {
			fmt.Fprintln(a.stdout)
		}
		infos := make([]probe.Info, len(srcs))
		found := make([]bool, len(srcs))
		var wg sync.WaitGroup
		for i, src := range srcs {
			wg.Go(func() { infos[i], found[i] = a.probe.About(src, pkg) })
		}
		wg.Wait()
		var hits []probe.Info
		var how []string
		for i := range srcs {
			how = append(how, infos[i].How...)
			if found[i] {
				hits = append(hits, infos[i])
			}
		}
		// A name several apps share: list them, never pick one.
		var several []string
		for _, src := range srcs {
			if ch := a.probe.Choices(src, pkg, false); len(ch) > 1 {
				several = append(several, fmt.Sprintf("Several %s apps are called %q: %s. For one of them: about %s", src, pkg, strings.Join(ch, ", "), ch[0]))
			}
		}
		if len(hits) == 0 && len(several) == 0 {
			fmt.Fprintf(a.stdout, "No package called %q here (looked in %s). Similar names: search %s\n",
				pkg, joinAnd(srcs), pkg)
			code = 1
			continue
		}
		if len(hits) > 0 {
			a.printAbout(pkg, hits, how)
		}
		for _, s := range several {
			fmt.Fprintln(a.stdout, s)
		}
	}
	return code
}

func (a *app) printAbout(pkg string, hits []probe.Info, how []string) {
	var summary, homepage string
	for _, h := range hits {
		summary = cmp.Or(summary, h.Summary)
		homepage = cmp.Or(homepage, h.Homepage)
	}
	if summary != "" {
		fmt.Fprintf(a.stdout, "%s: %s\n", pkg, summary)
	} else {
		fmt.Fprintln(a.stdout, pkg)
	}
	if homepage != "" {
		fmt.Fprintf(a.stdout, "Website: %s\n", homepage)
	}
	fmt.Fprintln(a.stdout)
	vw := 0
	for _, h := range hits {
		vw = max(vw, len(cmp.Or(h.Installed, h.Version)))
	}
	for _, h := range hits {
		status, version := "available", h.Version
		if h.Installed != "" {
			status, version = "installed", h.Installed
			if version == "yes" {
				version = h.Version
			}
		}
		var extra []string
		if h.Name != pkg {
			extra = append(extra, "as "+h.Name)
		}
		if h.Installed != "" && h.Version != "" && h.Installed != "yes" && h.Version != h.Installed {
			extra = append(extra, "newer: "+h.Version)
		}
		if h.Source == "snap" && h.Installed == "" {
			extra = append(extra, "details only online: snap info "+pkg)
		}
		line := fmt.Sprintf("  %-8s %-9s  %-*s  %s", h.Source, status, vw, version, strings.Join(extra, ", "))
		fmt.Fprintln(a.stdout, strings.TrimRight(line, " "))
	}
	fmt.Fprintf(a.stdout, "\nRead from: %s\n", strings.Join(how, "; "))
}

func (a *app) columns() int {
	if n, err := strconv.Atoi(a.getenv("COLUMNS")); err == nil && n > 20 {
		return n
	}
	return 80
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n < 1 {
		return s
	}
	return string(r[:n-1]) + "…"
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// tildeAll writes the home directory as ~ in every space-separated word
// that is or starts with it.
func tildeAll(s, home string) string {
	if home == "" {
		return s
	}
	words := strings.Split(s, " ")
	for i, w := range words {
		if w == home || strings.HasPrefix(w, home+"/") {
			words[i] = "~" + w[len(home):]
		}
	}
	return strings.Join(words, " ")
}
