package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maddog/internal/safety"
)

func TestRunLogWritesRedactedJSONLAndReport(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "runs", "run-1.jsonl")
	log := NewRunLog(RunLogOptions{
		Path:     logPath,
		Redactor: safety.DefaultRedactor(),
	})

	if err := log.Append(RunEvent{
		Kind:   RunEventStarted,
		RunID:  "run-1",
		LoopID: "coding-task",
		TurnID: "turn-1",
		Message: strings.Join([]string{
			"starting run",
			"Authorization: Bearer sk-run-secret",
			`api_key = "sk-file-secret"`,
		}, "\n"),
	}); err != nil {
		t.Fatalf("Append started: %v", err)
	}
	if err := log.Append(RunEvent{
		Kind:                  RunEventBudgetDebited,
		RunID:                 "run-1",
		Role:                  "frontier",
		BudgetUsedTokens:      25,
		BudgetLimitTokens:     100,
		BudgetRemainingTokens: 75,
		Payload: map[string]any{
			"headers": map[string]any{"Authorization": "Bearer secret-token"},
		},
	}); err != nil {
		t.Fatalf("Append budget: %v", err)
	}
	report, err := log.Close("completed")
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if report.RunID != "run-1" || report.Status != "completed" || report.Events != 2 || report.Path != logPath {
		t.Fatalf("report = %+v", report)
	}

	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	out := string(body)
	for _, secret := range []string{"sk-run-secret", "sk-file-secret", "secret-token"} {
		if strings.Contains(out, secret) {
			t.Fatalf("run log leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "[redacted-secret]") {
		t.Fatalf("run log missing redaction marker:\n%s", out)
	}

	var decoded RunEvent
	if err := json.Unmarshal(bytesLine(body, 0), &decoded); err != nil {
		t.Fatalf("decode first event: %v", err)
	}
	if decoded.Kind != RunEventStarted || decoded.RunID != "run-1" {
		t.Fatalf("decoded event = %+v", decoded)
	}
}

func TestRunLogBuildsStructuredRunReport(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "runs", "run-1", "run.jsonl")
	log := NewRunLog(RunLogOptions{Path: logPath, Redactor: safety.DefaultRedactor()})

	if err := log.Append(RunEvent{
		Kind:   RunEventStarted,
		RunID:  "run-1",
		LoopID: "coding-task",
		Payload: map[string]any{
			"templateId": "coding-task",
			"phase":      "readiness",
		},
	}); err != nil {
		t.Fatalf("append started: %v", err)
	}
	if err := log.Append(RunEvent{
		Kind:    RunEventProviderCallStarted,
		RunID:   "run-1",
		LoopID:  "coding-task",
		Role:    "frontier",
		Payload: map[string]any{"provider": "openai-official", "model": "gpt-5", "upgradeReason": "low confidence"},
	}); err != nil {
		t.Fatalf("append provider: %v", err)
	}
	if err := log.Append(RunEvent{
		Kind:                  RunEventBudgetDebited,
		RunID:                 "run-1",
		LoopID:                "coding-task",
		Role:                  "frontier",
		BudgetUsedTokens:      1200,
		BudgetLimitTokens:     2000,
		BudgetRemainingTokens: 800,
		Payload:               map[string]any{"cost": 0.42, "currency": "$"},
	}); err != nil {
		t.Fatalf("append budget: %v", err)
	}
	if err := log.Append(RunEvent{
		Kind:   RunEventHumanGateRequested,
		RunID:  "run-1",
		LoopID: "coding-task",
		Payload: map[string]any{
			"humanGate": HumanGateResult{Kind: HumanGateGitPush, Required: true, Status: "pending", Reason: "git push"},
		},
	}); err != nil {
		t.Fatalf("append human gate: %v", err)
	}
	if err := log.Append(RunEvent{
		Kind:   RunEventReportReady,
		RunID:  "run-1",
		LoopID: "coding-task",
		Payload: map[string]any{
			"readiness": ReadinessResult{Status: ReadinessReady, Score: 100, TemplateID: "coding-task"},
			"checker":   MakerCheckerResult{Mode: MakerCheckerEnforcedBeforeDone, Verdict: CheckerApproved, CanComplete: true},
			"evalHarness": EvalHarnessReport{
				ProposalID:                "long-horizon-v1",
				CandidateDocs:             1,
				CuratedEvidence:           2,
				VerificationRecords:       3,
				TrajectorySteps:           4,
				BudgetRemainingTokens:     800,
				DeterministicFailureCount: 1,
				ExcludedRuntimeDeps:       []string{"training", "cuda", "vllm", "checkpoint", "model_serving"},
				Recommendation:            "use Maddog replay bundles only",
			},
		},
	}); err != nil {
		t.Fatalf("append report: %v", err)
	}

	report, err := log.Close("completed")
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if report.TemplateID != "coding-task" || report.FinalStatus != "completed" {
		t.Fatalf("report identity = %+v", report)
	}
	if len(report.Phases) != 1 || report.Phases[0].ID != "readiness" {
		t.Fatalf("report phases = %+v", report.Phases)
	}
	if len(report.Models) != 1 || report.Models[0].Role != "frontier" || report.Models[0].Provider != "openai-official" || report.Models[0].Model != "gpt-5" {
		t.Fatalf("report models = %+v", report.Models)
	}
	if report.Budget.UsedTokens != 1200 || report.Budget.RemainingTokens != 800 || report.Budget.Cost != 0.42 {
		t.Fatalf("report budget = %+v", report.Budget)
	}
	if report.Readiness == nil || report.Readiness.Status != ReadinessReady {
		t.Fatalf("report readiness = %+v", report.Readiness)
	}
	if report.Checker == nil || report.Checker.Verdict != CheckerApproved {
		t.Fatalf("report checker = %+v", report.Checker)
	}
	if report.HumanGate == nil || report.HumanGate.Kind != HumanGateGitPush {
		t.Fatalf("report human gate = %+v", report.HumanGate)
	}
	if report.EvalHarness == nil || report.EvalHarness.ProposalID != "long-horizon-v1" || report.EvalHarness.TrajectorySteps != 4 {
		t.Fatalf("report eval harness = %+v", report.EvalHarness)
	}
	if !containsRunReportString(report.EvalHarness.ExcludedRuntimeDeps, "model_serving") {
		t.Fatalf("report eval harness should preserve excluded deps: %+v", report.EvalHarness.ExcludedRuntimeDeps)
	}
	if report.ReportPath == "" {
		t.Fatalf("report path missing: %+v", report)
	}
	if _, err := os.Stat(report.ReportPath); err != nil {
		t.Fatalf("report file not written: %v", err)
	}
}

func bytesLine(body []byte, index int) []byte {
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if index < 0 || index >= len(lines) {
		return nil
	}
	return []byte(lines[index])
}

func containsRunReportString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
