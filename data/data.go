// Package data embeds slash's command mappings.
//
// sources/<name>.tsv maps keys to command templates, one per line:
//
//	key<TAB>template
//
// A key may carry an os-release ID suffix ("update@opensuse-tumbleweed") that
// wins over the plain key on that OS. In templates, {sudo} becomes "sudo "
// unless running as root, and {pkgs} the shell-quoted package names.
package data

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed sources/*.tsv
var files embed.FS

// Templates maps source → key → template.
type Templates map[string]map[string]string

// Load parses all embedded mapping files.
func Load() (Templates, error) {
	entries, err := files.ReadDir("sources")
	if err != nil {
		return nil, err
	}
	t := Templates{}
	for _, e := range entries {
		source := strings.TrimSuffix(e.Name(), ".tsv")
		raw, err := files.ReadFile("sources/" + e.Name())
		if err != nil {
			return nil, err
		}
		m := map[string]string{}
		for i, line := range strings.Split(string(raw), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, tmpl, ok := strings.Cut(line, "\t")
			if !ok || key == "" || tmpl == "" {
				return nil, fmt.Errorf("%s:%d: want key<TAB>template", e.Name(), i+1)
			}
			m[key] = tmpl
		}
		t[source] = m
	}
	return t, nil
}

// Lookup returns the template for source and key, preferring the variant for
// osID. ok is false if the source has no such key.
func (t Templates) Lookup(source, key, osID string) (tmpl string, ok bool) {
	m := t[source]
	if osID != "" {
		if tmpl, ok = m[key+"@"+osID]; ok {
			return tmpl, true
		}
	}
	tmpl, ok = m[key]
	return tmpl, ok
}

//go:embed show.tsv
var showFile string

// ShowEntry is one thing `show` can show.
type ShowEntry struct {
	Phrases  []string // synonyms, first is the main one
	Target   string   // none, folder, port, service
	Needs    []string // programs that must be installed
	Command  string
	Internet string // if set: why this uses the internet (asks before running)
}

// LoadShow parses show.tsv.
func LoadShow() ([]ShowEntry, error) {
	var out []ShowEntry
	for i, line := range strings.Split(showFile, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 || len(f) > 5 {
			return nil, fmt.Errorf("show.tsv:%d: want 4 or 5 tab-separated columns", i+1)
		}
		e := ShowEntry{Phrases: strings.Split(f[0], "|"), Target: f[1], Command: f[3]}
		if f[2] != "-" {
			e.Needs = strings.Split(f[2], ",")
		}
		if len(f) == 5 {
			e.Internet = f[4]
		}
		switch e.Target {
		case "none", "folder", "port", "service":
		default:
			return nil, fmt.Errorf("show.tsv:%d: unknown target %q", i+1, e.Target)
		}
		out = append(out, e)
	}
	return out, nil
}
