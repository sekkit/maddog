package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"maddog/internal/safety"
)

type RunEventKind string

const (
	RunEventStarted             RunEventKind = "run_started"
	RunEventProviderCallStarted RunEventKind = "provider_call_started"
	RunEventBudgetDebited       RunEventKind = "budget_debited"
	RunEventHumanGateRequested  RunEventKind = "human_gate_requested"
	RunEventKillSwitchTriggered RunEventKind = "kill_switch_triggered"
	RunEventStopped             RunEventKind = "run_stopped"
	RunEventReportReady         RunEventKind = "run_report_ready"
)

type RunEvent struct {
	Kind                  RunEventKind   `json:"kind"`
	RunID                 string         `json:"runId,omitempty"`
	LoopID                string         `json:"loopId,omitempty"`
	TurnID                string         `json:"turnId,omitempty"`
	StepID                string         `json:"stepId,omitempty"`
	Role                  string         `json:"role,omitempty"`
	BudgetUsedTokens      int64          `json:"budgetUsedTokens,omitempty"`
	BudgetLimitTokens     int64          `json:"budgetLimitTokens,omitempty"`
	BudgetRemainingTokens int64          `json:"budgetRemainingTokens,omitempty"`
	Message               string         `json:"message,omitempty"`
	Payload               map[string]any `json:"payload,omitempty"`
	At                    time.Time      `json:"at,omitempty"`
}

type RunReport struct {
	RunID       string              `json:"runId,omitempty"`
	LoopID      string              `json:"loopId,omitempty"`
	TemplateID  string              `json:"templateId,omitempty"`
	Status      string              `json:"status"`
	FinalStatus string              `json:"finalStatus,omitempty"`
	Path        string              `json:"path"`
	ReportPath  string              `json:"reportPath,omitempty"`
	Events      int                 `json:"events"`
	Phases      []RunReportPhase    `json:"phases,omitempty"`
	Models      []RunReportModel    `json:"models,omitempty"`
	Budget      RunReportBudget     `json:"budget,omitempty"`
	Readiness   *ReadinessResult    `json:"readiness,omitempty"`
	Checker     *MakerCheckerResult `json:"checker,omitempty"`
	HumanGate   *HumanGateResult    `json:"humanGate,omitempty"`
	EvalHarness *EvalHarnessReport  `json:"evalHarness,omitempty"`
}

type RunReportPhase struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type RunReportModel struct {
	Role          string  `json:"role,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model,omitempty"`
	TotalTokens   int64   `json:"totalTokens,omitempty"`
	Cost          float64 `json:"cost,omitempty"`
	Currency      string  `json:"currency,omitempty"`
	UpgradeReason string  `json:"upgradeReason,omitempty"`
}

type RunReportBudget struct {
	UsedTokens      int64   `json:"usedTokens,omitempty"`
	LimitTokens     int64   `json:"limitTokens,omitempty"`
	RemainingTokens int64   `json:"remainingTokens,omitempty"`
	Cost            float64 `json:"cost,omitempty"`
	Currency        string  `json:"currency,omitempty"`
}

type EvalHarnessReport struct {
	ProposalID                string   `json:"proposalId,omitempty"`
	CandidateDocs             int      `json:"candidateDocs,omitempty"`
	CuratedEvidence           int      `json:"curatedEvidence,omitempty"`
	VerificationRecords       int      `json:"verificationRecords,omitempty"`
	TrajectorySteps           int      `json:"trajectorySteps,omitempty"`
	BudgetLimitTokens         int64    `json:"budgetLimitTokens,omitempty"`
	BudgetUsedTokens          int64    `json:"budgetUsedTokens,omitempty"`
	BudgetRemainingTokens     int64    `json:"budgetRemainingTokens,omitempty"`
	DeterministicFailureCount int      `json:"deterministicFailureCount,omitempty"`
	ExcludedRuntimeDeps       []string `json:"excludedRuntimeDeps,omitempty"`
	Recommendation            string   `json:"recommendation,omitempty"`
}

