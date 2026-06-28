// Package loop defines Maddog's workflow template and run-governance contracts.
package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const SchemaVersionV1 = "v1"

type Capability string

const (
	CapabilityRead       Capability = "read"
	CapabilityWrite      Capability = "write"
	CapabilityNetwork    Capability = "network"
	CapabilityGit        Capability = "git"
	CapabilityCredential Capability = "credential"
	CapabilityProcess    Capability = "process"
)

type MakerCheckerMode string

const (
	MakerCheckerOff                MakerCheckerMode = "off"
	MakerCheckerReviewOnly         MakerCheckerMode = "review_only"
	MakerCheckerEnforcedBeforeDone MakerCheckerMode = "enforced_before_done"
)

type LoopTemplateV1 struct {
	SchemaVersion        string                     `json:"schemaVersion"`
	ID                   string                     `json:"id"`
	Name                 string                     `json:"name"`
	Goal                 string                     `json:"goal"`
	Risk                 string                     `json:"risk"`
	Phases               []TemplatePhase            `json:"phases"`
	ProviderRoles        []string                   `json:"providerRoles"`
	Budget               TemplateBudget             `json:"budget"`
	ReadinessGates       []string                   `json:"readinessGates"`
	HumanGates           []string                   `json:"humanGates"`
	MakerChecker         MakerCheckerConfig         `json:"makerChecker"`
	RequiredCapabilities []Capability               `json:"requiredCapabilities"`
	Artifacts            TemplateArtifacts          `json:"artifacts,omitempty"`
	RefinementStrategy   TemplateRefinementStrategy `json:"refinementStrategy,omitempty"`
	StatePolicy          string                     `json:"statePolicy"`
	MaxIterations        int                        `json:"maxIterations"`
	Source               string                     `json:"source,omitempty"`
	SourcePath           string                     `json:"sourcePath,omitempty"`
	Hash                 string                     `json:"hash,omitempty"`
}

type TemplatePhase struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Goal string `json:"goal"`
}

type TemplateBudget struct {
	FrontierTokens int `json:"frontierTokens"`
	TotalTokens    int `json:"totalTokens,omitempty"`
}

type MakerCheckerConfig struct {
	Mode MakerCheckerMode `json:"mode"`
}

type TemplateArtifacts struct {
	TaskPacketFields           []string                  `json:"taskPacketFields,omitempty"`
	BoundedFanOut              TemplateBoundedFanOut     `json:"boundedFanOut,omitempty"`
	DelegationArtifacts        []string                  `json:"delegationArtifacts,omitempty"`
	IntegrationChecklist       []string                  `json:"integrationChecklist,omitempty"`
	FinalVerificationArtifacts []string                  `json:"finalVerificationArtifacts,omitempty"`
	RunReportMapping           []TemplateArtifactMapping `json:"runReportMapping,omitempty"`
}

type TemplateBoundedFanOut struct {
	MaxParallel           int  `json:"maxParallel,omitempty"`
	MaxDepth              int  `json:"maxDepth,omitempty"`
	RequiresHumanApproval bool `json:"requiresHumanApproval,omitempty"`
}

type TemplateArtifactMapping struct {
	Artifact    string `json:"artifact"`
	ReportField string `json:"reportField"`
}

type RefinementSearchMode string

const (
	RefinementSearchBFSHypothesis RefinementSearchMode = "bfs_hypothesis"
	RefinementSearchDFSCorrection RefinementSearchMode = "dfs_correction"
)

type TemplateRefinementStrategy struct {
	Enabled               bool                   `json:"enabled"`
	SearchModes           []RefinementSearchMode `json:"searchModes,omitempty"`
	CritiqueRounds        int                    `json:"critiqueRounds,omitempty"`
	CorrectionRounds      int                    `json:"correctionRounds,omitempty"`
	FinalJudgeIsolation   IsolationLevel         `json:"finalJudgeIsolation,omitempty"`
	BudgetCapTokens       int64                  `json:"budgetCapTokens,omitempty"`
	KillSwitchRequired    bool                   `json:"killSwitchRequired,omitempty"`
	HumanApprovalRequired bool                   `json:"humanApprovalRequired,omitempty"`
}

