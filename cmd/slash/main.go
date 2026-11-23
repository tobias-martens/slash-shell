// Command slash: simple and natural commands for the Linux shell.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tobias-martens/slash-shell/internal/intro"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, sources.RealEnv()))
}

func run(args []string, stdout, stderr io.Writer, env sources.Env) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, intro.Render(sources.Detect(env), version))
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, intro.Render(sources.Detect(env), version))
		return 0
	case "version", "--version":
		fmt.Fprintf(stdout, "slash %s\n", version)
		return 0
	}
	fmt.Fprintf(stderr, "slash: I don't know %q yet. Run slash on its own to see what I can do.\n", args[0])
	return 2
}
