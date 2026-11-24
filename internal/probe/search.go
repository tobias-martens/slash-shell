package probe

import (
	"bufio"
	"errors"
	"regexp"
	"slices"
	"strings"
)

// Match is one package found by Search.
type Match struct {
	Name      string // what to type: install <Name>
	ID        string // the source's own ID, if different (flatpak app ID)
	Summary   string
	Installed bool
}

// ErrNoData means the source has no local package data yet. slash never
// downloads it for a search: the user runs `update`.
var ErrNoData = errors.New("no local package data yet")

// Search finds packages of source whose name contains every word
// (case-insensitive), from local data only. It returns the best limit
// matches, the total count, and the real command it ran, for display.
func (p *Probe) Search(source string, words []string, limit int) (top []Match, total int, how string, err error) {
	var all []Match
	switch source {
	case "apt":
		all, how, err = p.searchApt(words)
	case "dnf":
		all, how, err = p.searchDnf(words)
	case "pacman":
		all, how, err = p.searchPacman(words)
	case "zypper":
		all, how, err = p.searchZypper(words)
	case "apk":
		all, how, err = p.searchApk(words)
	case "flatpak":
		all, how, err = p.searchFlatpak(words)
	case "snap":
		all, how, err = p.searchSnap(words)
	default:
		return nil, 0, "", errors.New("unknown source " + source)
	}
	if err != nil {
		return nil, 0, how, err
	}
	all = filterRank(all, words)
	total = len(all)
	if len(all) > limit {
		all = all[:limit]
	}
	p.markInstalled(source, all)
	return all, total, how, nil
}

// filterRank keeps matches whose name (or flatpak ID) contains every word,
// best first: exact name, then names with the words as whole parts
// ("text-editor" for "editor"), then names starting with them, then the
// rest; shorter names first within each group.
func filterRank(ms []Match, words []string) []Match {
	lower := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
	}
	var out []Match
	seen := map[string]bool{}
	for _, m := range ms {
		hay := strings.ToLower(m.Name + " " + m.ID)
		ok := !seen[m.Name+"\x00"+m.ID]
		for _, w := range lower {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			seen[m.Name+"\x00"+m.ID] = true
			out = append(out, m)
		}
	}
	query := strings.Join(lower, " ")
	rank := func(m Match) int {
		n := strings.ToLower(m.Name)
		parts := strings.FieldsFunc(n, func(r rune) bool { return strings.ContainsRune(" -_.+", r) })
		whole := true
		for _, w := range lower {
			whole = whole && slices.Contains(parts, w)
		}
		switch {
		case n == query:
			return 0
		case whole:
			return 1
		case strings.HasPrefix(n, lower[0]):
			return 2
		}
		return 3
	}
	slices.SortStableFunc(out, func(a, b Match) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if d := len(a.Name) - len(b.Name); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// has reports whether any file matches pattern (true when Glob isn't set).
func (p *Probe) has(pattern string) bool {
	if p.Glob == nil {
		return true
	}
	m, _ := p.Glob(pattern)
	return len(m) > 0
}

func regexWords(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = regexp.QuoteMeta(w)
	}
	return out
}

func (p *Probe) searchApt(words []string) ([]Match, string, error) {
	args := append([]string{"search", "--names-only", "--"}, regexWords(words)...)
	how := "apt-cache " + strings.Join(args, " ")
	if !p.has("/var/lib/apt/lists/*_Packages*") {
		return nil, how, ErrNoData
	}
	out, ok := p.aptCache(args...)
	if !ok && out == "" {
		return nil, how, ErrNoData
	}
	var ms []Match
	for _, line := range lines(out) {
		if name, summary, ok := strings.Cut(line, " - "); ok {
			ms = append(ms, Match{Name: name, Summary: summary})
		}
	}
	return ms, how, nil
}

// dnf 4: "vim-enhanced.x86_64 : summary"; dnf 5: " vim-enhanced.x86_64\tsummary".
var dnfLine = regexp.MustCompile(`^\s*(\S+)\.(?:x86_64|noarch|i686|aarch64|ppc64le|s390x|armv7hl|riscv64)\s*(?::\s|\t)\s*(.*)$`)

func (p *Probe) searchDnf(words []string) ([]Match, string, error) {
	// dnf 5 rejects "--", so words that look like options are left out
	// (filterRank still applies every word to the results).
	args := []string{"--cacheonly", "--quiet", "search"}
	for _, w := range words {
		if !strings.HasPrefix(w, "-") {
			args = append(args, w)
		}
	}
	how := "dnf " + strings.Join(args, " ")
	out, ok := p.Run("dnf", args...)
	if !ok && out == "" {
		return nil, how, ErrNoData
	}
	var ms []Match
	for _, line := range lines(out) {
		if m := dnfLine.FindStringSubmatch(line); m != nil {
			ms = append(ms, Match{Name: m[1], Summary: strings.TrimSpace(m[2])})
		}
	}
	return ms, how, nil
}

