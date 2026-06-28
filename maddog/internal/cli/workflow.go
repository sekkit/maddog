package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"maddog/internal/config"
	"maddog/internal/loop"
)

func workflowCommand(args []string) int {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list":
		return workflowList(argsAfterSubcommand(args))
	case "show":
		return workflowShow(argsAfterSubcommand(args))
	case "help", "-h", "--help":
		workflowUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown workflows subcommand %q\n\n", sub)
		workflowUsage()
		return 2
	}
}

func workflowList(args []string) int {
	fs := flag.NewFlagSet("workflows list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print workflow templates as JSON")
	dir := fs.String("dir", "", "project root; reads .maddog/loops overrides from this directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := workflowRoot(*dir)
	templates, err := loop.LoadTemplates(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *jsonOut {
		return writeWorkflowJSON(templates)
	}
	defaultTemplate := workflowDefaultTemplate()
	fmt.Printf("maddog workflows (default: %s)\n", defaultTemplate)
	for _, tmpl := range templates {
		marker := " "
		if tmpl.ID == defaultTemplate {
			marker = "*"
		}
		fmt.Printf("%s %-18s %-6s %-8s %s\n", marker, tmpl.ID, tmpl.Risk, tmpl.Source, tmpl.Name)
	}
	return 0
}

func workflowShow(args []string) int {
	fs := flag.NewFlagSet("workflows show", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print workflow template as JSON")
	dir := fs.String("dir", "", "project root; reads .maddog/loops overrides from this directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: maddog workflows show [--dir PATH] [--json] <template-id>")
		return 2
	}
	templates, err := loop.LoadTemplates(workflowRoot(*dir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	tmpl, ok := loop.FindTemplate(templates, rest[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "workflow template %q not found\n", rest[0])
		return 1
	}
	if *jsonOut {
		return writeWorkflowJSON(tmpl)
	}
	fmt.Printf("%s - %s\n", tmpl.ID, tmpl.Name)
	fmt.Printf("%-16s %s\n", "goal:", tmpl.Goal)
	fmt.Printf("%-16s %s\n", "risk:", tmpl.Risk)
	fmt.Printf("%-16s %s\n", "source:", tmpl.Source)
	fmt.Printf("%-16s %s\n", "provider roles:", strings.Join(tmpl.ProviderRoles, ", "))
	fmt.Printf("%-16s %s\n", "capabilities:", workflowCapabilities(tmpl.RequiredCapabilities))
	fmt.Printf("%-16s %s\n", "human gates:", strings.Join(tmpl.HumanGates, ", "))
	fmt.Printf("%-16s %s\n", "maker-checker:", tmpl.MakerChecker.Mode)
	fmt.Printf("%-16s %d frontier / %d total\n", "budget:", tmpl.Budget.FrontierTokens, tmpl.Budget.TotalTokens)
	fmt.Printf("%-16s %d\n", "max iterations:", tmpl.MaxIterations)
	return 0
}

func argsAfterSubcommand(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	return args[1:]
}

func workflowRoot(dir string) string {
	if strings.TrimSpace(dir) != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func workflowDefaultTemplate() string {
	cfg, err := config.Load()
	if err != nil || cfg.Loop.DefaultTemplate == "" {
		return config.Default().Loop.DefaultTemplate
	}
	return cfg.Loop.DefaultTemplate
}

func workflowCapabilities(caps []loop.Capability) string {
	out := make([]string, 0, len(caps))
	for _, cap := range caps {
		out = append(out, string(cap))
	}
	return strings.Join(out, ", ")
}

func writeWorkflowJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func workflowUsage() {
	fmt.Print(`maddog workflows - list and inspect Maddog loop workflow templates

Usage:
  maddog workflows list [--dir PATH] [--json]
  maddog workflows show [--dir PATH] [--json] <template-id>

Project overrides are read from .maddog/loops under the selected directory.
`)
}
