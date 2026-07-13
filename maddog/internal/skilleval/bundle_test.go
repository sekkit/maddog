package skilleval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maddog/internal/agent"
	"maddog/internal/evidence"
	"maddog/internal/provider"
	"maddog/internal/skill"
)

func TestBuildBundleV2FromSessionEvidenceAndHistory(t *testing.T) {
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "fix parser"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "used parser skill"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "parser fixed"})
	dir := t.TempDir()
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)

	bundle, path, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Task:      "fix parser",
		SkillName: "parser-helper",
		Session:   sess,
		Evidence: []evidence.Receipt{
			{ToolName: "read_file", Success: true, Paths: []string{"parser.go"}, Read: true},
			{ToolName: "go test", Success: false, Command: "go test ./..."},
		},
		History: []HistoryItem{
			{Kind: "skill_invoked", Text: "parser-helper"},
			{Kind: "checkpoint", Text: "before edit"},
		},
		Dir: dir,
		Now: now,
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Version != 2 || bundle.SessionID != "sess/123" || bundle.ID == "" {
		t.Fatalf("bundle identity = %+v, want v2 with session id", bundle)
	}
	if bundle.Outcome.FinalAnswer != "parser fixed" || bundle.Outcome.TotalTurns != 1 || bundle.Outcome.ToolErrors != 1 {
		t.Fatalf("bundle outcome = %+v", bundle.Outcome)
	}
	if len(bundle.Messages) != 3 || len(bundle.Evidence) != 2 || len(bundle.History) != 2 {
		t.Fatalf("bundle snapshot counts = messages %d evidence %d history %d", len(bundle.Messages), len(bundle.Evidence), len(bundle.History))
	}
	if filepath.Base(path) != "sess-123.json" {
		t.Fatalf("bundle path = %q, want sanitized session filename", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("bundle file is not valid JSON: %s", raw)
	}
	loaded, err := LoadBundle(path)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if loaded.ID != bundle.ID || loaded.CreatedAt != now.UTC() || loaded.History[0].Kind != "skill_invoked" {
		t.Fatalf("loaded bundle = %+v, want roundtrip", loaded)
	}
}

func TestCaptureBundleDoesNotOverwriteSameSessionHistory(t *testing.T) {
	dir := t.TempDir()
	first, firstPath, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "first"}},
		Dir:       dir,
		Now:       time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle first: %v", err)
	}
	second, secondPath, err := CaptureBundle(CaptureOptions{
		SessionID: "sess/123",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "second"}},
		Dir:       dir,
		Now:       time.Date(2026, 6, 30, 9, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle second: %v", err)
	}
	if firstPath == secondPath {
		t.Fatalf("second capture reused %q, want a distinct replay file", firstPath)
	}
	loadedFirst, err := LoadBundle(firstPath)
	if err != nil {
		t.Fatalf("LoadBundle first: %v", err)
	}
	if loadedFirst.ID != first.ID || loadedFirst.Outcome.FinalAnswer != "first" {
		t.Fatalf("first bundle was overwritten: %+v", loadedFirst)
	}
	loadedSecond, err := LoadBundle(secondPath)
	if err != nil {
		t.Fatalf("LoadBundle second: %v", err)
	}
	if loadedSecond.ID != second.ID || loadedSecond.Outcome.FinalAnswer != "second" {
		t.Fatalf("second bundle mismatch: %+v", loadedSecond)
	}
}

func TestCaptureBundleDefaultsMissingOutcomeConfidenceToUnverified(t *testing.T) {
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "sess-unverified",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "done"}},
		Dir:       t.TempDir(),
		Now:       time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Outcome.Success || bundle.Outcome.GoalMet {
		t.Fatalf("capture without verified outcome should not mark success: %+v", bundle.Outcome)
	}
	if bundle.Outcome.Confidence != OutcomeConfidenceUnverified {
		t.Fatalf("Confidence = %q, want unverified", bundle.Outcome.Confidence)
	}
	if bundle.Outcome.ConfidenceReason == "" {
		t.Fatalf("ConfidenceReason should explain unverified outcome: %+v", bundle.Outcome)
	}
}

