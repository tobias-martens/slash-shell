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