// pacman -Ss: "extra/vim 9.1.0-1 [installed]" then an indented summary line.
func (p *Probe) searchPacman(words []string) ([]Match, string, error) {
	args := append([]string{"-Ss", "--"}, regexWords(words)...)
	how := "pacman " + strings.Join(args, " ")
	if !p.has("/var/lib/pacman/sync/*.db") {
		return nil, how, ErrNoData
	}
	out, _ := p.Run("pacman", args...) // exits 1 when nothing matches
	var ms []Match
	for _, line := range lines(out) {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(ms) > 0 && ms[len(ms)-1].Summary == "" {
				ms[len(ms)-1].Summary = strings.TrimSpace(line)
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		_, name, _ := strings.Cut(f[0], "/")
		ms = append(ms, Match{Name: name, Installed: strings.Contains(line, "[installed")})
	}
	return ms, how, nil
}

// zypper search: a table "S | Name | Summary | Type".
func (p *Probe) searchZypper(words []string) ([]Match, string, error) {
	args := append([]string{"--no-refresh", "--quiet", "search", "--type", "package", "--"}, words...)
	how := "zypper " + strings.Join(args, " ")
	if !p.has("/var/cache/zypp/solv/*/solv") {
		return nil, how, ErrNoData
	}
	out, _ := p.Run("zypper", args...) // exits 104 when nothing matches
	var ms []Match
	for _, line := range lines(out) {
		f := strings.Split(line, "|")
		if len(f) < 3 {
			continue
		}
		status, name := strings.TrimSpace(f[0]), strings.TrimSpace(f[1])
		if name == "Name" || name == "" || strings.Trim(name, "-+") == "" {
			continue
		}
		ms = append(ms, Match{Name: name, Summary: strings.TrimSpace(f[2]), Installed: strings.HasPrefix(status, "i")})
	}
	return ms, how, nil
}

// apk search -v: "vim-9.1.0-r0 - Improved vi-style text editor".
var apkVersion = regexp.MustCompile(`^(.+)-\d[^-]*-r\d+$`)

func (p *Probe) searchApk(words []string) ([]Match, string, error) {
	args := append([]string{"search", "-v", "--"}, words...)
	how := "apk " + strings.Join(args, " ")
	out, _ := p.Run("apk", args...)
	var ms []Match
	for _, line := range lines(out) {
		nv, summary, _ := strings.Cut(line, " - ")
		if m := apkVersion.FindStringSubmatch(nv); m != nil {
			ms = append(ms, Match{Name: m[1], Summary: summary})
		}
	}
	return ms, how, nil
}

func (p *Probe) searchFlatpak(_ []string) ([]Match, string, error) {
	how := "flatpak's local catalog"
	apps, ok := p.Flatpak.Apps()
	if !ok {
		return nil, how, ErrNoData
	}
	ms := make([]Match, len(apps))
	for i, a := range apps {
		ms[i] = Match{Name: a.Name, ID: a.ID, Summary: a.Summary}
	}
	return ms, how, nil
}

func (p *Probe) searchSnap(_ []string) ([]Match, string, error) {
	how := "snapd's local list of names"
	data, err := p.ReadFile(p.SnapNames)
	if err != nil {
		return nil, how, ErrNoData
	}
	var ms []Match
	for _, name := range lines(string(data)) {
		ms = append(ms, Match{Name: name})
	}
	return ms, how, nil
}

// markInstalled sets Installed on ms, asking the package database once.
func (p *Probe) markInstalled(source string, ms []Match) {
	if len(ms) == 0 {
		return
	}
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	installed := map[string]bool{}
	switch source {
	case "apt":
		out, _ := p.Run("dpkg-query", append([]string{"-W", "-f=${Package}\t${db:Status-Abbrev}\n", "--"}, names...)...)
		for _, line := range lines(out) {
			if name, status, _ := strings.Cut(line, "\t"); strings.HasPrefix(status, "ii") {
				installed[name] = true
			}
		}
	case "dnf":
		out, _ := p.Run("rpm", append([]string{"-q", "--qf", "%{NAME}\n", "--"}, names...)...)
		for _, line := range lines(out) {
			installed[line] = true // "package x is not installed" never equals a name
		}
	case "apk":
		out, _ := p.Run("apk", append([]string{"info", "-e", "--"}, names...)...)
		for _, line := range lines(out) {
			installed[line] = true
		}
	case "flatpak":
		out, _ := p.Run("flatpak", "list", "--app", "--columns=application")
		for _, line := range lines(out) {
			installed[line] = true
		}
		for i := range ms {
			ms[i].Installed = installed[ms[i].ID]
		}
		return
	case "snap":
		out, _ := p.Run("snap", "list")
		for _, line := range lines(out) {
			if f := strings.Fields(line); len(f) > 0 {
				installed[f[0]] = true
			}
		}
	default: // pacman and zypper say it in their search output
		return
	}
	for i := range ms {
		ms[i].Installed = installed[ms[i].Name]
	}
}

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), " \r"); strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
