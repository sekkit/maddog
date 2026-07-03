package main

import (
	"os"

	"maddog/internal/cli"

	_ "maddog/internal/provider/anthropic"
	_ "maddog/internal/provider/openai"
	_ "maddog/internal/tool/builtin"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
