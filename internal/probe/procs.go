package probe

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Proc is one process, read from /proc.
type Proc struct {
	PID, PPID, UID int
	Comm           string // kernel's short name (max 15 characters)
	Arg0           string // base name of the program as started ("" for kernel threads)
	Scope          string // last part of its cgroup, e.g. "snap.firefox.firefox-….scope"
	State          byte   // R, S, Z, …
	RSS            int64  // resident memory in pages
}

// ReadProcs reads all processes from the proc file system at root ("/proc").
func ReadProcs(root string) []Proc {
	entries, _ := os.ReadDir(root)
	var out []Proc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if p, ok := readProc(root, pid); ok {
			out = append(out, p)
		}
	}
	return out
}

func readProc(root string, pid int) (Proc, bool) {
	dir := filepath.Join(root, strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return Proc{}, false
	}
	// "pid (comm) state ppid …": comm may contain spaces and parentheses.
	s := string(stat)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return Proc{}, false
	}
	p := Proc{PID: pid, Comm: s[open+1 : end]}
	f := strings.Fields(s[end+1:])
	if len(f) < 22 {
		return Proc{}, false
	}
	p.State = f[0][0]
	p.PPID, _ = strconv.Atoi(f[1])
	p.RSS, _ = strconv.ParseInt(f[21], 10, 64)
	if st, err := os.Stat(dir); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			p.UID = int(sys.Uid)
		}
	}
	if cmd, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(cmd) > 0 {
		arg0, _, nul := strings.Cut(string(cmd), "\x00")
		if !nul || strings.Count(string(cmd), "\x00") == 1 {
			// Some programs (Chromium) rewrite their command line as one
			// space-separated string.
			arg0, _, _ = strings.Cut(arg0, " ")
		}
		p.Arg0 = strings.TrimPrefix(filepath.Base(arg0), "-") // "-bash" for login shells
	}
	if cg, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
		for _, line := range strings.Split(string(cg), "\n") {
			if path, ok := strings.CutPrefix(line, "0::"); ok {
				p.Scope = filepath.Base(path)
			}
		}
	}
	return p, true
}

// Alive reports whether pid still runs (zombies count as gone).
func Alive(root string, pid int) bool {
	p, ok := readProc(root, pid)
	return ok && p.State != 'Z' && p.State != 'X'
}

// Program is something a person would call a running program: an app
// window, or a command started in a terminal.
type Program struct {
	Name    string
	Aliases []string // other names it answers to (snap name, app ID part, kernel name)
	PIDs    []int    // processes to stop: the program's main processes
	RSS     int64
}

var shells = []string{"bash", "zsh", "fish", "sh", "dash", "ksh", "mksh", "tcsh", "csh", "nu"}