func (t LoopTemplateV1) Validate() error {
	if strings.TrimSpace(t.SchemaVersion) != SchemaVersionV1 {
		return fmt.Errorf("template %q schemaVersion = %q, want %q", t.ID, t.SchemaVersion, SchemaVersionV1)
	}
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("template id is required")
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("template %q name is required", t.ID)
	}
	if strings.TrimSpace(t.Goal) == "" {
		return fmt.Errorf("template %q goal is required", t.ID)
	}
	if t.Budget.FrontierTokens < 0 || t.Budget.TotalTokens < 0 {
		return fmt.Errorf("template %q budget cannot be negative", t.ID)
	}
	if t.MaxIterations <= 0 {
		return fmt.Errorf("template %q maxIterations must be positive", t.ID)
	}
	if len(t.Phases) == 0 {
		return fmt.Errorf("template %q must declare at least one phase", t.ID)
	}
	seenPhase := map[string]bool{}
	for _, phase := range t.Phases {
		id := strings.TrimSpace(phase.ID)
		if id == "" {
			return fmt.Errorf("template %q phase id is required", t.ID)
		}
		if seenPhase[id] {
			return fmt.Errorf("template %q has duplicate phase %q", t.ID, id)
		}
		seenPhase[id] = true
		if strings.TrimSpace(phase.Name) == "" || strings.TrimSpace(phase.Goal) == "" {
			return fmt.Errorf("template %q phase %q name and goal are required", t.ID, id)
		}
	}
	if len(t.ProviderRoles) == 0 {
		return fmt.Errorf("template %q must declare providerRoles", t.ID)
	}
	if !validMakerCheckerMode(t.MakerChecker.Mode) {
		return fmt.Errorf("template %q makerChecker mode %q is invalid", t.ID, t.MakerChecker.Mode)
	}
	for _, cap := range t.RequiredCapabilities {
		if !validCapability(cap) {
			return fmt.Errorf("template %q capability %q is invalid", t.ID, cap)
		}
	}
	if err := validateTemplateArtifacts(t.ID, t.Artifacts); err != nil {
		return err
	}
	if err := validateRefinementStrategy(t.ID, t.RefinementStrategy); err != nil {
		return err
	}
	return nil
}

func withMetadata(t LoopTemplateV1, source, sourcePath string) (LoopTemplateV1, error) {
	t.Source = source
	t.SourcePath = sourcePath
	t.Hash = ""
	raw, err := json.Marshal(t)
	if err != nil {
		return t, err
	}
	sum := sha256.Sum256(raw)
	t.Hash = hex.EncodeToString(sum[:])
	return t, nil
}

func validCapability(cap Capability) bool {
	switch cap {
	case CapabilityRead, CapabilityWrite, CapabilityNetwork, CapabilityGit, CapabilityCredential, CapabilityProcess:
		return true
	default:
		return false
	}
}

func validMakerCheckerMode(mode MakerCheckerMode) bool {
	switch mode {
	case MakerCheckerOff, MakerCheckerReviewOnly, MakerCheckerEnforcedBeforeDone:
		return true
	default:
		return false
	}
}

func validateTemplateArtifacts(templateID string, artifacts TemplateArtifacts) error {
	if artifacts.BoundedFanOut.MaxParallel < 0 || artifacts.BoundedFanOut.MaxDepth < 0 {
		return fmt.Errorf("template %q artifact fan-out cannot be negative", templateID)
	}
	for _, field := range artifacts.TaskPacketFields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("template %q artifact task packet field cannot be empty", templateID)
		}
	}
	for _, item := range artifacts.RunReportMapping {
		if strings.TrimSpace(item.Artifact) == "" || strings.TrimSpace(item.ReportField) == "" {
			return fmt.Errorf("template %q artifact run report mapping is incomplete", templateID)
		}
	}
	return nil
}

func validateRefinementStrategy(templateID string, strategy TemplateRefinementStrategy) error {
	if strategy.CritiqueRounds < 0 || strategy.CorrectionRounds < 0 || strategy.BudgetCapTokens < 0 {
		return fmt.Errorf("template %q refinement strategy cannot use negative limits", templateID)
	}
	for _, mode := range strategy.SearchModes {
		if !validRefinementSearchMode(mode) {
			return fmt.Errorf("template %q refinement search mode %q is invalid", templateID, mode)
		}
	}
	if strategy.FinalJudgeIsolation != "" && !validIsolationLevel(strategy.FinalJudgeIsolation) {
		return fmt.Errorf("template %q refinement final judge isolation %q is invalid", templateID, strategy.FinalJudgeIsolation)
	}
	return nil
}

func validRefinementSearchMode(mode RefinementSearchMode) bool {
	switch mode {
	case RefinementSearchBFSHypothesis, RefinementSearchDFSCorrection:
		return true
	default:
		return false
	}
}

func validIsolationLevel(level IsolationLevel) bool {
	switch level {
	case IsolationNone, IsolationWeak, IsolationStrong:
		return true
	default:
		return false
	}
}
