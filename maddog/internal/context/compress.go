package context

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Compressor interface {
	Compress(ToolOutput) CompressionResult
}

type ToolOutput struct {
	Tool   string
	CallID string
	Args   string
	Output string
}

type CompressOptions struct {
	ThresholdBytes int
	HeadBytes      int
	TailBytes      int
	MaxErrorLines  int
	RawStore       RawStore
}

type CompressionResult struct {
	Text            string
	Compressed      bool
	Strategy        string
	RawRef          string
	RawAvailable    bool
	RawError        string
	OriginalBytes   int
	CompressedBytes int
	SavedBytes      int
}

type DeterministicCompressor struct {
	opts CompressOptions
}

func NewDeterministicCompressor(opts CompressOptions) DeterministicCompressor {
	if opts.ThresholdBytes <= 0 {
		opts.ThresholdBytes = 16 * 1024
	}
	if opts.HeadBytes <= 0 {
		opts.HeadBytes = 4 * 1024
	}
	if opts.TailBytes <= 0 {
		opts.TailBytes = 4 * 1024
	}
	if opts.MaxErrorLines <= 0 {
		opts.MaxErrorLines = 12
	}
	return DeterministicCompressor{opts: opts}
}

var errorLinePattern = regexp.MustCompile(`(?i)(error|fail|failed|fatal|panic|exception|traceback|denied|permission)`)

func (c DeterministicCompressor) Compress(in ToolOutput) CompressionResult {
	originalBytes := len(in.Output)
	if originalBytes <= c.opts.ThresholdBytes {
		return CompressionResult{
			Text:            in.Output,
			OriginalBytes:   originalBytes,
			CompressedBytes: originalBytes,
		}
	}
	if isShellTool(in.Tool) {
		return c.compressShell(in, originalBytes)
	}
	rawRef, rawAvailable, rawError := c.rawRefFor(in)
	head := snapToRuneBoundary(in.Output, 0, min(c.opts.HeadBytes, len(in.Output)))
	tailStart := max(0, len(in.Output)-c.opts.TailBytes)
	tail := snapToRuneBoundary(in.Output, tailStart, len(in.Output))
	errors := extractErrorLines(in.Output, c.opts.MaxErrorLines)

	var b strings.Builder
	fmt.Fprintf(&b, "[compressed tool output]\nstrategy: deterministic_head_tail_errors\nraw_ref: %s\noriginal_bytes: %d\n\n", rawRef, originalBytes)
	if len(errors) > 0 {
		b.WriteString("errors:\n")
		for _, line := range errors {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString("head:\n")
	b.WriteString(head)
	b.WriteString("\n\n")
	b.WriteString("tail:\n")
	b.WriteString(tail)
	text := b.String()
	compressedBytes := len(text)
	saved := originalBytes - compressedBytes
	if saved < 0 {
		saved = 0
	}
	return CompressionResult{
		Text:            text,
		Compressed:      true,
		Strategy:        "deterministic_head_tail_errors",
		RawRef:          rawRef,
		RawAvailable:    rawAvailable,
		RawError:        rawError,
		OriginalBytes:   originalBytes,
		CompressedBytes: compressedBytes,
		SavedBytes:      saved,
	}
}

func (c DeterministicCompressor) compressShell(in ToolOutput, originalBytes int) CompressionResult {
	rawRef, rawAvailable, rawError := c.rawRefFor(in)
	text := SummarizeShellOutput(ShellOutput{Command: shellCommand(in), Output: in.Output, MaxLines: c.opts.MaxErrorLines})
	text = strings.TrimRight(text, "\n") + "\nraw_ref: " + rawRef + "\n"
	compressedBytes := len(text)
	saved := originalBytes - compressedBytes
	if saved < 0 {
		saved = 0
	}
	return CompressionResult{
		Text:            text,
		Compressed:      true,
		Strategy:        "shell_summary",
		RawRef:          rawRef,
		RawAvailable:    rawAvailable,
		RawError:        rawError,
		OriginalBytes:   originalBytes,
		CompressedBytes: compressedBytes,
		SavedBytes:      saved,
	}
}

func (c DeterministicCompressor) rawRefFor(in ToolOutput) (string, bool, string) {
	fallback := rawRef(in)
	if c.opts.RawStore == nil {
		return fallback, false, ""
	}
	rec, err := c.opts.RawStore.Put(in)
	if err != nil || strings.TrimSpace(rec.Ref) == "" {
		return fallback, false, "raw store write failed"
	}
	return rec.Ref, true, ""
}

func isShellTool(tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	return tool == "bash" || tool == "shell" || tool == "powershell" || tool == "cmd"
}

func shellCommand(in ToolOutput) string {
	var parsed map[string]any
	if strings.TrimSpace(in.Args) != "" && json.Unmarshal([]byte(in.Args), &parsed) == nil {
		for _, key := range []string{"command", "cmd", "script"} {
			if v, ok := parsed[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return in.Tool
}

func rawRef(in ToolOutput) string {
	id := strings.TrimSpace(in.CallID)
	if id == "" {
		id = strings.TrimSpace(in.Tool)
	}
	if id == "" {
		id = "unknown"
	}
	return "tool://" + id + "/raw"
}

func extractErrorLines(s string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !errorLinePattern.MatchString(line) {
			continue
		}
		if len(line) > 240 {
			line = snapToRuneBoundary(line, 0, 240) + "..."
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func snapToRuneBoundary(s string, lo, hi int) string {
	lo = max(0, min(lo, len(s)))
	hi = max(lo, min(hi, len(s)))
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}
