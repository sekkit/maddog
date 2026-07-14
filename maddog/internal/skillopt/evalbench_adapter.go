package skillopt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"maddog/internal/evalbench"
)

// EvalbenchExecutor adapts disposable Maddog benchmark tasks to the optimizer's
// rollout interface. Task verifiers remain hidden until the child agent exits.
type EvalbenchExecutor struct {
	Binary        string
	Model         string
	ProjectConfig string
	Tasks         map[string]evalbench.Task
	KeepWorkspace bool
	Environment   []string
}

func NewEvalbenchExecutor(tasks []evalbench.Task, binary, model string) *EvalbenchExecutor {
	byID := make(map[string]evalbench.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	return &EvalbenchExecutor{Binary: binary, Model: model, Tasks: byID}
}

func (e *EvalbenchExecutor) Evaluate(ctx context.Context, req RolloutRequest) (Result, error) {
	if e == nil {
		return Result{}, fmt.Errorf("evalbench executor is nil")
	}
	taskID := strings.TrimSpace(req.Case.Metadata["task_id"])
	if taskID == "" {
		taskID = strings.TrimSpace(req.Case.ID)
	}
	task, ok := e.Tasks[taskID]
	if !ok {
		return Result{}, fmt.Errorf("benchmark task %q is not loaded", taskID)
	}
	// The manifest owns the immutable case input and split. The task directory
	// owns only the seed workspace and hidden verifier.
	task.Prompt = req.Case.Input
	result, err := evalbench.Run(ctx, task, evalbench.RunOptions{
		Binary: e.Binary, Model: firstNonEmpty(req.ModelRef, e.Model), Skill: req.Skill,
		ProjectConfig: e.ProjectConfig, KeepWorkspace: e.KeepWorkspace, Environment: append([]string(nil), e.Environment...),
	})
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	evidence, _ := json.Marshal(struct {
		RunError   string `json:"run_error,omitempty"`
		GradeError string `json:"grade_error,omitempty"`
		Trace      string `json:"trace,omitempty"`
	}{RunError: result.RunError, GradeError: result.GradeError, Trace: result.Trace})
	return Result{
		Hard: result.Passed && result.Hard >= 1,
		Soft: result.Soft,
		Cost: Cost{
			InputTokens:  int64(result.Metrics.PromptTokens),
			OutputTokens: int64(result.Metrics.CompletionTokens),
			Amount:       result.Metrics.Cost,
		},
		Output: result.Trace, Evidence: evidence, ModelRef: firstNonEmpty(req.ModelRef, e.Model),
	}, nil
}
