package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"maddog/internal/sandbox"
	"maddog/internal/secrets"
)

func TestBashCommandEnvRemovesStoredCredentialAndKeepsOrdinaryEnv(t *testing.T) {
	const credential = "MADDOG_TEST_BASH_STORED_CREDENTIAL"
	t.Setenv(credential, "private")
	t.Setenv("MADDOG_TEST_BASH_VISIBLE", "ordinary")
	secrets.RegisterCredentialEnvKeys([]string{strings.ToLower(credential)})

	env := bashCommandEnv(context.Background(), false)
	joined := strings.Join(env, "\n")
	if strings.Contains(strings.ToUpper(joined), credential+"=") {
		t.Fatal("stored credential leaked into bash child environment")
	}
	if !strings.Contains(joined, "MADDOG_TEST_BASH_VISIBLE=ordinary") {
		t.Fatal("ordinary environment variable was removed")
	}
}

func TestBashMergesLoginShellPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell PATH probing is POSIX-only")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	probe := filepath.Join(bin, "maddog-path-probe")
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nprintf 'shell-path-ok\\n'\n"), 0o755); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	loginShell := filepath.Join(dir, "login-shell")
	if err := os.WriteFile(loginShell, []byte("#!/bin/sh\nprintf '\\n__MADDOG_BASH_PATH__=%s\\n' '"+bin+":/usr/bin:/bin"+"'\n"), 0o755); err != nil {
		t.Fatalf("write login shell: %v", err)
	}

	// Inject a deterministic login-shell PATH instead of spawning a real login
	// shell. The real probe (defaultBashShellPATH) runs up to three
	// interactive-login shells with a 2s timeout each; under the CPU load of
	// `go test ./...` it times out and returns an empty PATH, so this test failed
	// with command-not-found only in the full suite, never in isolation. This
	// test covers merging the probed PATH into the exec environment; the probe's
	// own parsing/merging is covered by TestParseShellPATH and TestMergePathLists.
	prev := bashShellPATH
	bashShellPATH = func(context.Context) string { return bin + ":/usr/bin:/bin" }
	t.Cleanup(func() { bashShellPATH = prev })

	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	b := bash{shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "/bin/sh"}}
	args, _ := json.Marshal(map[string]string{"command": "maddog-path-probe"})

	out, err := b.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("command should resolve through merged login-shell PATH: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "shell-path-ok") {
		t.Fatalf("output = %q, want shell-path-ok", out)
	}
}

func TestParseShellPATH(t *testing.T) {
	const marker = "__MADDOG_BASH_PATH__="
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"simple", marker + "/usr/local/bin:/usr/bin\n", "/usr/local/bin:/usr/bin"},
		{"crlf", "noise\r\n" + marker + "/opt/bin:/bin\r\n", "/opt/bin:/bin"},
		{"last marker wins", marker + "/early\n" + marker + "/late\n", "/late"},
		{"ignores surrounding output", "login banner\n" + marker + "/p\ntrailing\n", "/p"},
		{"absent", "no marker here\n", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseShellPATH([]byte(c.out), marker); got != c.want {
				t.Fatalf("parseShellPATH(%q) = %q, want %q", c.out, got, c.want)
			}
		})
	}
}

func TestScrubSecretEnvironmentKeepsRuntimePaths(t *testing.T) {
	input := []string{
		"PATH=/usr/bin", "HOME=/home/test", "TEMP=/tmp",
		"OPENAI_API_KEY=secret", "GITHUB_TOKEN=secret", "DB_PASSWORD=secret",
		"AUTH_HEADER=secret", "NORMAL_SETTING=kept",
	}
	got := scrubSecretEnvironment(input)
	joined := strings.Join(got, "\n")
	for _, keep := range []string{"PATH=/usr/bin", "HOME=/home/test", "TEMP=/tmp", "NORMAL_SETTING=kept"} {
		if !strings.Contains(joined, keep) {
			t.Fatalf("scrubbed environment dropped %q: %v", keep, got)
		}
	}
	for _, secret := range []string{"OPENAI_API_KEY", "GITHUB_TOKEN", "DB_PASSWORD", "AUTH_HEADER"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("scrubbed environment retained %q: %v", secret, got)
		}
	}
}

func TestMergePathLists(t *testing.T) {
	sep := string(os.PathListSeparator)
	cases := []struct {
		name      string
		primary   string
		secondary string
		want      string
	}{
		{"dedupes, primary first", "/a" + sep + "/b", "/b" + sep + "/c", "/a" + sep + "/b" + sep + "/c"},
		{"empty secondary", "/a" + sep + "/b", "", "/a" + sep + "/b"},
		{"empty primary", "", "/x" + sep + "/y", "/x" + sep + "/y"},
		{"skips blank entries", "/a" + sep + sep + "/b", "", "/a" + sep + "/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergePathLists(c.primary, c.secondary); got != c.want {
				t.Fatalf("mergePathLists(%q, %q) = %q, want %q", c.primary, c.secondary, got, c.want)
			}
		})
	}
}
