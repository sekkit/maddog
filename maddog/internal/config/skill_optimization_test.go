package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSkillOptimizationDefaultsRemainDisabledAndConservative(t *testing.T) {
	cfg := Default()
	got := cfg.EffectiveSkillOptimizationConfig()
	if got.Enabled {
		t.Fatal("skill optimization must be opt-in")
	}
	if got.Rounds != 3 || got.BatchSize != 4 || got.MaxConcurrency != 1 {
		t.Fatalf("unexpected optimization defaults: %+v", got)
	}
	if got.RedactArtifacts == nil || !*got.RedactArtifacts {
		t.Fatal("artifact redaction must default on")
	}
	if got.RequireApproval == nil || !*got.RequireApproval {
		t.Fatal("promotion approval must default on")
	}
	if got.CaptureReplay || got.AllowShell || got.MaxReplayBundles != 200 {
		t.Fatalf("replay capture must default off with bounded retention: %+v", got)
	}
}

func TestRenderProjectSkillOptimizationRoundTrips(t *testing.T) {
	redact, approve := false, false
	cfg := Default()
	cfg.Skills.Optimization = SkillOptimizationConfig{
		Enabled: true, CaptureReplay: true, AllowShell: true,
		Model: "rollout", ProposerModel: "optimizer", Rounds: 5, BatchSize: 7,
		MaxConcurrency: 2, MinDelta: 0.02, Deadband: 0.003,
		MaxCalls: 80, MaxInputTokens: 120000, MaxOutputTokens: 20000, MaxCost: 4.5,
		RetentionDays: 14, MaxReplayBundles: 50, RedactArtifacts: &redact, RequireApproval: &approve,
	}
	rendered := RenderTOMLForScope(cfg, RenderScopeProject)
	if !strings.Contains(rendered, "[skills.optimization]") || !strings.Contains(rendered, "allow_shell = true") {
		t.Fatalf("rendered optimization block missing:\n%s", rendered)
	}
	var decoded Config
	if _, err := toml.Decode(rendered, &decoded); err != nil {
		t.Fatalf("decode rendered project config: %v\n%s", err, rendered)
	}
	got := decoded.Skills.Optimization
	if !got.Enabled || !got.CaptureReplay || !got.AllowShell || got.Model != "rollout" || got.ProposerModel != "optimizer" || got.MaxCalls != 80 {
		t.Fatalf("round-tripped optimization config = %+v", got)
	}
	if got.RedactArtifacts == nil || *got.RedactArtifacts || got.RequireApproval == nil || *got.RequireApproval {
		t.Fatalf("round-tripped safety toggles = %+v", got)
	}
}

func TestLoadSkillOptimizationProjectConfig(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	path := filepath.Join(root, ProjectConfigFilename)
	if err := os.WriteFile(path, []byte(`
[skills.optimization]
enabled = true
capture_replay = true
allow_shell = true
model = "rollout"
proposer_model = "optimizer"
rounds = 5
batch_size = 7
max_calls = 80
max_input_tokens = 120000
max_output_tokens = 20000
max_cost = 4.5
retention_days = 14
max_replay_bundles = 50
redact_artifacts = false
require_approval = false
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.EffectiveSkillOptimizationConfig()
	if !got.Enabled || !got.CaptureReplay || !got.AllowShell || got.Model != "rollout" || got.ProposerModel != "optimizer" || got.Rounds != 5 || got.BatchSize != 7 {
		t.Fatalf("project optimization config not loaded: %+v", got)
	}
	if got.MaxCalls != 80 || got.MaxInputTokens != 120000 || got.MaxOutputTokens != 20000 || got.MaxCost != 4.5 || got.RetentionDays != 14 || got.MaxReplayBundles != 50 {
		t.Fatalf("project optimization budget not loaded: %+v", got)
	}
	if got.RedactArtifacts == nil || *got.RedactArtifacts || got.RequireApproval == nil || *got.RequireApproval {
		t.Fatalf("explicit safety toggles not preserved: %+v", got)
	}
}
