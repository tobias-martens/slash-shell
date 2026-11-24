// Package probe answers "is this package available / installed?" from local
// data only. It never touches the network: every command used here reads
// local databases, and flatpak and snap catalogs are read from disk directly
// (`flatpak search` and `snap find` would go online).
package probe

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner runs a command and returns its stdout and whether it exited 0.
type Runner func(name string, args ...string) (stdout string, ok bool)

// Probe implements resolve.Probe.
type Probe struct {
	Run       Runner
	ReadFile  func(string) ([]byte, error)
	Glob      func(string) ([]string, error) // nil: assume local data exists
	Flatpak   *FlatpakCatalog
	SnapNames string // path of snapd's local list of snap names
}

// New returns a Probe for the real system.
func New() *Probe {
	home, _ := os.UserHomeDir()
	cache, _ := os.UserCacheDir()
	return &Probe{
		Run:      ExecRunner(1500 * time.Millisecond),
		ReadFile: os.ReadFile,
		Glob:     filepath.Glob,
		Flatpak: &FlatpakCatalog{
			Globs: []string{
				"/var/lib/flatpak/appstream/*/*/active/appstream.xml",
				filepath.Join(home, ".local/share/flatpak/appstream/*/*/active/appstream.xml"),
			},
			CacheDir: filepath.Join(cache, "slash"),
		},
		SnapNames: "/var/cache/snapd/names",
	}
}

// ExecRunner runs real commands with a timeout and a C locale.
func ExecRunner(timeout time.Duration) Runner {
	return func(name string, args ...string) (string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		out, err := cmd.Output()
		return string(out), err == nil
	}
}

// Available implements resolve.Probe.
func (p *Probe) Available(source, pkg string) (string, bool) {
	switch source {
	case "apt":
		// --no-generate: use the existing cache instead of rebuilding it in
		// memory (800 ms → 20 ms; it is refreshed by every `update`). No
		// --no-all-versions: computing version priorities costs another 140 ms.
		_, ok := p.aptCache("show", "--", pkg)
		return pkg, ok
	case "dnf":
		if strings.HasPrefix(pkg, "-") {
			return pkg, false // dnf 5 doesn't accept "--"
		}
		_, ok := p.Run("dnf", "--cacheonly", "--quiet", "info", pkg)
		return pkg, ok
	case "pacman":
		_, ok := p.Run("pacman", "-Si", "--", pkg)
		return pkg, ok
	case "zypper":
		_, ok := p.Run("zypper", "--no-refresh", "--quiet", "search", "--match-exact", "--type", "package", pkg)
		return pkg, ok
	case "apk":
		out, ok := p.Run("apk", "search", "--exact", pkg)
		return pkg, ok && strings.TrimSpace(out) != ""
	case "flatpak":
		return p.Flatpak.Find(pkg)
	case "snap":
		return pkg, p.snapNameExists(pkg)
	}
	return "", false
}

// aptCache runs apt-cache on the existing binary cache (--no-generate:
// 20 ms instead of 800). `apt clean` deletes that cache, though; then
// apt-cache rebuilds it from the package lists, slower but still offline.
func (p *Probe) aptCache(args ...string) (string, bool) {
	out, ok := p.Run("apt-cache", append([]string{"--no-generate"}, args...)...)
	if ok || out != "" {
		return out, ok
	}
	return p.Run("apt-cache", args...)
}

// Installed implements resolve.Probe.
func (p *Probe) Installed(source, pkg string) (string, bool) {
	switch source {
	case "apt":
		out, ok := p.Run("dpkg-query", "-W", "-f=${db:Status-Abbrev}", "--", pkg)
		return pkg, ok && strings.HasPrefix(out, "ii")
	case "dnf", "zypper":
		_, ok := p.Run("rpm", "-q", "--", pkg)
		return pkg, ok
	case "pacman":
		_, ok := p.Run("pacman", "-Q", "--", pkg)
		return pkg, ok
	case "apk":
		_, ok := p.Run("apk", "info", "--installed", pkg)
		return pkg, ok
	case "flatpak":
		out, ok := p.Run("flatpak", "list", "--app", "--columns=application,name")
		if !ok {
			return "", false
		}
		var apps []App
		for _, line := range strings.Split(out, "\n") {
			id, name, _ := strings.Cut(line, "\t")
			if id != "" {
				apps = append(apps, App{ID: id, Name: name})
			}
		}
		return match(apps, pkg)
	case "snap":
		_, ok := p.Run("snap", "list", "--", pkg)
		return pkg, ok
	}
	return "", false
}

func (p *Probe) snapNameExists(pkg string) bool {
	data, err := p.ReadFile(p.SnapNames)
	if err != nil {
		return false
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if sc.Text() == pkg {
			return true
		}
	}
	return false
}
