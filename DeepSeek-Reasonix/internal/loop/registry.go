package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const ProjectTemplateDir = ".maddog/loops"

func BuiltInTemplates() ([]LoopTemplateV1, error) {
	raw := []LoopTemplateV1{
		{
			SchemaVersion: SchemaVersionV1,
			ID:            "coding-task",
			Name:          "Coding task",
			Goal:          "Implement a code change with readiness, budget tracking, and verification.",
			Risk:          "medium",
			Phases: []TemplatePhase{
				{ID: "readiness", Name: "Readiness", Goal: "Check providers, credentials, budget, workspace, and required capabilities."},
				{ID: "implement", Name: "Implement", Goal: "Make the requested code changes using the selected provider roles and tools."},
				{ID: "verify", Name: "Verify", Goal: "Run tests, review findings, and produce a run report."},
			},
			ProviderRoles:        []string{"default", "small", "frontier", "advisor", "maker", "checker"},
			Budget:               TemplateBudget{FrontierTokens: 500000, TotalTokens: 800000},
			ReadinessGates:       []string{"provider_configured", "credential_available", "budget_available", "log_sink_writable", "kill_switch_enabled", "human_gate_policy"},
			HumanGates:           []string{"git_push", "delete_files", "credential_change", "budget_increase", "skill_promotion"},
			MakerChecker:         MakerCheckerConfig{Mode: MakerCheckerReviewOnly},
			RequiredCapabilities: []Capability{CapabilityRead, CapabilityWrite, CapabilityGit, CapabilityProcess},
			Artifacts: TemplateArtifacts{
				TaskPacketFields: []string{
					"request",
					"workspace_state",
					"acceptance_criteria",
					"files_changed",
					"test_plan",
				},
				BoundedFanOut: TemplateBoundedFanOut{MaxParallel: 3, MaxDepth: 1},
				DelegationArtifacts: []string{
					"worker_summary",
					"files_changed",
					"tests_run",
					"concerns",
				},
				IntegrationChecklist: []string{
					"merge_worker_outputs",
					"resolve_conflicts",
					"run_focused_tests",
					"run_ui_contracts",
				},
				FinalVerificationArtifacts: []string{
					"run_report",
					"test_summary",
					"review_notes",
				},
				RunReportMapping: []TemplateArtifactMapping{
					{Artifact: "task_packet", ReportField: "report.templateID"},
					{Artifact: "delegation_summary", ReportField: "report.phases"},
					{Artifact: "final_verification", ReportField: "report.finalStatus"},
				},
			},
			RefinementStrategy: TemplateRefinementStrategy{
				Enabled:               false,
				SearchModes:           []RefinementSearchMode{RefinementSearchBFSHypothesis, RefinementSearchDFSCorrection},
				CritiqueRounds:        2,
				CorrectionRounds:      2,
				FinalJudgeIsolation:   IsolationStrong,
				BudgetCapTokens:       100000,
				KillSwitchRequired:    true,
				HumanApprovalRequired: true,
			},
			StatePolicy:   "workspace",
			MaxIterations: 6,
		},
		{
			SchemaVersion: SchemaVersionV1,
			ID:            "review-task",
			Name:          "Review task",
			Goal:          "Review a change with deterministic checks and a read-only checker path.",
			Risk:          "low",
			Phases: []TemplatePhase{
				{ID: "readiness", Name: "Readiness", Goal: "Check code intelligence, provider, and log readiness."},
				{ID: "inspect", Name: "Inspect", Goal: "Inspect diffs, evidence, and affected symbols."},
				{ID: "report", Name: "Report", Goal: "Return prioritized findings with verification notes."},
			},
			ProviderRoles:        []string{"default", "small", "advisor", "checker"},
			Budget:               TemplateBudget{FrontierTokens: 200000, TotalTokens: 350000},
			ReadinessGates:       []string{"provider_configured", "log_sink_writable", "required_code_backend_available"},
			HumanGates:           []string{"credential_change", "budget_increase"},
			MakerChecker:         MakerCheckerConfig{Mode: MakerCheckerReviewOnly},
			RequiredCapabilities: []Capability{CapabilityRead, CapabilityGit},
			Artifacts: TemplateArtifacts{
				TaskPacketFields: []string{
					"diff_scope",
					"review_objective",
					"risk_focus",
				},
				BoundedFanOut: TemplateBoundedFanOut{MaxParallel: 2, MaxDepth: 1},
				DelegationArtifacts: []string{
					"review_findings",
					"affected_files",
				},
				IntegrationChecklist: []string{
					"deduplicate_findings",
					"rank_findings",
					"verify_line_references",
				},
				FinalVerificationArtifacts: []string{
					"review_report",
					"residual_risk",
				},
				RunReportMapping: []TemplateArtifactMapping{
					{Artifact: "review_report", ReportField: "report.checker"},
					{Artifact: "residual_risk", ReportField: "report.finalStatus"},
				},
			},
			StatePolicy:   "workspace",
			MaxIterations: 3,
		},
		{
			SchemaVersion: SchemaVersionV1,
			ID:            "skill-improvement",
			Name:          "Skill improvement",
			Goal:          "Evaluate and promote skill candidates through replay evidence and guardrails.",
			Risk:          "high",
			Phases: []TemplatePhase{
				{ID: "readiness", Name: "Readiness", Goal: "Check replay bundles, skill roots, budget, and human gate policy."},
				{ID: "evaluate", Name: "Evaluate", Goal: "Run replay scoring and guardrail checks."},
				{ID: "promote", Name: "Promote", Goal: "Request approval before writing promoted skills."},
			},
			ProviderRoles:        []string{"default", "small", "frontier", "advisor", "checker"},
			Budget:               TemplateBudget{FrontierTokens: 300000, TotalTokens: 600000},
			ReadinessGates:       []string{"provider_configured", "budget_available", "skill_root_writable", "human_gate_policy"},
			HumanGates:           []string{"skill_promotion", "credential_change", "budget_increase"},
			MakerChecker:         MakerCheckerConfig{Mode: MakerCheckerEnforcedBeforeDone},
			RequiredCapabilities: []Capability{CapabilityRead, CapabilityWrite, CapabilityProcess},
			Artifacts: TemplateArtifacts{
				TaskPacketFields: []string{
					"candidate_id",
					"source_bundle_id",
					"guardrail_policy",
					"held_out_bundle_set",
				},
				BoundedFanOut: TemplateBoundedFanOut{MaxParallel: 2, MaxDepth: 1, RequiresHumanApproval: true},
				DelegationArtifacts: []string{
					"eval_summary",
					"guardrail_findings",
					"promotion_diff",
				},
				IntegrationChecklist: []string{
					"validate_candidate_hash",
					"verify_skill_root",
					"record_promotion_audit",
				},
				FinalVerificationArtifacts: []string{
					"promotion_audit",
					"rollback_point",
				},
				RunReportMapping: []TemplateArtifactMapping{
					{Artifact: "eval_summary", ReportField: "report.phases"},
					{Artifact: "promotion_audit", ReportField: "report.humanGate"},
				},
			},
			StatePolicy:   "workspace",
			MaxIterations: 4,
		},
	}
	out := make([]LoopTemplateV1, 0, len(raw))
	for _, tmpl := range raw {
		if err := tmpl.Validate(); err != nil {
			return nil, err
		}
		withHash, err := withMetadata(tmpl, "built-in", "")
		if err != nil {
			return nil, err
		}
		out = append(out, withHash)
	}
	return out, nil
}

