// Package intro renders the screen shown when `slash` runs without arguments.
package intro

import (
	"fmt"
	"strings"

	"github.com/tobias-martens/slash-shell/internal/sources"
)

type example struct{ cmd, what string }

// Render returns the intro text, adapted to the detected system.
func Render(sys sources.System, version string) string {
	var b strings.Builder
	b.WriteString("slash: simple and natural commands for your terminal.\n")
	b.WriteString("Every command shows what it really runs, so you learn as you go.\n")
	if strings.HasPrefix(version, "0.") {
		fmt.Fprintf(&b, "(version %s: in development, not every command below works yet)\n", version)
	}
	b.WriteString("\n")

	ex := examples(sys)
	width := 0
	for _, e := range ex {
		width = max(width, len(e.cmd))
	}
	for _, e := range ex {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, e.cmd, e.what)
	}
	return b.String()
}

func examples(sys sources.System) []example {
	var ex []example

	if all := updateSources(sys); len(all) > 0 {
		ex = append(ex, example{"update", "update everything (" + strings.Join(all, ", ") + ")"})
	}
	if def := sys.InstallDefault(); def != "" {
		ex = append(ex, example{"install vlc", "install a program"})
		// Show "as <source>" only for a source that isn't already the default.
		switch {
		case sys.Flatpak && def != "flatpak":
			ex = append(ex, example{"install firefox as flatpak", "…from a specific source"})
		case sys.Snap && def != "snap":
			ex = append(ex, example{"install firefox as snap", "…from a specific source"})
		}
		ex = append(ex, example{"remove spotify", "uninstall a program"})
	}
	ex = append(ex,
		example{"search holiday", "find files and programs"},
		example{"show disk space", "also: memory, ip, ports, programs, services, …"},
		example{"stop firefox", "stop a program or service"},
		example{"unpack photos.zip", "unpack any archive"},
		example{"delete old-notes.txt", "move a file to the trash"},
	)
	if sys.InstallDefault() != "" {
		ex = append(ex, example{"clean up", "free space from packages and caches"})
	}
	return ex
}

// updateSources lists what `update` touches, in the order it runs them.
// On immutable systems the base system is updated by the OS's own updater.
func updateSources(sys sources.System) []string {
	var all []string
	if sys.Manager != "" && !sys.Immutable {
		all = append(all, sys.Manager)
	}
	if sys.Flatpak {
		all = append(all, "flatpak")
	}
	if sys.Snap {
		all = append(all, "snap")
	}
	return all
}