type RunLogOptions struct {
	Path     string
	Redactor safety.Redactor
}

type RunLog struct {
	mu       sync.Mutex
	path     string
	redactor safety.Redactor
	file     *os.File
	closed   bool
	events   int
	runID    string
	loopID   string
	report   RunReport
}

func NewRunLog(opts RunLogOptions) *RunLog {
	return &RunLog{path: opts.Path, redactor: opts.Redactor}
}

func (l *RunLog) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *RunLog) Append(e RunEvent) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return os.ErrClosed
	}
	if err := l.openLocked(); err != nil {
		return err
	}
	e = l.redactEvent(e)
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if l.runID == "" && e.RunID != "" {
		l.runID = e.RunID
	}
	if l.loopID == "" && e.LoopID != "" {
		l.loopID = e.LoopID
	}
	l.updateReportLocked(e)
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := l.file.Write(append(body, '\n')); err != nil {
		return err
	}
	l.events++
	return nil
}

func (l *RunLog) Close(status string) (RunReport, error) {
	if l == nil {
		return RunReport{Status: status, FinalStatus: status}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil && !l.closed {
		if err := l.file.Close(); err != nil {
			return RunReport{}, err
		}
	}
	l.closed = true
	report := l.report
	report.RunID = firstNonEmpty(report.RunID, l.runID)
	report.LoopID = firstNonEmpty(report.LoopID, l.loopID)
	report.Status = status
	report.FinalStatus = firstNonEmpty(report.FinalStatus, status)
	report.Path = l.path
	report.ReportPath = reportPathForLog(l.path)
	report.Events = l.events
	if report.TemplateID == "" {
		report.TemplateID = report.LoopID
	}
	if report.ReportPath != "" {
		if err := writeRunReportFile(report.ReportPath, report); err != nil {
			return RunReport{}, err
		}
	}
	return report, nil
}

func writeRunReportFile(path string, report RunReport) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

func reportPathForLog(logPath string) string {
	if logPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(logPath), "report.json")
}

func (l *RunLog) updateReportLocked(e RunEvent) {
	l.report.RunID = firstNonEmpty(l.report.RunID, e.RunID)
	l.report.LoopID = firstNonEmpty(l.report.LoopID, e.LoopID)
	if templateID := payloadString(e.Payload, "templateId"); templateID != "" {
		l.report.TemplateID = templateID
	}
	if phase := payloadString(e.Payload, "phase"); phase != "" {
		l.upsertPhase(phase, payloadString(e.Payload, "phaseStatus"))
	}
	switch e.Kind {
	case RunEventProviderCallStarted:
		l.upsertModel(RunReportModel{
			Role:          e.Role,
			Provider:      payloadString(e.Payload, "provider"),
			Model:         payloadString(e.Payload, "model"),
			UpgradeReason: payloadString(e.Payload, "upgradeReason"),
		})
	case RunEventBudgetDebited:
		l.report.Budget.UsedTokens = e.BudgetUsedTokens
		l.report.Budget.LimitTokens = e.BudgetLimitTokens
		l.report.Budget.RemainingTokens = e.BudgetRemainingTokens
		if cost, ok := payloadFloat(e.Payload, "cost"); ok {
			l.report.Budget.Cost = cost
		}
		if currency := payloadString(e.Payload, "currency"); currency != "" {
			l.report.Budget.Currency = currency
		}
		if e.Role != "" {
			l.upsertModel(RunReportModel{
				Role:        e.Role,
				TotalTokens: e.BudgetUsedTokens,
				Cost:        l.report.Budget.Cost,
				Currency:    l.report.Budget.Currency,
			})
		}
	case RunEventHumanGateRequested:
		if gate := payloadHumanGate(e.Payload, "humanGate"); gate != nil {
			l.report.HumanGate = gate
		}
	case RunEventStopped:
		if e.Message != "" {
			l.report.FinalStatus = e.Message
		}
	case RunEventReportReady:
		if readiness := payloadReadiness(e.Payload, "readiness"); readiness != nil {
			l.report.Readiness = readiness
		}
		if checker := payloadChecker(e.Payload, "checker"); checker != nil {
			l.report.Checker = checker
		}
		if gate := payloadHumanGate(e.Payload, "humanGate"); gate != nil {
			l.report.HumanGate = gate
		}
		if harness := payloadEvalHarness(e.Payload, "evalHarness"); harness != nil {
			l.report.EvalHarness = harness
		}
	}
}