func LoadTemplates(workspaceRoot string) ([]LoopTemplateV1, error) {
	templates, err := BuiltInTemplates()
	if err != nil {
		return nil, err
	}
	byID := map[string]LoopTemplateV1{}
	for _, tmpl := range templates {
		byID[tmpl.ID] = tmpl
	}
	if workspaceRoot != "" {
		project, err := loadProjectTemplates(workspaceRoot)
		if err != nil {
			return nil, err
		}
		for _, tmpl := range project {
			byID[tmpl.ID] = tmpl
		}
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]LoopTemplateV1, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

func FindTemplate(templates []LoopTemplateV1, id string) (LoopTemplateV1, bool) {
	for _, tmpl := range templates {
		if tmpl.ID == id {
			return tmpl, true
		}
	}
	return LoopTemplateV1{}, false
}

func loadProjectTemplates(workspaceRoot string) ([]LoopTemplateV1, error) {
	dir := filepath.Join(workspaceRoot, ProjectTemplateDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []LoopTemplateV1{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		tmpl, err := readTemplateFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, tmpl)
	}
	return out, nil
}

func readTemplateFile(path string) (LoopTemplateV1, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LoopTemplateV1{}, err
	}
	var tmpl LoopTemplateV1
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		return LoopTemplateV1{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := tmpl.Validate(); err != nil {
		return LoopTemplateV1{}, fmt.Errorf("%s: %w", path, err)
	}
	withHash, err := withMetadata(tmpl, "project", path)
	if err != nil {
		return LoopTemplateV1{}, err
	}
	return withHash, nil
}
