// Command maddog is a config- and plugin-driven coding agent CLI.
package main

import (
	"os"

	"maddog/internal/cli"

	// Blank imports wire compile-time built-ins into their registries.
	_ "maddog/internal/provider/anthropic"
	_ "maddog/internal/provider/openai"
	_ "maddog/internal/tool/builtin"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
