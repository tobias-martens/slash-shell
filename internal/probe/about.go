package probe

import (
	"strings"
)

// Info is what one source knows about a package, from local data only.
type Info struct {
	Source    string
	Name      string // name in that source (flatpak: app ID)
	Title     string // display name, if the source has one
	Version   string // version available
	Installed string // installed version; "" if not installed
	Summary   string
	Homepage  string
	How       []string // the real commands (or files) it was read from
}

// About reports what source knows about pkg; ok is false if the source
// doesn't have it.
func (p *Probe) About(source, pkg string) (info Info, ok bool) {
	info = Info{Source: source, Name: pkg}
	switch source {
	case "apt":
		info.How = []string{"apt-cache show -- " + pkg, "dpkg-query -W -- " + pkg}
		out, _ := p.aptCache("show", "--", pkg)
		f := fields(firstStanza(out), ":")
		if f["Package"] == "" {
			return info, false
		}
		info.Version, info.Homepage = f["Version"], f["Homepage"]
		info.Summary = firstNonEmpty(f["Description-en"], f["Description"])
		inst, _ := p.Run("dpkg-query", "-W", "-f=${db:Status-Abbrev}\t${Version}", "--", pkg)
		if status, version, _ := strings.Cut(inst, "\t"); strings.HasPrefix(status, "ii") {
			info.Installed = version
		}
	case "dnf":
		if strings.HasPrefix(pkg, "-") {
			return info, false // dnf 5 doesn't accept "--": never pass an option
		}
		args := []string{"--cacheonly", "--quiet", "info", pkg}
		info.How = []string{"dnf " + strings.Join(args, " "), "rpm -q -- " + pkg}
		out, _ := p.Run("dnf", args...)
		f := fields(out, ":")
		if f["Name"] == "" {
			return info, false
		}
		info.Version = joinVersion(f["Version"], f["Release"])
		info.Summary, info.Homepage = f["Summary"], f["URL"]
		info.Installed = p.rpmVersion(pkg)
	case "zypper":
		args := []string{"--no-refresh", "--quiet", "info", "--", pkg}
		info.How = []string{"zypper " + strings.Join(args, " "), "rpm -q -- " + pkg}
		out, _ := p.Run("zypper", args...)
		f := fields(out, ":")
		if f["Name"] == "" {
			return info, false
		}
		info.Version, info.Summary = f["Version"], f["Summary"]
		info.Installed = p.rpmVersion(pkg)
	case "pacman":
		info.How = []string{"pacman -Si -- " + pkg, "pacman -Q -- " + pkg}
		out, _ := p.Run("pacman", "-Si", "--", pkg)
		f := fields(firstStanza(out), ":")
		if f["Name"] == "" {
			return info, false
		}
		info.Version, info.Summary, info.Homepage = f["Version"], f["Description"], f["URL"]
		if q, ok := p.Run("pacman", "-Q", "--", pkg); ok {
			if fs := strings.Fields(q); len(fs) == 2 {
				info.Installed = fs[1]
			}
		}
	case "apk":
		info.How = []string{"apk search -v -x -- " + pkg, "apk info -e -- " + pkg}
		out, _ := p.Run("apk", "search", "-v", "-x", "--", pkg)
		for _, line := range lines(out) {
			nv, summary, _ := strings.Cut(line, " - ")
			if m := apkVersion.FindStringSubmatch(nv); m != nil && m[1] == pkg {
				info.Version, info.Summary = strings.TrimPrefix(nv, pkg+"-"), summary
			}
		}
		if info.Version == "" {
			return info, false
		}
		if _, ok := p.Run("apk", "info", "-e", "--", pkg); ok {
			info.Installed = "yes"
		}
	case "flatpak":
		info.How = []string{"flatpak's local catalog", "flatpak list --app"}
		a, found := p.Flatpak.Lookup(pkg)
		if !found {
			return info, false
		}
		info.Name, info.Title, info.Summary, info.Homepage, info.Version = a.ID, a.Name, a.Summary, a.Homepage, a.Version
		out, _ := p.Run("flatpak", "list", "--app", "--columns=application,version")
		for _, line := range lines(out) {
			if id, version, _ := strings.Cut(line, "\t"); id == a.ID {
				info.Installed = firstNonEmpty(version, "yes")
			}
		}
	case "snap":
		// Details of snaps that aren't installed are only in the online
		// store (snap info), so slash only knows the name.
		info.How = []string{"snapd's local list of names", "snap list -- " + pkg}
		out, ok := p.Run("snap", "list", "--", pkg)
		for _, line := range lines(out)[min(1, len(lines(out))):] {
			if f := strings.Fields(line); ok && len(f) >= 2 && f[0] == pkg {
				info.Installed = f[1]
			}
		}
		if info.Installed == "" && !p.snapNameExists(pkg) {
			return info, false
		}
	default:
		return info, false
	}
	return info, true
}

func (p *Probe) rpmVersion(pkg string) string {
	out, ok := p.Run("rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", "--", pkg)
	if !ok {
		return ""
	}
	return strings.TrimSpace(out)
}

// fields parses "Key: value" / "Key  : value" lines; the first value wins.
func fields(text, sep string) map[string]string {
	f := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // continuation lines
		}
		k, v, ok := strings.Cut(line, sep)
		k = strings.TrimSpace(k)
		if ok && k != "" && f[k] == "" {
			f[k] = strings.TrimSpace(v)
		}
	}
	return f
}

// firstStanza returns text up to the first blank line.
func firstStanza(text string) string {
	text = strings.TrimLeft(text, "\n")
	if i := strings.Index(text, "\n\n"); i >= 0 {
		return text[:i]
	}
	return text
}

func joinVersion(v, r string) string {
	if v == "" || r == "" {
		return v
	}
	return v + "-" + r
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
