package probe

import (
	"os"
	"strings"
)

// Services answers questions about systemd services and the user's running
// programs. Read-only: it never starts or stops anything.
type Services struct {
	Run  Runner
	Root bool          // as root, there are no user services to look at
	Proc func() []Proc // the processes
	UID  int
	Self int
}

// NewServices returns Services for the real system.
func NewServices(run Runner) Services {
	return Services{Run: run, Root: os.Geteuid() == 0, Proc: func() []Proc { return ReadProcs("/proc") },
		UID: os.Geteuid(), Self: os.Getpid()}
}

func (s Services) systemctl(scope string, args ...string) (string, bool) {
	if scope == "user" {
		args = append([]string{"--user"}, args...)
	}
	return s.Run("systemctl", args...)
}

func (s Services) scopes() []string {
	if s.Root {
		return []string{"system"}
	}
	return []string{"system", "user"}
}

// Unit implements resolve.Services.
func (s Services) Unit(name string) (string, bool) {
	for _, scope := range s.scopes() {
		out, _ := s.systemctl(scope, "show", "--property=LoadState", "--value", "--", name+".service")
		if strings.TrimSpace(out) == "loaded" {
			return scope, true
		}
	}
	return "", false
}

// Active implements resolve.Services.
func (s Services) Active(scope, name string) bool {
	out, _ := s.systemctl(scope, "is-active", "--", name+".service")
	return strings.TrimSpace(out) == "active"
}

// Program implements resolve.Services.
func (s Services) Program(name string) bool {
	_, ok := FindProgram(s.Programs(), name)
	return ok
}

// Programs lists the user's running programs, biggest first.
func (s Services) Programs() []Program {
	return Programs(s.Proc(), s.UID, s.Self)
}

// Running lists the running services of scope ("system" or "user").
func (s Services) Running(scope string) []string {
	out, _ := s.systemctl(scope, "list-units", "--type=service", "--state=running", "--no-legend", "--plain", "--no-pager")
	return unitNames(out)
}

// All lists every service of scope that can be started (no templates).
func (s Services) All(scope string) []string {
	out, _ := s.systemctl(scope, "list-unit-files", "--type=service", "--no-legend", "--no-pager")
	return unitNames(out)
}

// Scopes lists "system" and, unless root, "user".
func (s Services) Scopes() []string { return s.scopes() }

func unitNames(out string) []string {
	var names []string
	for _, line := range lines(out) {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasSuffix(f[0], ".service") || strings.HasSuffix(f[0], "@.service") {
			continue
		}
		names = append(names, strings.TrimSuffix(f[0], ".service"))
	}
	return names
}
