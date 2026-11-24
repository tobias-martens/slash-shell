package main

import (
	"fmt"
	"time"

	"github.com/tobias-martens/slash-shell/internal/trash"
)

// trash moves files to the trash itself, on systems without gio or
// trash-put. It says where each file went, so the user can find it again.
func (a *app) trash(files []string) int {
	dir := trash.Dir(a.home, a.getenv("XDG_DATA_HOME"))
	code := 0
	for _, f := range files {
		to, err := trash.Put(dir, f, time.Now())
		if err != nil {
			fmt.Fprintf(a.stderr, "slash: %v\n", err)
			code = 1
			continue
		}
		fmt.Fprintf(a.stderr, "→ moved %s to the trash: %s\n", f, tildePath(to, a.home))
	}
	return code
}
