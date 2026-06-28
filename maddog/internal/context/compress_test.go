package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeterministicCompressorNoopsUnderThreshold(t *testing.T) {
	c := NewDeterministicCompressor(CompressOptions{ThresholdBytes: 100})

	got := c.Compress(ToolOutput{Tool: "read_file", CallID: "call-1", Output: "short output"})

	if got.Compressed {
		t.Fatalf("Compressed = true, want false: %+v", got)
	}
	if got.Text != "short output" || got.OriginalBytes != len("short output") || got.SavedBytes != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestDeterministicCompressorPreservesHeadTailErrorsAndRawRef(t *testing.T) {
	output := strings.Join([]string{
		"HEAD: package maddog",
		strings.Repeat("middle noise\n", 40),
		"ERROR: failed to open internal/context/compress.go:12",
		strings.Repeat("more noise\n", 40),
		"TAIL: final status failed",
	}, "\n")
	c := NewDeterministicCompressor(CompressOptions{
		ThresholdBytes: 80,
		HeadBytes:      32,
		TailBytes:      32,
		MaxErrorLines:  4,
	})

	got := c.Compress(ToolOutput{Tool: "web_fetch", CallID: "call-42", Output: output})

	if !got.Compressed {
		t.Fatalf("Compressed = false, want true")
	}
	if got.RawRef != "tool://call-42/raw" {
		t.Fatalf("RawRef = %q", got.RawRef)
	}
	for _, want := range []string{"HEAD: package", "TAIL: final status failed", "ERROR: failed to open", "tool://call-42/raw"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("compressed text missing %q:\n%s", want, got.Text)
		}
	}
	if got.CompressedBytes >= got.OriginalBytes || got.SavedBytes <= 0 {
		t.Fatalf("bytes = original:%d compressed:%d saved:%d", got.OriginalBytes, got.CompressedBytes, got.SavedBytes)
	}
	if got.Strategy != "deterministic_head_tail_errors" {
		t.Fatalf("Strategy = %q", got.Strategy)
	}
}

func TestDeterministicCompressorUsesShellSummaryForShellTools(t *testing.T) {
	output := strings.Join([]string{
		"server: retrying",
		"server: retrying",
		"ERROR src/App.tsx:12 failed",
		strings.Repeat("noise\n", 60),
	}, "\n")
	c := NewDeterministicCompressor(CompressOptions{ThresholdBytes: 80})

	got := c.Compress(ToolOutput{Tool: "bash", CallID: "shell-1", Args: `{"command":"npm run build"}`, Output: output})

	if !got.Compressed || got.Strategy != "shell_summary" {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Text, "[shell output summary]") || !strings.Contains(got.Text, "command: npm run build") || !strings.Contains(got.Text, "src/App.tsx:12") || !strings.Contains(got.Text, "repeated 2x") {
		t.Fatalf("shell summary missing expected details:\n%s", got.Text)
	}
}

func TestDeterministicCompressorExternalizesRawOutput(t *testing.T) {
	raw := "HEAD\n" + strings.Repeat("secret-token-noise\n", 30) + "TAIL"
	store := NewFileRawStore(t.TempDir())
	c := NewDeterministicCompressor(CompressOptions{
		ThresholdBytes: 32,
		HeadBytes:      8,
		TailBytes:      8,
		RawStore:       store,
	})

	got := c.Compress(ToolOutput{Tool: "read_file", CallID: "call-raw", Output: raw})

	if !got.Compressed || !got.RawAvailable || got.RawError != "" {
		t.Fatalf("compression raw store result = %+v", got)
	}
	if !strings.HasPrefix(got.RawRef, "raw://tool-output/") {
		t.Fatalf("RawRef = %q, want raw store ref", got.RawRef)
	}
	stored, err := store.Get(got.RawRef)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored != raw {
		t.Fatalf("stored raw output mismatch")
	}
}

func TestDeterministicCompressorReportsRawStoreFailure(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(rootFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewDeterministicCompressor(CompressOptions{
		ThresholdBytes: 32,
		RawStore:       NewFileRawStore(rootFile),
	})

	got := c.Compress(ToolOutput{Tool: "read_file", CallID: "call-fail", Output: strings.Repeat("noise\n", 20)})

	if !got.Compressed {
		t.Fatalf("Compressed = false, want true")
	}
	if got.RawAvailable || got.RawError == "" {
		t.Fatalf("raw store failure was not reported: %+v", got)
	}
	if got.RawRef != "tool://call-fail/raw" {
		t.Fatalf("RawRef = %q, want fallback tool ref", got.RawRef)
	}
}