func (l *RunLog) upsertPhase(id, status string) {
	if id == "" {
		return
	}
	for i := range l.report.Phases {
		if l.report.Phases[i].ID == id {
			if status != "" {
				l.report.Phases[i].Status = status
			}
			return
		}
	}
	l.report.Phases = append(l.report.Phases, RunReportPhase{ID: id, Status: status})
}

func (l *RunLog) upsertModel(next RunReportModel) {
	if next.Role == "" && next.Provider == "" && next.Model == "" {
		return
	}
	for i := range l.report.Models {
		if sameReportModel(l.report.Models[i], next) {
			mergeRunReportModel(&l.report.Models[i], next)
			return
		}
	}
	l.report.Models = append(l.report.Models, next)
}

func sameReportModel(a, b RunReportModel) bool {
	if a.Role != "" && b.Role != "" && a.Role == b.Role {
		return true
	}
	return a.Provider != "" && b.Provider != "" && a.Model != "" && b.Model != "" && a.Provider == b.Provider && a.Model == b.Model
}

func mergeRunReportModel(dst *RunReportModel, src RunReportModel) {
	if dst.Role == "" {
		dst.Role = src.Role
	}
	if dst.Provider == "" {
		dst.Provider = src.Provider
	}
	if dst.Model == "" {
		dst.Model = src.Model
	}
	if src.TotalTokens != 0 {
		dst.TotalTokens = src.TotalTokens
	}
	if src.Cost != 0 {
		dst.Cost = src.Cost
	}
	if src.Currency != "" {
		dst.Currency = src.Currency
	}
	if src.UpgradeReason != "" {
		dst.UpgradeReason = src.UpgradeReason
	}
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func payloadFloat(payload map[string]any, key string) (float64, bool) {
	if payload == nil {
		return 0, false
	}
	switch v := payload[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func payloadReadiness(payload map[string]any, key string) *ReadinessResult {
	if payload == nil {
		return nil
	}
	switch v := payload[key].(type) {
	case ReadinessResult:
		cp := v
		return &cp
	case *ReadinessResult:
		return v
	default:
		return nil
	}
}

func payloadChecker(payload map[string]any, key string) *MakerCheckerResult {
	if payload == nil {
		return nil
	}
	switch v := payload[key].(type) {
	case MakerCheckerResult:
		cp := v
		return &cp
	case *MakerCheckerResult:
		return v
	default:
		return nil
	}
}

func payloadHumanGate(payload map[string]any, key string) *HumanGateResult {
	if payload == nil {
		return nil
	}
	switch v := payload[key].(type) {
	case HumanGateResult:
		cp := v
		return &cp
	case *HumanGateResult:
		return v
	default:
		return nil
	}
}

func payloadEvalHarness(payload map[string]any, key string) *EvalHarnessReport {
	if payload == nil {
		return nil
	}
	switch v := payload[key].(type) {
	case EvalHarnessReport:
		cp := v
		return &cp
	case *EvalHarnessReport:
		return v
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (l *RunLog) openLocked() error {
	if l.file != nil {
		return nil
	}
	if l.path == "" {
		f, err := os.CreateTemp("", "maddog-run-*.jsonl")
		if err != nil {
			return err
		}
		l.path = f.Name()
		l.file = f
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

func (l *RunLog) redactEvent(e RunEvent) RunEvent {
	e.Message = l.redactor.String(e.Message)
	e.Payload = l.redactor.Map(e.Payload)
	return e
}
