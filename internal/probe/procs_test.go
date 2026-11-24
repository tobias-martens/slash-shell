package probe

import (
	"os"
	"reflect"
	"testing"
)

func TestPrograms(t *testing.T) {
	const me, other = 1000, 0
	ff := "snap.firefox.firefox-1234.scope"
	term := "vte-spawn-abcd.scope"
	procs := []Proc{
		{PID: 1, PPID: 0, UID: other, Arg0: "systemd", Scope: "init.scope"},
		{PID: 10, PPID: 1, UID: me, Arg0: "gnome-shell", Scope: "org.gnome.Shell@wayland.service", RSS: 900},
		{PID: 20, PPID: 10, UID: me, Arg0: "firefox", Comm: "firefox", Scope: ff, RSS: 500},
		{PID: 21, PPID: 20, UID: me, Arg0: "firefox", Comm: "Isolated Web Co", Scope: ff, RSS: 300},
		{PID: 22, PPID: 10, UID: me, Arg0: "crashhelper", Comm: "crashhelper", Scope: ff, RSS: 5},
		{PID: 30, PPID: 10, UID: me, Arg0: "bash", Scope: term}, // another terminal tab
		{PID: 31, PPID: 30, UID: me, Arg0: "vim", Comm: "vim", Scope: term, RSS: 50},
		{PID: 40, PPID: 10, UID: me, Arg0: "bash", Scope: term},  // the terminal slash runs in
		{PID: 41, PPID: 40, UID: me, Arg0: "slash", Scope: term}, // slash itself
		{PID: 50, PPID: 1, UID: other, Arg0: "sshd", Scope: "ssh.service"},
		{PID: 60, PPID: 10, UID: me, Arg0: "evolution", Scope: "app-gnome-org.gnome.Evolution-60.scope", RSS: 80},
		{PID: 70, PPID: 10, UID: me, Arg0: "gimp", Scope: "app-flatpak-org.gimp.GIMP-70.scope", RSS: 70},
		{PID: 80, PPID: 0, UID: me, Arg0: "", Comm: "kworker", Scope: ""},
	}
	progs := Programs(procs, me, 41)
	var names []string
	for _, p := range progs {
		names = append(names, p.Name)
	}
	if want := []string{"firefox", "evolution", "gimp", "vim"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("got %v", names)
	}
	if !reflect.DeepEqual(progs[0].PIDs, []int{20, 22}) {
		t.Errorf("firefox: stop main process and crash helper, got %v", progs[0].PIDs)
	}
	for name, want := range map[string]string{"FIREFOX": "firefox", "Isolated Web Co": "", "vim": "vim",
		"gimp": "gimp", "sshd": "", "bash": "", "slash": ""} {
		p, ok := FindProgram(progs, name)
		if ok != (want != "") || p.Name != want {
			t.Errorf("FindProgram(%q) = %q %v", name, p.Name, ok)
		}
	}
}

func TestProgramsWithoutScopes(t *testing.T) {
	procs := []Proc{
		{PID: 2, PPID: 1, UID: 5, Arg0: "bash"},
		{PID: 3, PPID: 2, UID: 5, Arg0: "top"},
		{PID: 4, PPID: 3, UID: 5, Arg0: "top"}, // helper of the same name
		{PID: 5, PPID: 2, UID: 5, Arg0: "slash"},
	}
	progs := Programs(procs, 5, 5)
	if len(progs) != 1 || progs[0].Name != "top" || !reflect.DeepEqual(progs[0].PIDs, []int{3}) {
		t.Errorf("got %+v", progs)
	}
}

func TestReadProcsSelf(t *testing.T) {
	p, ok := readProc("/proc", os.Getpid())
	if !ok || p.PPID != os.Getppid() || p.UID != os.Getuid() || p.Arg0 == "" {
		t.Errorf("got %+v", p)
	}
	if !Alive("/proc", os.Getpid()) || Alive("/proc", 1<<30) {
		t.Error("Alive")
	}
}
