package intro

import (
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/internal/sources"
)

func TestRenderAdaptsToSystem(t *testing.T) {
	tests := []struct {
		name        string
		sys         sources.System
		want, avoid []string
	}{
		{
			name:  "ubuntu with flatpak and snap",
			sys:   sources.System{Manager: "apt", Flatpak: true, Snap: true},
			want:  []string{"update everything (apt, flatpak, snap)", "install firefox as flatpak", "clean up"},
			avoid: []string{"as snap"},
		},
		{
			name:  "fedora without snap",
			sys:   sources.System{Manager: "dnf", Flatpak: true},
			want:  []string{"update everything (dnf, flatpak)"},
			avoid: []string{"snap"},
		},
		{
			name:  "snap but no flatpak",
			sys:   sources.System{Manager: "apt", Snap: true},
			want:  []string{"install firefox as snap"},
			avoid: []string{"flatpak"},
		},
		{
			name:  "steamos: no system updates, flatpak is already the default",
			sys:   sources.System{Manager: "pacman", Immutable: true, Flatpak: true},
			want:  []string{"update everything (flatpak)", "install vlc"},
			avoid: []string{"pacman", "as flatpak"},
		},
		{
			name:  "no package sources at all",
			sys:   sources.System{},
			want:  []string{"search holiday", "show disk space"},
			avoid: []string{"update", "install", "clean up"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Render(tt.sys, "1.0.0")
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, a := range tt.avoid {
				if strings.Contains(out, a) {
					t.Errorf("unexpected %q in:\n%s", a, out)
				}
			}
		})
	}
}

func TestRenderDevNote(t *testing.T) {
	if !strings.Contains(Render(sources.System{}, "0.1.0"), "in development") {
		t.Error("0.x versions should say they are in development")
	}
	if strings.Contains(Render(sources.System{}, "1.0.0"), "in development") {
		t.Error("1.x versions should not carry the development note")
	}
}
