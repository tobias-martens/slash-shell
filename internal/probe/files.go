package probe

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// LineRunner runs a command and returns up to max lines of its output;
// ok is false if it couldn't run or failed before printing anything.
type LineRunner func(max int, name string, args ...string) (out []string, ok bool)

// StreamRunner returns a LineRunner that stops the command after max lines
// or the timeout, whichever comes first.
func StreamRunner(timeout time.Duration) LineRunner {
	return func(max int, name string, args ...string) ([]string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		pipe, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			return nil, false
		}
		var out []string
		sc := bufio.NewScanner(pipe)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for len(out) < max && sc.Scan() {
			out = append(out, sc.Text())
		}
		cancel() // stop it early if it has more
		err = cmd.Wait()
		return out, err == nil || len(out) > 0
	}
}

// Files finds files by name on this machine, offline: plocate/locate's
// index if there is one, otherwise find in the home directory.
type Files struct {
	Home     string
	LookPath func(string) (string, error)
	Run      LineRunner
}

// NewFiles returns Files for the real system.
func NewFiles() Files {
	home, _ := os.UserHomeDir()
	return Files{Home: home, LookPath: exec.LookPath, Run: StreamRunner(8 * time.Second)}
}

const maxFileLines = 20000

// Find returns the best limit paths whose name contains every word
// (case-insensitive), the total number found, and the command it ran.
// Hidden files and folders are skipped unless a word starts with ".".
func (f Files) Find(words []string, limit int) (top []string, total int, how string) {
	var found []string
	for _, tool := range []string{"plocate", "locate"} {
		if _, err := f.LookPath(tool); err != nil {
			continue
		}
		args := append([]string{"-i", "-b", "--"}, words...)
		out, ok := f.Run(maxFileLines, tool, args...)
		if ok {
			found, how = out, tool+" "+shellJoin(args)
			break
		}
	}
	if how == "" {
		args := f.findArgs(words)
		found, _ = f.Run(maxFileLines, "find", args...)
		how = "find " + shellJoin(args)
	}
	found = f.filterRank(found, words)
	total = len(found)
	if len(found) > limit {
		found = found[:limit]
	}
	return found, total, how
}

func (f Files) findArgs(words []string) []string {
	args := []string{f.Home, "-xdev"}
	if !wantsHidden(words) {
		args = append(args, "-name", ".*", "-prune", "-o")
	}
	for _, w := range words {
		args = append(args, "-iname", "*"+globEscape(w)+"*")
	}
	return append(args, "-print")
}

// filterRank keeps paths whose base name contains every word, visible ones
// only (unless asked for), files in the home directory first, then by depth.
func (f Files) filterRank(paths []string, words []string) []string {
	hidden := wantsHidden(words)
	var out []string
	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(base, strings.ToLower(w))
		}
		if ok && (hidden || !strings.Contains(p, "/.")) {
			out = append(out, p)
		}
	}
	inHome := func(p string) int {
		if f.Home != "" && strings.HasPrefix(p, f.Home+"/") {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(out, func(a, b string) int {
		if d := inHome(a) - inHome(b); d != 0 {
			return d
		}
		if d := strings.Count(a, "/") - strings.Count(b, "/"); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return out
}

func wantsHidden(words []string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, ".") {
			return true
		}
	}
	return false
}

var globSpecial = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)

func globEscape(s string) string { return globSpecial.Replace(s) }

// shellJoin quotes args for display the way a user would type them.
func shellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._+:@/%-") == "" {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}