// Programs groups uid's processes into programs, biggest first. Processes
// of the desktop session itself (services, the session scope) aren't
// programs; neither are self and the processes it runs inside (its shell).
func Programs(all []Proc, uid, self int) []Program {
	byPID := map[int]Proc{}
	for _, p := range all {
		byPID[p.PID] = p
	}
	skip := map[int]bool{}
	for pid := self; pid > 1 && !skip[pid]; pid = byPID[pid].PPID {
		skip[pid] = true
	}
	var mine []Proc
	scoped := false
	for _, p := range all {
		if p.UID == uid && p.Arg0 != "" && !skip[p.PID] {
			mine = append(mine, p)
			scoped = scoped || strings.HasSuffix(p.Scope, ".scope")
		}
	}

	var progs []Program
	// add records the program whose main process is p; more are extra
	// processes of the same app (crash handlers, …), stopped along with it.
	add := func(p Proc, rss int64, scope string, more ...Proc) {
		name := cmpOr(p.Arg0, p.Comm)
		aliases := []string{p.Comm}
		if a := scopeName(scope); a != "" {
			// A snap or flatpak is known by its own name: chromium, not chrome.
			name, aliases = a, append(aliases, name)
		}
		pids := []int{p.PID}
		for _, m := range more {
			pids = append(pids, m.PID)
		}
		for i := range progs {
			if progs[i].Name == name {
				progs[i].PIDs = append(progs[i].PIDs, pids...)
				progs[i].RSS += rss
				return
			}
		}
		progs = append(progs, Program{Name: name, Aliases: aliases, PIDs: pids, RSS: rss})
	}

	if !scoped {
		// No per-app cgroups (no systemd user session): every process
		// that isn't a shell or a helper of a same-named parent.
		for _, p := range mine {
			parent := byPID[p.PPID]
			if !isShell(p) && cmpOr(parent.Arg0, parent.Comm) != cmpOr(p.Arg0, p.Comm) {
				add(p, p.RSS, "")
			}
		}
	} else {
		groups := map[string][]Proc{}
		for _, p := range mine {
			if appScope(p.Scope) {
				groups[p.Scope] = append(groups[p.Scope], p)
			}
		}
		for scope, ps := range groups {
			in := map[int]bool{}
			var total int64
			for _, p := range ps {
				in[p.PID] = true
				total += p.RSS
			}
			var roots []Proc
			for _, p := range ps {
				if in[p.PPID] {
					continue
				}
				if isShell(p) {
					// A terminal: the programs are what its shells run.
					for _, c := range commandsUnder(p, ps) {
						add(c, c.RSS, "")
					}
					continue
				}
				roots = append(roots, p)
			}
			if !strings.HasPrefix(scope, "app-") && !strings.HasPrefix(scope, "snap.") {
				// Not an app's own scope (a terminal, …): each root is a program.
				for _, r := range roots {
					add(r, r.RSS, scope)
				}
				continue
			}
			if len(roots) > 0 {
				// One app per scope; its main process has the most children.
				main := slices.MaxFunc(roots, func(a, b Proc) int {
					return descendants(a, ps) - descendants(b, ps)
				})
				var others []Proc
				for _, r := range roots {
					if r.PID != main.PID {
						others = append(others, r)
					}
				}
				add(main, total, scope, others...)
			}
		}
	}
	slices.SortFunc(progs, func(a, b Program) int {
		if a.RSS != b.RSS {
			return int(b.RSS - a.RSS)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return progs
}

// commandsUnder returns the non-shell processes run by shell (directly, or
// through further shells).
func commandsUnder(shell Proc, ps []Proc) []Proc {
	var out []Proc
	for _, p := range ps {
		if p.PPID != shell.PID {
			continue
		}
		if isShell(p) {
			out = append(out, commandsUnder(p, ps)...)
		} else {
			out = append(out, p)
		}
	}
	return out
}

func descendants(p Proc, ps []Proc) int {
	n := 0
	for _, c := range ps {
		if c.PPID == p.PID {
			n += 1 + descendants(c, ps)
		}
	}
	return n
}

func isShell(p Proc) bool { return slices.Contains(shells, cmpOr(p.Arg0, p.Comm)) }

// appScope: scopes of things the user started (apps, terminals), not of
// session services (*.service), the session itself or the user manager.
func appScope(scope string) bool {
	return strings.HasSuffix(scope, ".scope") && scope != "init.scope" && !strings.HasPrefix(scope, "session-")
}

// scopeName derives a program name from its scope:
// snap.firefox.firefox-<uuid>.scope → firefox,
// app-flatpak-org.mozilla.firefox-12345.scope → firefox.
// Other launchers' scopes (app-gnome-…) are named after .desktop files,
// which often differ from the program name; those give no name.
func scopeName(scope string) string {
	s := strings.TrimSuffix(scope, ".scope")
	if rest, ok := strings.CutPrefix(s, "snap."); ok {
		name, _, _ := strings.Cut(rest, ".")
		return name
	}
	rest, ok := strings.CutPrefix(s, "app-")
	if !ok {
		return ""
	}
	launcher, id, ok := strings.Cut(rest, "-")
	if !ok || launcher != "flatpak" {
		return ""
	}
	if i := strings.LastIndexByte(id, '-'); i > 0 {
		id = id[:i] // drop the instance number or random suffix
	}
	id, _, _ = strings.Cut(id, "@")
	id = strings.ReplaceAll(id, `\x2d`, "-")
	return strings.ToLower(id[strings.LastIndexByte(id, '.')+1:])
}

// FindProgram finds the program called name (case-insensitive).
func FindProgram(progs []Program, name string) (Program, bool) {
	for _, p := range progs {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	for _, p := range progs {
		for _, a := range p.Aliases {
			if strings.EqualFold(a, name) {
				return p, true
			}
		}
	}
	return Program{}, false
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
