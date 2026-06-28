package loop

import "strings"

type CheckerVerdict string

const (
	CheckerApproved         CheckerVerdict = "approved"
	CheckerChangesRequested CheckerVerdict = "changes_requested"
	CheckerBlocked          CheckerVerdict = "blocked"
	CheckerNeedsHuman       CheckerVerdict = "needs_human"
)

type IsolationLevel string

const (
	IsolationNone   IsolationLevel = "none"
	IsolationWeak   IsolationLevel = "weak"
	IsolationStrong IsolationLevel = "strong"
)

type HumanGateKind string

const (
	HumanGateGitPush          HumanGateKind = "git_push"
	HumanGateDeleteFiles      HumanGateKind = "delete_files"
	HumanGateCredentialChange HumanGateKind = "credential_change"
	HumanGateBudgetIncrease   HumanGateKind = "budget_increase"
	HumanGateSkillPromotion   HumanGateKind = "skill_promotion"
	HumanGateCheckerVerdict   HumanGateKind = "checker_verdict"
)

type MakerCheckerInput struct {
	Mode            MakerCheckerMode `json:"mode"`
	MakerProvider   string           `json:"makerProvider,omitempty"`
	MakerModel      string           `json:"makerModel,omitempty"`
	CheckerProvider string           `json:"checkerProvider,omitempty"`
	CheckerModel    string           `json:"checkerModel,omitempty"`
	Verdict         CheckerVerdict   `json:"verdict,omitempty"`
	RetryCount      int              `json:"retryCount,omitempty"`
	MaxRetries      int              `json:"maxRetries,omitempty"`
	Reason          string           `json:"reason,omitempty"`
}

type MakerCheckerResult struct {
	Mode            MakerCheckerMode `json:"mode"`
	Verdict         CheckerVerdict   `json:"verdict,omitempty"`
	Isolation       IsolationLevel   `json:"isolation"`
	CanComplete     bool             `json:"canComplete"`
	RetryAllowed    bool             `json:"retryAllowed,omitempty"`
	RetryCount      int              `json:"retryCount,omitempty"`
	MaxRetries      int              `json:"maxRetries,omitempty"`
	MakerProvider   string           `json:"makerProvider,omitempty"`
	MakerModel      string           `json:"makerModel,omitempty"`
	CheckerProvider string           `json:"checkerProvider,omitempty"`
	CheckerModel    string           `json:"checkerModel,omitempty"`
	Reason          string           `json:"reason,omitempty"`
	HumanGate       *HumanGateResult `json:"humanGate,omitempty"`
}

