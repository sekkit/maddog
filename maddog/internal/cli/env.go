package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"maddog/internal/config"
	"maddog/internal/environment"
)

func envCommand(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status", "list":
		return envList(args[1:])
	case "refresh":
		return envRefresh(args[1:])
	case "show":
		return envShow(args[1:])
	case "help", "-h", "--help":
		envUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown env subcommand %q\n\n", sub)
		envUsage()
		return 2
	}
}

func envList(args []string) int {
	fs := flag.NewFlagSet("env list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tools, err := environment.ListTools(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return printJSON(tools)
	}
	for _, tool := range tools {
		line := fmt.Sprintf("%-12s %-8s %s", tool.Record.Name, envValueOr(tool.Record.Status, "unknown"), envValueOr(tool.Record.Selected, "(not selected)"))
		if strings.TrimSpace(tool.Record.Version) != "" {
			line += " version:" + tool.Record.Version
		}
		if strings.TrimSpace(tool.Record.Expected) != "" {
			line += " expected:" + tool.Record.Expected
		}
		fmt.Println(line)
	}
	return 0
}

func envRefresh(args []string) int {
	fs := flag.NewFlagSet("env refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg, err := environment.Refresh(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return printJSON(reg)
	}
	fmt.Printf("refreshed %d tools: %s\n", len(reg.Tools), config.ProjectEnvironmentRegistryPath("."))
	return 0
}

func envShow(args []string) int {
	fs := flag.NewFlagSet("env show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", true, "print JSON")
	toolName := fs.String("tool", "", "tool name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*toolName) == "" {
		fmt.Fprintln(os.Stderr, "tool name is required")
		return 2
	}
	tool, err := environment.ResolveTool(".", *toolName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return printJSON(tool)
	}
	fmt.Println(tool.Record.Selected)
	return 0
}

func envUsage() {
	fmt.Print(`maddog env — inspect and refresh project environment registry

Usage:
  maddog env list [--json]           list known tool resolutions
  maddog env refresh [--json]        refresh registry for this project
  maddog env show --tool NAME        show one tool resolution
`)
}

func envValueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
