package probe

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// App is a flatpak application from the catalog.
type App struct{ ID, Name, Summary, Homepage, Version string }

// FlatpakCatalog reads the appstream catalogs flatpak keeps on disk. Parsing
// a catalog (tens of MB of XML) is slow, so a small index is cached and
// rebuilt whenever the catalog file changes.
type FlatpakCatalog struct {
	Globs    []string
	CacheDir string // "" disables caching
}

// Find returns the app ID for pkg, matched case-insensitively against the
// full ID, the last part of the ID (org.mozilla.firefox → firefox) or the
// display name.
func (c *FlatpakCatalog) Find(pkg string) (string, bool) {
	a, ok := c.Lookup(pkg)
	return a.ID, ok
}

// Lookup is Find, returning the whole catalog entry.
func (c *FlatpakCatalog) Lookup(pkg string) (App, bool) {
	apps, _ := c.Apps()
	id, ok := match(apps, pkg)
	for _, a := range apps {
		if ok && a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// Apps returns every app in the local catalogs; ok is false if there are
// no catalogs at all (flatpak hasn't downloaded any yet).
func (c *FlatpakCatalog) Apps() (apps []App, ok bool) {
	seen := map[string]bool{}
	for _, g := range c.Globs {
		paths, _ := filepath.Glob(g)
		for _, path := range paths {
			ok = true
			for _, a := range c.load(path) {
				if !seen[a.ID] { // the same app can be in several remotes
					seen[a.ID] = true
					apps = append(apps, a)
				}
			}
		}
	}
	return apps, ok
}

// match prefers an exact ID, then the last ID part, then the display name.
func match(apps []App, pkg string) (string, bool) {
	want := strings.ToLower(pkg)
	for _, rank := range []func(App) string{
		func(a App) string { return strings.ToLower(a.ID) },
		func(a App) string { return strings.ToLower(a.ID[strings.LastIndexByte(a.ID, '.')+1:]) },
		func(a App) string { return strings.ToLower(a.Name) },
	} {
		for _, a := range apps {
			if rank(a) == want {
				return a.ID, true
			}
		}
	}
	return "", false
}

func (c *FlatpakCatalog) load(path string) []App {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var cache string
	if c.CacheDir != "" {
		cache = filepath.Join(c.CacheDir, fmt.Sprintf("flatpak%d-%s-%s.tsv", indexVersion,
			short(path), short(fmt.Sprintf("%d|%d", st.Size(), st.ModTime().UnixNano()))))
		if apps, ok := readIndex(cache); ok {
			return apps
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	apps := scanAppstream(f)
	if cache != "" {
		writeIndex(cache, apps)
	}
	return apps
}

// indexVersion changes whenever the index's columns change.
const indexVersion = 2

// scanAppstream extracts desktop applications: ID, and untranslated name,
// summary, homepage, and latest release version.
// A line scanner instead of a full XML parser: much faster, and appstream
// files written by flatpak keep id and name elements on their own lines.
func scanAppstream(f *os.File) []App {
	var apps []App
	var cur App
	inApp := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		for _, line := range splitTags(sc.Text()) {
			apps, cur, inApp = step(apps, cur, inApp, line)
		}
	}
	return apps
}

// splitTags splits lines like "</component><component …>" that flatpak
// sometimes writes, so each component boundary is on its own line.
func splitTags(line string) []string {
	if !strings.Contains(line, "component") {
		return []string{strings.TrimSpace(line)}
	}
	line = strings.ReplaceAll(line, "</component>", "\n</component>\n")
	line = strings.ReplaceAll(line, "<component ", "\n<component ")
	line = strings.ReplaceAll(line, "<component>", "\n<component>")
	var out []string
	for _, l := range strings.Split(line, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

var appTypes = []string{`type="desktop-application"`, `type="desktop"`, `type="console-application"`}

func step(apps []App, cur App, inApp bool, line string) ([]App, App, bool) {
	switch {
	case strings.HasPrefix(line, "<component"):
		inApp = false
		for _, t := range appTypes {
			inApp = inApp || strings.Contains(line, t)
		}
		cur = App{}
	case line == "</component>":
		if inApp && cur.ID != "" {
			apps = append(apps, cur)
		}
		inApp = false
	case inApp && cur.ID == "" && strings.HasPrefix(line, "<id>"):
		cur.ID = strings.TrimSuffix(inner(line), ".desktop")
	case inApp && cur.Name == "" && strings.HasPrefix(line, "<name>"):
		cur.Name = unescape(inner(line))
	case inApp && cur.Summary == "" && strings.HasPrefix(line, "<summary>"):
		cur.Summary = unescape(inner(line))
	case inApp && cur.Homepage == "" && strings.HasPrefix(line, `<url type="homepage">`):
		cur.Homepage = unescape(inner(line))
	case inApp && cur.Version == "" && strings.HasPrefix(line, "<release "):
		cur.Version = attr(line, "version")
	}
	return apps, cur, inApp
}

func short(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func inner(line string) string {
	i := strings.IndexByte(line, '>')
	j := strings.LastIndexByte(line, '<')
	if i < 0 || j <= i {
		return ""
	}
	return line[i+1 : j]
}

// attr returns the value of name="…" in a tag.
func attr(tag, name string) string {
	_, rest, ok := strings.Cut(tag, " "+name+`="`)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, `"`)
	return unescape(v)
}

var xmlEntities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'")

func unescape(s string) string { return xmlEntities.Replace(s) }

func readIndex(path string) ([]App, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var apps []App
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 5 {
			apps = append(apps, App{ID: f[0], Name: f[1], Summary: f[2], Homepage: f[3], Version: f[4]})
		}
	}
	return apps, true
}

func writeIndex(path string, apps []App) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// Drop indexes of older versions of the same catalog (same path hash).
	base := filepath.Base(path)
	prefix := base[:strings.LastIndexByte(base, '-')+1]
	old, _ := filepath.Glob(filepath.Join(filepath.Dir(path), prefix+"*.tsv"))
	var b strings.Builder
	clean := strings.NewReplacer("\t", " ", "\n", " ")
	for _, a := range apps {
		b.WriteString(strings.Join([]string{a.ID, clean.Replace(a.Name), clean.Replace(a.Summary),
			clean.Replace(a.Homepage), clean.Replace(a.Version)}, "\t") + "\n")
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(b.String()), 0o644) == nil && os.Rename(tmp, path) == nil {
		for _, o := range old {
			if o != path {
				os.Remove(o)
			}
		}
	}
}
