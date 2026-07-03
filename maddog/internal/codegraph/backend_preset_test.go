package codegraph

import "testing"

func TestKnownBackendPresetsIncludeSupportedMCPBackends(t *testing.T) {
	presets := KnownBackendPresets()
	byID := map[string]BackendPreset{}
	for _, preset := range presets {
		byID[preset.ID] = preset
	}
	for _, id := range []string{"serena", "claude-context", "codebase-memory"} {
		preset, ok := byID[id]
		if !ok {
			t.Fatalf("missing preset %q in %+v", id, presets)
		}
		if preset.ResearchOnly || preset.Backend.Kind != BackendKindMCP || preset.Backend.Server == "" || len(preset.Backend.Tools) == 0 {
			t.Fatalf("preset %q = %+v, want supported MCP backend mapping", id, preset)
		}
		for capName, toolName := range preset.Backend.Tools {
			wantPrefix := "mcp__" + preset.Backend.Server + "__"
			if len(toolName) <= len(wantPrefix) || toolName[:len(wantPrefix)] != wantPrefix {
				t.Fatalf("preset %q capability %q tool %q should use prefix %q", id, capName, toolName, wantPrefix)
			}
		}
	}
}

func TestKnownBackendPresetsMarkZvecResearchOnly(t *testing.T) {
	for _, preset := range KnownBackendPresets() {
		if preset.ID != "zvec" {
			continue
		}
		if !preset.ResearchOnly || preset.Backend.Kind != "" || preset.Backend.Server != "" || len(preset.Backend.Tools) != 0 {
			t.Fatalf("zvec preset = %+v, want research-only without MCP backend config", preset)
		}
		return
	}
	t.Fatal("zvec preset missing")
}
