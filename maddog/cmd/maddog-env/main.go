package main

import (
	"fmt"
	"os"

	"maddog/internal/environment"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "path" {
		fmt.Fprintln(os.Stderr, "usage: maddog-env path <tool>")
		os.Exit(2)
	}
	tool, err := environment.ResolveTool(".", os.Args[2])
	if err != nil || tool.Record.Selected == "" {
		os.Exit(1)
	}
	fmt.Print(tool.Record.Selected)
}
