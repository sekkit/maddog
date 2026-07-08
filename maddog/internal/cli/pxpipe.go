package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"maddog/internal/config"
	pxpipemgr "maddog/internal/pxpipe"
)

func pxpipeCommand(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	rest := args
	if len(rest) > 0 {
		rest = rest[1:]
	}
	switch sub {
	case "", "status":
		return pxpipeStatus(rest)
	case "start":
		return pxpipeStart(rest)
	case "stop":
		return pxpipeStop(rest)
	case "help", "-h", "--help":
		pxpipeUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown pxpipe subcommand %q\n\n", sub)
		pxpipeUsage()
		return 2
	}
}

func pxpipeStatus(args []string) int {
	opt, jsonOut, rc := parsePxpipeFlags("pxpipe status", args, false)
	if rc != 0 {
		return rc
	}
	st := pxpipemgr.NewManager().Status(context.Background(), opt)
	if *jsonOut {
		return printJSON(st)
	}
	printPxpipeStatus(st)
	return 0
}

func pxpipeStart(args []string) int {
	opt, jsonOut, rc := parsePxpipeFlags("pxpipe start", args, true)
	if rc != 0 {
		return rc
	}
	st, err := pxpipemgr.NewManager().Start(context.Background(), opt)
	if *jsonOut {
		rc := printJSON(st)
		if err != nil {
			return 1
		}
		return rc
	}
	printPxpipeStatus(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func pxpipeStop(args []string) int {
	opt, jsonOut, rc := parsePxpipeFlags("pxpipe stop", args, false)
	if rc != 0 {
		return rc
	}
	st, err := pxpipemgr.NewManager().Stop(context.Background(), opt)
	if *jsonOut {
		rc := printJSON(st)
		if err != nil {
			return 1
		}
		return rc
	}
	printPxpipeStatus(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func parsePxpipeFlags(name string, args []string, loadConfig bool) (pxpipemgr.StartOptions, *bool, int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	host := fs.String("host", pxpipemgr.DefaultHost, "bind/check host")
	port := fs.Int("port", pxpipemgr.DefaultPort, "bind/check port")
	models := fs.String("models", pxpipemgr.DefaultModels, "PXPIPE_MODELS value")
	logPath := fs.String("log", "", "PXPIPE_LOG path")
	anthropicUpstream := fs.String("anthropic-upstream", "", "ANTHROPIC_UPSTREAM")
	openAIUpstream := fs.String("openai-upstream", "", "OPENAI_UPSTREAM")
	if err := fs.Parse(args); err != nil {
		return pxpipemgr.StartOptions{}, jsonOut, 2
	}
	opt := pxpipemgr.StartOptions{
		Host:              *host,
		Port:              *port,
		LogPath:           *logPath,
		Models:            *models,
		AnthropicUpstream: *anthropicUpstream,
		OpenAIUpstream:    *openAIUpstream,
	}
	if loadConfig {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return opt, jsonOut, 1
		}
		opt.Config = cfg
	}
	return opt, jsonOut, 0
}

func printPxpipeStatus(st pxpipemgr.Status) {
	fmt.Printf("pxpipe %s\n", st.State)
	fmt.Printf("  dashboard  %s\n", st.DashboardURL)
	fmt.Printf("  installed  %v\n", st.Installed)
	if strings.TrimSpace(st.Binary) != "" {
		fmt.Printf("  binary     %s\n", st.Binary)
	}
	fmt.Printf("  managed    %v\n", st.Managed)
	if st.PID > 0 {
		fmt.Printf("  pid        %d\n", st.PID)
	}
	if strings.TrimSpace(st.Models) != "" {
		fmt.Printf("  models     %s\n", st.Models)
	}
	if strings.TrimSpace(st.AnthropicUpstream) != "" {
		fmt.Printf("  anthropic  %s\n", st.AnthropicUpstream)
	}
	if strings.TrimSpace(st.OpenAIUpstream) != "" {
		fmt.Printf("  openai     %s\n", st.OpenAIUpstream)
	}
	if strings.TrimSpace(st.LogPath) != "" {
		fmt.Printf("  log        %s\n", st.LogPath)
	}
	if ev := st.EventSummary; ev != nil {
		fmt.Printf("  events     %d requests (%d compressed / %d pass-through, %d images)\n", ev.Requests, ev.Compressed, ev.PassThrough, ev.Images)
	}
	if strings.TrimSpace(st.Error) != "" {
		fmt.Printf("  error      %s\n", st.Error)
	}
}

func pxpipeUsage() {
	fmt.Print(`maddog pxpipe — manage an optional local pxpipe sidecar

Usage:
  maddog pxpipe status [--json] [--host HOST] [--port PORT]
  maddog pxpipe start  [--json] [--host HOST] [--port PORT] [--models LIST] [--anthropic-upstream URL] [--openai-upstream URL] [--log PATH]
  maddog pxpipe stop   [--json] [--host HOST] [--port PORT]

Defaults bind to 127.0.0.1 and keep pxpipe optional.
`)
}