func TestCaptureBundlePromotesExplicitVerificationEvidence(t *testing.T) {
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "sess-verified",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "fix parser"},
			{Role: provider.RoleAssistant, Content: "parser fixed"},
		},
		Evidence: []evidence.Receipt{
			{ToolName: "bash", Success: true, Command: "go test ./internal/parser"},
		},
		Outcome: OutcomeInfo{
			Confidence:       OutcomeConfidenceUnverified,
			ConfidenceReason: "turn completed with a final answer but no verified goal signal",
		},
		Dir: t.TempDir(),
		Now: time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if !bundle.Outcome.Success || !bundle.Outcome.GoalMet {
		t.Fatalf("verified evidence should mark captured outcome successful: %+v", bundle.Outcome)
	}
	if bundle.Outcome.Confidence != OutcomeConfidenceVerified {
		t.Fatalf("Confidence = %q, want verified from test receipt", bundle.Outcome.Confidence)
	}
	if bundle.Outcome.ConfidenceReason == "" || bundle.Outcome.ConfidenceReason == "turn completed with a final answer but no verified goal signal" {
		t.Fatalf("ConfidenceReason did not cite verification evidence: %+v", bundle.Outcome)
	}
}

func TestCaptureBundleUsesLastVerificationCommandResult(t *testing.T) {
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "sess-stale-verification",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "fix parser"},
			{Role: provider.RoleAssistant, Content: "parser fixed"},
		},
		Evidence: []evidence.Receipt{
			{ToolName: "bash", Success: true, Command: "go test ./internal/parser"},
			{ToolName: "edit", Success: true},
			{ToolName: "bash", Success: false, Command: "go test ./internal/parser"},
		},
		Dir: t.TempDir(),
		Now: time.Date(2026, 7, 4, 9, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Outcome.Confidence == OutcomeConfidenceVerified || bundle.Outcome.Success || bundle.Outcome.GoalMet {
		t.Fatalf("stale successful verification followed by failed verification should not verify outcome: %+v", bundle.Outcome)
	}
}

func TestCaptureBundleDoesNotTreatCompileOnlyReceiptsAsGoalVerification(t *testing.T) {
	for _, command := range []string{"go build ./...", "go vet ./...", "tsc --noEmit"} {
		t.Run(command, func(t *testing.T) {
			bundle, _, err := CaptureBundle(CaptureOptions{
				SessionID: "sess-compile-only",
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: "fix parser"},
					{Role: provider.RoleAssistant, Content: "parser fixed"},
				},
				Evidence: []evidence.Receipt{
					{ToolName: "bash", Success: true, Command: command},
				},
				Dir: t.TempDir(),
				Now: time.Date(2026, 7, 4, 9, 3, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("CaptureBundle: %v", err)
			}
			if bundle.Outcome.Confidence == OutcomeConfidenceVerified || bundle.Outcome.Success || bundle.Outcome.GoalMet {
				t.Fatalf("compile-only command %q should not verify outcome: %+v", command, bundle.Outcome)
			}
		})
	}
}

