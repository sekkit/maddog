package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/config"
	"maddog/internal/provider"
)

type imageFallbackRecordingRunner struct {
	inputs []string
}

func (r *imageFallbackRecordingRunner) Run(ctx context.Context, input string) error {
	r.inputs = append(r.inputs, input)
	return nil
}

type imageFallbackProvider struct {
	reqs []provider.Request
	text string
	err  error
}

func (p *imageFallbackProvider) Name() string { return "vision-fallback" }

func (p *imageFallbackProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.reqs = append(p.reqs, req)
	ch := make(chan provider.Chunk, 2)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
		case ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}:
			ch <- provider.Chunk{Type: provider.ChunkDone}
		}
	}()
	return ch, nil
}

func TestImageFallbackInjectsSummaryForTextOnlyModel(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "maddog.toml")); err != nil {
		t.Fatalf("save workspace config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "diagram.png"), mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &imageFallbackRecordingRunner{}
	fallback := &imageFallbackProvider{text: "The screenshot shows a blue toolbar and a misaligned design image."}
	c := New(Options{
		Runner:                runner,
		ModelRef:              "custom/text-only",
		WorkspaceRoot:         workspace,
		ImageFallbackProvider: fallback,
	})
	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "align @diagram.png", "align @diagram.png", ""); err != nil {
		t.Fatal(err)
	}

	if len(fallback.reqs) != 1 {
		t.Fatalf("fallback provider calls = %d, want 1", len(fallback.reqs))
	}
	fallbackMsg := fallback.reqs[0].Messages[len(fallback.reqs[0].Messages)-1]
	if len(fallbackMsg.Images) != 1 || !strings.HasPrefix(fallbackMsg.Images[0], "data:image/png;base64,") {
		t.Fatalf("fallback images = %#v, want one png data URL", fallbackMsg.Images)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	if !strings.Contains(runner.inputs[0], "<image_fallback") ||
		!strings.Contains(runner.inputs[0], "The screenshot shows a blue toolbar") {
		t.Fatalf("runner input missing fallback summary:\n%s", runner.inputs[0])
	}
	if strings.Contains(runner.inputs[0], "data:image/png;base64,") {
		t.Fatalf("runner input should not inline image bytes:\n%s", runner.inputs[0])
	}
}
