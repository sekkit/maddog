package contextpack

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDefaultCompressorImplementsToolOutputCompressor(t *testing.T) {
	var compressor ToolOutputCompressor = DefaultCompressor{}

	got := compressor.Compress(ToolOutput{ToolName: "bash", Output: strings.Repeat("warning\n", 40)}, Options{
		ThresholdBytes: 64,
		MaxBytes:       96,
		RawRef:         "raw://tool/interface",
	})

	if !got.Compressed || got.Strategy == "" || got.Summary == "" {
		t.Fatalf("compressor result missing strategy/summary metadata: %+v", got)
	}
	if got.RawTokens <= 0 || got.CompressedTokens <= 0 || got.SavedTokens <= 0 {
		t.Fatalf("compressor result missing token delta metrics: %+v", got)
	}
}

func TestCompressLeavesShortOutputUnchanged(t *testing.T) {
	got := Compress(ToolOutput{
		ToolName: "bash",
		Output:   "ok\n",
		ReadOnly: true,
	}, Options{ThresholdBytes: 128, MaxBytes: 64, RawRef: "raw://tool/1"})

	if got.Content != "ok\n" {
		t.Fatalf("content = %q, want original output", got.Content)
	}
	if got.Compressed || got.SavedChars != 0 || got.RawRef != "" {
		t.Fatalf("short output should not report compression metadata: %+v", got)
	}
}

func TestCompressionPolicyOffLeavesLargeOutputUncompressed(t *testing.T) {
	raw := strings.Repeat("INFO heartbeat ready\n", 120) + "panic: boom\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Output:   raw,
		ReadOnly: true,
	}, Options{Policy: "off", ThresholdBytes: 1, MaxBytes: 80, RawRef: "raw://tool/off"})

	if got.Content != raw {
		t.Fatalf("policy=off content length = %d, want raw length %d", len(got.Content), len(raw))
	}
	if got.Compressed || got.RawRef != "" || got.SavedChars != 0 || got.SavedTokens != 0 {
		t.Fatalf("policy=off should suppress compression metadata: %+v", got)
	}
}

func TestCompressionPolicyAutoHonorsThresholdAndAggressiveBypassesIt(t *testing.T) {
	raw := strings.Repeat("small but repetitive\n", 8)

	auto := Compress(ToolOutput{
		ToolName: "bash",
		Output:   raw,
		ReadOnly: true,
	}, Options{Policy: "auto", ThresholdBytes: len(raw) + 1, MaxBytes: 48, RawRef: "raw://tool/auto"})
	if auto.Content != raw || auto.Compressed {
		t.Fatalf("policy=auto should leave output below threshold unchanged: %+v", auto)
	}

	aggressive := Compress(ToolOutput{
		ToolName: "bash",
		Output:   raw,
		ReadOnly: true,
	}, Options{Policy: "aggressive", ThresholdBytes: len(raw) + 1, MaxBytes: 48, RawRef: "raw://tool/aggressive"})
	if !aggressive.Compressed {
		t.Fatalf("policy=aggressive should compress compressible output below auto threshold: %+v", aggressive)
	}
}

func TestCompressLongShellOutputPreservesFailuresAndDedupe(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 80; i++ {
		b.WriteString("INFO heartbeat ready\n")
	}
	b.WriteString("--- FAIL: TestAddsNumbers (0.01s)\n")
	b.WriteString("    math/add_test.go:42: expected 4, got 5\n")
	b.WriteString("FAIL\n")
	b.WriteString("exit status 1\n")

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./..."}`,
		Output:   b.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 256, MaxBytes: 240, RawRef: "raw://session/tool-1"})

	if !got.Compressed {
		t.Fatalf("expected compressed result, got %+v", got)
	}
	if got.RawRef != "raw://session/tool-1" {
		t.Fatalf("raw ref = %q, want raw://session/tool-1", got.RawRef)
	}
	for _, want := range []string{"TestAddsNumbers", "math/add_test.go:42", "expected 4, got 5", "exit status 1"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("compressed content missing %q:\n%s", want, got.Content)
		}
	}
	if !strings.Contains(got.Content, "repeated 80 times") {
		t.Fatalf("compressed content should dedupe repeated log lines:\n%s", got.Content)
	}
	if len(got.Content) >= len(b.String()) {
		t.Fatalf("compressed content length = %d, raw = %d", len(got.Content), len(b.String()))
	}
	if got.RawChars != len(b.String()) || got.CompressedChars != len(got.Content) || got.SavedChars <= 0 {
		t.Fatalf("delta metrics = %+v, want raw/compressed/saved chars", got)
	}
	if got.Strategy == "" || got.Summary == "" {
		t.Fatalf("compressed output should include strategy and summary: %+v", got)
	}
	if got.RawTokens <= got.CompressedTokens || got.SavedTokens <= 0 {
		t.Fatalf("token metrics = %+v, want raw > compressed and positive saved tokens", got)
	}
}

