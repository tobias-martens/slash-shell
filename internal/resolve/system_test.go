package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

type fakeServices struct {
	units    map[string]string // name → scope
	active   map[string]bool
	programs map[string]bool
}

func (f fakeServices) Unit(n string) (string, bool) { s, ok := f.units[n]; return s, ok }
func (f fakeServices) Active(_, n string) bool      { return f.active[n] }
func (f fakeServices) Program(n string) bool        { return f.programs[n] }

func TestShowAndServices(t *testing.T) {
	show, err := data.LoadShow()
	if err != nil {
		t.Fatal(err)
	}
	installed := map[string]bool{"df": true, "du": true, "sort": true, "head": true, "journalctl": true,
		"ss": true, "systemctl": true, "curl": true, "firefox": true}
	svc := fakeServices{
		units:    map[string]string{"ssh": "system", "cups": "system", "pipewire": "user", "docker": "system", "idle": "system"},
		active:   map[string]bool{"ssh": true, "cups": true, "pipewire": true, "docker": true},
		programs: map[string]bool{"firefox": true, "docker": true},
	}
	sys := sources.System{Manager: "apt", Systemd: true}
	tests := []struct {
		sys     sources.System
		line    string
		want    string
		confirm bool
		errText string
	}{
		{sys, "show disk space", "df -h -x tmpfs -x devtmpfs", false, ""},
		{sys, "show Disk Usage", "df -h -x tmpfs -x devtmpfs", false, ""},
		{sys, "show size of Downloads", "du -sh -- Downloads", false, ""},
		{sys, "show size", "du -sh -- .", false, ""},
		{sys, "show biggest files in Videos", "du -ah -- Videos 2>/dev/null | sort -rh | head -n 20", false, ""},
		{sys, "show what uses port 8080", "ss -tulpn 'sport = :8080'", false, ""},
		{sys, "show what uses port http", "", false, "which port?"},
		{sys, "show logs of ssh", "journalctl -u ssh -e", false, ""},
		{sys, "show logs for pipewire.service", "journalctl --user-unit pipewire -e", false, ""},
		{sys, "show logs of nosuch", "", false, "no service called"},
		{sys, "show public ip", "curl -s https://ifconfig.me; echo", true, ""},
		{sys, "show memory", "", false, "needs free"},
		{sys, "show memory now", "", false, "works without anything"},
		{sys, "show cats", "", false, "I can show: disk space, size of <folder>"},
		{sys, "show", "", false, "show what?"},
		{sys, "start ssh", "sudo systemctl start ssh", false, ""},
		{sys, "restart ssh and pipewire", "sudo systemctl restart ssh; systemctl --user restart pipewire", false, ""},
		{sys, "stop cups", "sudo systemctl stop cups", false, ""},
		{sys, "stop idle", "", false, "isn't running"},
		{sys, "stop nothing", "", false, "nothing called"},
		{sys, "stop firefox", "slash stop firefox", false, ""},
		{sys, "stop cups and firefox", "slash stop cups and firefox", false, ""},
		{sys, "stop docker", "slash stop docker", false, ""}, // both: slash asks
		{sys, "stop", "slash stop", false, ""},
		{sys, "restart", "slash restart", false, ""},
		{sys, "start firefox", "", false, "is a program, not a service. To start it, type: firefox"},
		{sys, "restart firefox", "", false, "is a program, not a service"},
		{sys, "start x;y", "", false, "isn't a service name"},
		{sources.System{Manager: "apk"}, "start sshd", "", false, "only know systemd"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			p, err := grammar.Parse(strings.Fields(tt.line))
			if err != nil {
				t.Fatal(err)
			}
			r := &Resolver{Sys: tt.sys, Show: show, Services: svc, LookPath: func(n string) (string, error) {
				if installed[n] {
					return "/usr/bin/" + n, nil
				}
				return "", errors.New("not found")
			}}
			res, err := r.Resolve(p)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("want error with %q, got %v (%q)", tt.errText, err, res.Command)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Command != tt.want || res.Confirm != tt.confirm {
				t.Errorf("got %q confirm=%v", res.Command, res.Confirm)
			}
		})
	}
}
