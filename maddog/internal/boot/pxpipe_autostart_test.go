package boot

import (
	"context"
	"errors"
	"testing"

	"maddog/internal/config"
	"maddog/internal/pxpipe"
)

func TestEnsurePxpipeProviderStartsOnlyWhenEnabledForPxpipeProvider(t *testing.T) {
	old := startPxpipeSidecar
	defer func() { startPxpipeSidecar = old }()

	calls := 0
	startPxpipeSidecar = func(_ context.Context, _ *config.Config, providerName string) (pxpipe.Status, error) {
		calls++
		if providerName != "pxpipe-gpt" {
			t.Fatalf("providerName = %q", providerName)
		}
		return pxpipe.Status{Healthy: true, DashboardURL: "http://127.0.0.1:47821/"}, nil
	}

	cfg := config.Default()
	cfg.Pxpipe.Enabled = true
	cfg.Pxpipe.AutoStart = true
	entry := &config.ProviderEntry{Name: "pxpipe-gpt", Kind: "openai", Model: "gpt-5.6"}

	st, required, err := ensurePxpipeProvider(context.Background(), cfg, entry)
	if err != nil || !required || !st.Healthy || calls != 1 {
		t.Fatalf("ensure = (%+v, %v, %v), calls=%d", st, required, err, calls)
	}

	_, required, err = ensurePxpipeProvider(context.Background(), cfg, &config.ProviderEntry{Name: "deepseek-flash"})
	if err != nil || required || calls != 1 {
		t.Fatalf("ordinary provider started pxpipe: required=%v err=%v calls=%d", required, err, calls)
	}
}

func TestEnsurePxpipeProviderSurfacesStartupFailure(t *testing.T) {
	old := startPxpipeSidecar
	defer func() { startPxpipeSidecar = old }()

	startPxpipeSidecar = func(context.Context, *config.Config, string) (pxpipe.Status, error) {
		return pxpipe.Status{State: pxpipe.StateNotInstalled}, errors.New("runtime missing")
	}
	cfg := config.Default()
	cfg.Pxpipe.Enabled = true
	cfg.Pxpipe.AutoStart = true

	_, required, err := ensurePxpipeProvider(context.Background(), cfg, &config.ProviderEntry{Name: "pxpipe-gpt"})
	if !required || err == nil || err.Error() != "pxpipe auto-start: runtime missing" {
		t.Fatalf("ensure error = %v, required=%v", err, required)
	}
}