func TestCompressMultibyteOutputKeepsUTF8AndRuneMetrics(t *testing.T) {
	raw := strings.Repeat("日志 心跳 正常\n", 40) +
		"--- FAIL: Test处理中文 (0.01s)\n" +
		"    包/加法_test.go:42: 期望 四, 得到 五\n" +
		"exit status 1\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Args:     `{"command":"go test ./..."}`,
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 64, MaxBytes: 180, RawRef: "raw://session/multibyte"})

	if !got.Compressed {
		t.Fatalf("expected compressed result, got %+v", got)
	}
	if !utf8.ValidString(got.Content) {
		t.Fatalf("compressed content is invalid UTF-8: %q", got.Content)
	}
	rawRunes := utf8.RuneCountInString(raw)
	compressedRunes := utf8.RuneCountInString(got.Content)
	if got.RawChars != rawRunes || got.CompressedChars != compressedRunes || got.SavedChars != rawRunes-compressedRunes {
		t.Fatalf("rune metrics = %+v, want raw=%d compressed=%d saved=%d", got, rawRunes, compressedRunes, rawRunes-compressedRunes)
	}
}

func TestUTF8TrimmingHelpersDoNotSplitRunes(t *testing.T) {
	if got := trimString("你好", 4); !utf8.ValidString(got) {
		t.Fatalf("trimString emitted invalid UTF-8: %q", got)
	}
	if got := headTail(strings.Repeat("界", 20), 11); !utf8.ValidString(got) {
		t.Fatalf("headTail emitted invalid UTF-8: %q", got)
	}
}

func TestCompressTinyMaxBytesKeepsSignalBeforeHeader(t *testing.T) {
	raw := strings.Repeat("noise line\n", 20) + "panic: boom\n"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 16, MaxBytes: len("panic: boom"), RawRef: "raw://session/tiny"})

	if got.Content != "panic: boom" {
		t.Fatalf("tiny max content = %q, want signal line without banner", got.Content)
	}
}

func TestCompressNoSavingsReturnsUncompressedWithoutMetrics(t *testing.T) {
	raw := "ERROR one\nERROR two"

	got := Compress(ToolOutput{
		ToolName: "bash",
		Output:   raw,
		ReadOnly: true,
	}, Options{ThresholdBytes: 1, MaxBytes: 1024, RawRef: "raw://session/no-savings"})

	if got.Content != raw {
		t.Fatalf("content = %q, want original raw output", got.Content)
	}
	if got.Compressed || got.RawRef != "" || got.RawChars != 0 || got.CompressedChars != 0 || got.SavedChars != 0 {
		t.Fatalf("no-savings output should be uncompressed with no metadata: %+v", got)
	}
}

func TestCompressPreservesWindowsPathLineSignals(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("setup line ")
		b.WriteString(string(rune('A' + i)))
		b.WriteByte('\n')
	}
	b.WriteString(`    C:\work\pkg\file.go:42: value changed`)
	b.WriteByte('\n')
	for i := 0; i < 10; i++ {
		b.WriteString("cleanup line ")
		b.WriteString(string(rune('A' + i)))
		b.WriteByte('\n')
	}

	got := Compress(ToolOutput{
		ToolName: "bash",
		Output:   b.String(),
		ReadOnly: true,
	}, Options{ThresholdBytes: 32, MaxBytes: 240, RawRef: "raw://session/windows-path"})

	if !strings.Contains(got.Content, `C:\work\pkg\file.go:42`) {
		t.Fatalf("compressed content should preserve Windows file:line signal:\n%s", got.Content)
	}
}