func TestCaptureBundleDoesNotVerifyGenericSuccessfulTool(t *testing.T) {
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "sess-read-only",
		Messages:  []provider.Message{{Role: provider.RoleAssistant, Content: "done"}},
		Evidence: []evidence.Receipt{
			{ToolName: "read_file", Success: true, Paths: []string{"parser.go"}, Read: true},
		},
		Dir: t.TempDir(),
		Now: time.Date(2026, 7, 4, 9, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if bundle.Outcome.Confidence == OutcomeConfidenceVerified || bundle.Outcome.Success || bundle.Outcome.GoalMet {
		t.Fatalf("generic successful tool should not verify outcome: %+v", bundle.Outcome)
	}
}

func TestSafeCaptureRedactsSecretsAndSensitivePayloads(t *testing.T) {
	policy := *SafeCaptureSanitizationPolicy()
	// Turn inclusion on explicitly to prove redaction remains effective even
	// when a caller chooses to retain optional fields for local debugging.
	policy.IncludeImages = true
	policy.IncludeNativeBlocks = true
	policy.IncludeToolArguments = true
	policy.IncludeToolResults = true
	policy.IncludeEvidenceArguments = true
	policy.IncludeUnrelatedSkills = true
	policy.IncludeUnrelatedSkillBody = true
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "secret-capture",
		Task:      "use api_key=super-secret-value",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "Authorization: Bearer abcdefghijklmnop", Images: []string{"data:image/png;base64,AAAASECRET"}},
			{Role: provider.RoleAssistant, Content: "token=assistant-secret", ReasoningContent: "password: hidden", ToolCalls: []provider.ToolCall{{Name: "read_file", Arguments: `{"token":"call-secret","ok":"yes"}`}}},
			{Role: provider.RoleTool, Content: "secret=tool-secret"},
		},
		Evidence: []evidence.Receipt{{ToolName: "bash", Args: json.RawMessage(`{"api_key":"receipt-secret","ok":true}`), Command: "curl token=command-secret"}},
		Skills: []skill.Skill{
			{Name: "used", Body: "secret=used-secret"},
			{Name: "unrelated", Body: "password: unrelated-secret"},
		},
		SkillName:    "used",
		Sanitization: &policy,
		Dir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"super-secret-value", "abcdefghijklmnop", "AAAASECRET", "assistant-secret", "call-secret", "tool-secret", "receipt-secret", "command-secret", "used-secret", "unrelated-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("safe capture leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "REDACTED") || !strings.Contains(text, "REDACTED_IMAGE_DATA_URL") {
		t.Fatalf("safe capture missing redaction markers: %s", text)
	}
}

func TestSafeCaptureOmitsOptionalSensitiveFieldsByDefault(t *testing.T) {
	bundle, _, err := CaptureBundle(CaptureOptions{
		SessionID: "safe-default",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "question"},
			{Role: provider.RoleAssistant, ReasoningContent: "private reasoning", ToolCalls: []provider.ToolCall{{Name: "bash", Arguments: `{"x":1}`}}},
			{Role: provider.RoleTool, Content: "private tool result"},
		},
		Skills: []skill.Skill{
			{Name: "used", Body: "used body"},
			{Name: "unrelated", Body: "unrelated body"},
		},
		SkillName:    "used",
		Sanitization: SafeCaptureSanitizationPolicy(),
		Dir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CaptureBundle: %v", err)
	}
	if len(bundle.Messages) != 2 {
		t.Fatalf("messages = %d, want user + assistant only", len(bundle.Messages))
	}
	if bundle.Messages[1].ReasoningContent != "" || len(bundle.Messages[1].ToolCalls) != 1 || bundle.Messages[1].ToolCalls[0].Arguments != "" {
		t.Fatalf("optional assistant fields were retained: %+v", bundle.Messages[1])
	}
	if len(bundle.Skills) != 1 || bundle.Skills[0].Name != "used" {
		t.Fatalf("unrelated skill was retained: %+v", bundle.Skills)
	}
}

func TestCleanupBundlesEnforcesTTLAndCount(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for i, created := range []time.Time{base, base.Add(24 * time.Hour), base.Add(48 * time.Hour)} {
		_, path, err := CaptureBundle(CaptureOptions{
			SessionID: "retention-" + string(rune('a'+i)),
			Outcome:   OutcomeInfo{FinalAnswer: "ok"},
			Dir:       dir,
			Now:       created,
		})
		if err != nil {
			t.Fatalf("CaptureBundle %d: %v", i, err)
		}
		if path == "" {
			t.Fatal("CaptureBundle returned empty path")
		}
	}
	// An unrelated JSON artifact must never be removed by bundle cleanup.
	unrelated := filepath.Join(dir, "unrelated.json")
	if err := os.WriteFile(unrelated, []byte(`{"hello":"world"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := CleanupBundles(dir, BundleRetentionPolicy{MaxAge: 0, MaxCount: 2, Now: base.Add(72 * time.Hour)})
	if err != nil {
		t.Fatalf("CleanupBundles count: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want one oldest bundle", removed)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated JSON removed: %v", err)
	}
	removed, err = CleanupBundles(dir, BundleRetentionPolicy{MaxAge: 36 * time.Hour, Now: base.Add(96 * time.Hour)})
	if err != nil {
		t.Fatalf("CleanupBundles TTL: %v", err)
	}
	if removed != 2 {
		t.Fatalf("TTL removed = %d, want two remaining old bundles", removed)
	}
}
