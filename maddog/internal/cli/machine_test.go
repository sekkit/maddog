package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/event"
)

func TestParseOutputFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want outputFormat
	}{{"", outputText}, {"text", outputText}, {"json", outputJSON}, {"stream-json", outputStreamJSON}} {
		got, err := parseOutputFormat(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("parseOutputFormat(%q) = %q, %v", tc.in, got, err)
		}
	}
	if _, err := parseOutputFormat("yaml"); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestMachineResumeErrorIsPureJSONAndMatchesExitCode(t *testing.T) {
	isolateCLIConfigHome(t)
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	var rc int
	out := captureStdout(t, func() {
		rc = runAgent([]string{"--output-format=json", "--resume", missing, "continue"})
	})
	if rc != 1 {
		t.Fatalf("exit code = %d, want 1", rc)
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var result machineResult
	if err := dec.Decode(&result); err != nil {
		t.Fatalf("stdout is not JSON: %v; stdout=%q", err, out)
	}
	if result.OK || result.Error == "" {
		t.Fatalf("result = %+v, want ok=false with error", result)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contained non-protocol data: %q", out)
	}
}

func TestExplicitEmptyAllowedToolsFlagIsDetected(t *testing.T) {
	fsArgs := []string{"--allowed-tools=", "task"}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var allowed stringList
	fs.Var(&allowed, "allowed-tools", "")
	if err := fs.Parse(fsArgs); err != nil {
		t.Fatal(err)
	}
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == "allowed-tools" })
	if !set || len(allowed) != 0 {
		t.Fatalf("explicit empty allowlist = (set=%v, values=%v)", set, allowed)
	}
}

func TestMachineSinkJSONAndStream(t *testing.T) {
	for _, format := range []outputFormat{outputJSON, outputStreamJSON} {
		var b bytes.Buffer
		s := newMachineSink(&b, format)
		s.Emit(event.Event{Kind: event.Text, Text: "hello"})
		if err := s.Finish(nil); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(strings.NewReader(b.String()))
		var first map[string]any
		if err := dec.Decode(&first); err != nil {
			t.Fatalf("decode %s: %v", format, err)
		}
		if format == outputJSON {
			if first["schema_version"] != machineSchemaVersion || first["ok"] != true {
				t.Fatalf("result = %#v", first)
			}
		} else if first["kind"] != "text" {
			t.Fatalf("stream event = %#v", first)
		}
	}
}
