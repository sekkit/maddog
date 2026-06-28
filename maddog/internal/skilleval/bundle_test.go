package skilleval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildBundleRedactsSnapshotAndKeepsRawRefMetadata(t *testing.T) {
	bundle := BuildBundle(BundleInput{
		Task:   "inspect failure with OPENAI_API_KEY=sk-secret-123",
		Source: "runtime_skill_generation",
		Snapshot: map[string]any{
			"authorization": "Bearer live-token-123456",
			"tool_output":   "raw command output with icodeeasy_token=secret-token and useful line",
		},
		RawRefs: []RawRefMetadata{{
			Ref:             "raw://tool-output/call-1.txt",
			Source:          "tool_result",
			ToolName:        "bash",
			Available:       true,
			OriginalBytes:   4096,
			CompressedBytes: 512,
		}},
	})

	if bundle.SchemaVersion != BundleSchemaVersion || bundle.ID == "" {
		t.Fatalf("bundle identity missing: %+v", bundle)
	}
	if bundle.LowConfidence {
		t.Fatal("bundle with raw refs should not be low confidence")
	}
	if len(bundle.RawRefs) != 1 || bundle.RawRefs[0].Ref != "raw://tool-output/call-1.txt" || bundle.RawRefs[0].OriginalBytes != 4096 {
		t.Fatalf("raw ref metadata not preserved: %+v", bundle.RawRefs)
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, leaked := range []string{"sk-secret-123", "live-token-123456", "secret-token"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("bundle leaked secret %q in %s", leaked, text)
		}
	}
	if !strings.Contains(text, "[redacted-secret]") {
		t.Fatalf("bundle did not include redaction marker: %s", text)
	}
}

func TestPureChatBundleIsLowConfidence(t *testing.T) {
	bundle := BuildBundle(BundleInput{
		Task:   "hello, can you explain this idea?",
		Source: "runtime_skill_generation",
	})

	if !bundle.LowConfidence || bundle.Confidence != ConfidenceLow {
		t.Fatalf("pure chat bundle should be low confidence: %+v", bundle)
	}
}

func TestBuildBundleCapturesLongHorizonHarnessEvidence(t *testing.T) {
	bundle := BuildBundle(BundleInput{
		Task:   "evaluate candidate with ANTHROPIC_API_KEY=secret-anthropic",
		Source: "long_horizon_eval",
		CandidateDocs: []CandidateDoc{{
			ID:      "candidate-doc",
			Title:   "Candidate behavior",
			Ref:     "candidate://skill/retry",
			Summary: "Use after provider timeout",
		}},
		CuratedEvidence: []CuratedEvidence{{
			ID:      "evidence-1",
			Kind:    "failure_signal",
			Ref:     "bundle://evidence/1",
			Summary: "Tool failed with token sk-secret-456",
		}},
		VerificationRecords: []VerificationRecord{{
			ID:      "verify-tests",
			Command: "go test ./internal/skilleval",
			Status:  "passed",
			Summary: "Focused tests passed",
		}},
		Trajectory: []ActionObservation{{
			StepID:      "step-1",
			Action:      "run replay",
			Observation: "candidate improved held-out task",
			Status:      "passed",
		}},
		BudgetContext: ReplayBudgetContext{
			LimitTokens:     1000,
			UsedTokens:      250,
			RemainingTokens: 750,
			Cost:            0.42,
			Currency:        "USD",
		},
	})

	if bundle.LowConfidence {
		t.Fatalf("long-horizon evidence should raise confidence: %+v", bundle)
	}
	if len(bundle.CandidateDocs) != 1 || len(bundle.CuratedEvidence) != 1 || len(bundle.VerificationRecords) != 1 || len(bundle.Trajectory) != 1 {
		t.Fatalf("long-horizon evidence not preserved: %+v", bundle)
	}
	if bundle.BudgetContext.RemainingTokens != 750 || bundle.BudgetContext.Currency != "USD" {
		t.Fatalf("budget context missing: %+v", bundle.BudgetContext)
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, leaked := range []string{"secret-anthropic", "sk-secret-456"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("long-horizon bundle leaked secret %q in %s", leaked, text)
		}
	}
}