type HumanGateResult struct {
	Kind     HumanGateKind `json:"kind,omitempty"`
	Required bool          `json:"required"`
	Status   string        `json:"status,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

type RefinementInput struct {
	Strategy              TemplateRefinementStrategy `json:"strategy"`
	BudgetRemainingTokens int64                      `json:"budgetRemainingTokens,omitempty"`
	KillSwitchEnabled     bool                       `json:"killSwitchEnabled,omitempty"`
	HumanApproved         bool                       `json:"humanApproved,omitempty"`
}

type RefinementResult struct {
	Enabled             bool                   `json:"enabled"`
	Status              string                 `json:"status"`
	CanStart            bool                   `json:"canStart"`
	SearchModes         []RefinementSearchMode `json:"searchModes,omitempty"`
	CritiqueRounds      int                    `json:"critiqueRounds,omitempty"`
	CorrectionRounds    int                    `json:"correctionRounds,omitempty"`
	FinalJudgeIsolation IsolationLevel         `json:"finalJudgeIsolation,omitempty"`
	BudgetCapTokens     int64                  `json:"budgetCapTokens,omitempty"`
	Reason              string                 `json:"reason,omitempty"`
	HumanGate           *HumanGateResult       `json:"humanGate,omitempty"`
}

func EvaluateMakerChecker(in MakerCheckerInput) MakerCheckerResult {
	mode := in.Mode
	if mode == "" {
		mode = MakerCheckerOff
	}
	result := MakerCheckerResult{
		Mode:            mode,
		Verdict:         in.Verdict,
		Isolation:       makerCheckerIsolation(in),
		RetryCount:      in.RetryCount,
		MaxRetries:      in.MaxRetries,
		MakerProvider:   strings.TrimSpace(in.MakerProvider),
		MakerModel:      strings.TrimSpace(in.MakerModel),
		CheckerProvider: strings.TrimSpace(in.CheckerProvider),
		CheckerModel:    strings.TrimSpace(in.CheckerModel),
		Reason:          strings.TrimSpace(in.Reason),
	}
	switch mode {
	case MakerCheckerOff:
		result.CanComplete = true
	case MakerCheckerReviewOnly:
		result.CanComplete = true
	case MakerCheckerEnforcedBeforeDone:
		applyEnforcedMakerChecker(&result)
	default:
		result.CanComplete = false
		result.HumanGate = &HumanGateResult{
			Kind:     HumanGateCheckerVerdict,
			Required: true,
			Status:   "needs_human",
			Reason:   "maker-checker mode is invalid",
		}
	}
	return result
}

func EvaluateRefinementStrategy(in RefinementInput) RefinementResult {
	strategy := in.Strategy
	result := RefinementResult{
		Enabled:             strategy.Enabled,
		Status:              "off",
		SearchModes:         append([]RefinementSearchMode(nil), strategy.SearchModes...),
		CritiqueRounds:      strategy.CritiqueRounds,
		CorrectionRounds:    strategy.CorrectionRounds,
		FinalJudgeIsolation: strategy.FinalJudgeIsolation,
		BudgetCapTokens:     strategy.BudgetCapTokens,
	}
	if !strategy.Enabled {
		return result
	}
	if strategy.BudgetCapTokens <= 0 {
		result.Status = "blocked"
		result.Reason = "deep refinement requires an explicit budget cap"
		return result
	}
	if strategy.KillSwitchRequired && !in.KillSwitchEnabled {
		result.Status = "blocked"
		result.Reason = "deep refinement requires an enabled kill switch"
		return result
	}
	if in.BudgetRemainingTokens < strategy.BudgetCapTokens {
		result.Status = "blocked"
		result.Reason = "deep refinement budget cap exceeds remaining run budget"
		return result
	}
	if strategy.HumanApprovalRequired && !in.HumanApproved {
		result.Status = "needs_human"
		result.HumanGate = &HumanGateResult{
			Kind:     HumanGateBudgetIncrease,
			Required: true,
			Status:   "needs_human",
			Reason:   "deep refinement requires human approval before spending additional budget",
		}
		return result
	}
	result.Status = "ready"
	result.CanStart = true
	return result
}

func applyEnforcedMakerChecker(result *MakerCheckerResult) {
	switch result.Verdict {
	case CheckerApproved:
		result.CanComplete = true
	case CheckerChangesRequested:
		if result.RetryCount < result.MaxRetries {
			result.RetryAllowed = true
			return
		}
		result.HumanGate = &HumanGateResult{
			Kind:     HumanGateCheckerVerdict,
			Required: true,
			Status:   "needs_human",
			Reason:   "checker requested changes after retry budget was exhausted",
		}
	case CheckerBlocked, CheckerNeedsHuman:
		result.HumanGate = &HumanGateResult{
			Kind:     HumanGateCheckerVerdict,
			Required: true,
			Status:   "needs_human",
			Reason:   "checker verdict requires a human decision",
		}
	default:
		result.HumanGate = &HumanGateResult{
			Kind:     HumanGateCheckerVerdict,
			Required: true,
			Status:   "needs_human",
			Reason:   "enforced maker-checker requires an approved checker verdict",
		}
	}
}

func makerCheckerIsolation(in MakerCheckerInput) IsolationLevel {
	makerProvider := strings.TrimSpace(in.MakerProvider)
	makerModel := strings.TrimSpace(in.MakerModel)
	checkerProvider := strings.TrimSpace(in.CheckerProvider)
	checkerModel := strings.TrimSpace(in.CheckerModel)
	if makerProvider == "" && makerModel == "" && checkerProvider == "" && checkerModel == "" {
		return IsolationNone
	}
	if makerProvider != "" && checkerProvider != "" && makerProvider != checkerProvider {
		return IsolationStrong
	}
	if makerModel != "" && checkerModel != "" && makerModel != checkerModel {
		return IsolationStrong
	}
	return IsolationWeak
}

func EvaluateHumanGate(kind HumanGateKind, policy []HumanGateKind, reason string) HumanGateResult {
	for _, allowed := range policy {
		if allowed == kind {
			return HumanGateResult{
				Kind:     kind,
				Required: true,
				Status:   "needs_human",
				Reason:   strings.TrimSpace(reason),
			}
		}
	}
	return HumanGateResult{Kind: kind}
}
