package skillopt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"maddog/internal/provider"
)

// ProviderProposer asks a configured optimizer model for a structured body
// patch. Frozen capability metadata is copied from Base locally and is never
// accepted from model output.
type ProviderProposer struct {
	Provider  provider.Provider
	Pricing   *provider.Pricing
	ModelRef  string
	MaxTokens int
}

type providerProposalPayload struct {
	CandidateBody string     `json:"candidate_body"`
	Edits         []BodyEdit `json:"edits"`
	Rationale     string     `json:"rationale"`
}

func (p ProviderProposer) Propose(ctx context.Context, req ProposalRequest) (Proposal, error) {
	if p.Provider == nil {
		return Proposal{}, fmt.Errorf("optimizer provider is required")
	}
	maxTokens := p.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 6000
	}
	input, err := proposalPrompt(req)
	if err != nil {
		return Proposal{}, err
	}
	stream, err := p.Provider.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: providerProposalSystemPrompt},
			{Role: provider.RoleUser, Content: input},
		},
		Temperature: 0,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return Proposal{}, err
	}
	var text strings.Builder
	var usage provider.Usage
	for chunk := range stream {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				usage.PromptTokens += chunk.Usage.PromptTokens
				usage.CompletionTokens += chunk.Usage.CompletionTokens
				usage.TotalTokens += chunk.Usage.TotalTokens
				usage.CacheHitTokens += chunk.Usage.CacheHitTokens
				usage.CacheMissTokens += chunk.Usage.CacheMissTokens
			}
		case provider.ChunkError:
			if chunk.Err != nil {
				return Proposal{Cost: providerCost(usage, p.Pricing)}, chunk.Err
			}
		}
	}
	payload, err := decodeProviderProposal(text.String())
	if err != nil {
		return Proposal{Cost: providerCost(usage, p.Pricing)}, err
	}
	candidate := cloneSkill(req.Base.Artifact.Skill)
	candidate.Body = payload.CandidateBody
	return Proposal{
		Candidate: candidate,
		Edits:     append([]BodyEdit(nil), payload.Edits...),
		Rationale: strings.TrimSpace(payload.Rationale),
		ModelRef:  firstNonEmpty(p.ModelRef, req.ModelRef, p.Provider.Name()),
		Cost:      providerCost(usage, p.Pricing),
	}, nil
}

const providerProposalSystemPrompt = `You optimize a reusable Maddog skill from training rollouts.
Return exactly one JSON object with this schema:
{"candidate_body":"complete resulting skill body","edits":[{"start":0,"end":0,"replacement":"text"}],"rationale":"short reason"}

Rules:
- Edit only the skill body. Never change its name, description, allowed tools, run mode, model, effort, path, or scope.
- Byte offsets are zero-based half-open offsets into the current UTF-8 body and must be ordered and non-overlapping.
- The edits must reproduce candidate_body exactly when applied.
- Learn only from the supplied training cases and rollouts. Do not infer validation or test answers.
- Prefer small, general instructions over case-specific memorization.
- Do not include Markdown fences or text outside the JSON object.`

func proposalPrompt(req ProposalRequest) (string, error) {
	type sample struct {
		ID       string          `json:"id"`
		Input    string          `json:"input"`
		Expected json.RawMessage `json:"expected,omitempty"`
		Hard     bool            `json:"hard"`
		Soft     float64         `json:"soft"`
		Output   string          `json:"output,omitempty"`
		Evidence json.RawMessage `json:"evidence,omitempty"`
	}
	results := make(map[string]EvaluationRecord, len(req.TrainResult))
	for _, record := range req.TrainResult {
		results[record.CaseID] = record
	}
	samples := make([]sample, 0, len(req.TrainCases))
	for _, c := range req.TrainCases {
		record := results[c.ID]
		output := redactOptimizationText(record.Result.Output)
		if len(output) > 8000 {
			output = output[:8000] + "\n[truncated]"
		}
		evidence := append(json.RawMessage(nil), record.Result.Evidence...)
		if len(evidence) > 8000 {
			evidence = nil
		}
		expected := append(json.RawMessage(nil), c.Expected...)
		if len(expected) > 0 {
			var decoded any
			if json.Unmarshal(expected, &decoded) == nil {
				if encoded, encodeErr := json.Marshal(redactJSONValue(decoded)); encodeErr == nil {
					expected = encoded
				}
			} else {
				expected = json.RawMessage(redactOptimizationText(string(expected)))
			}
		}
		samples = append(samples, sample{
			ID: c.ID, Input: redactOptimizationText(c.Input), Expected: expected,
			Hard: record.Result.Hard, Soft: record.Result.Soft, Output: output, Evidence: evidence,
		})
	}
	encoded, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Run: %s\nRound: %d\nEdit limits: max_edits=%d max_changed_bytes=%d max_body_bytes=%d\n\nCurrent skill body (%d bytes):\n%s\n\nTraining cases and rollouts:\n%s",
		req.RunID, req.Round, req.Limits.MaxEdits, req.Limits.MaxChangedBytes, req.Limits.MaxBodyBytes,
		len(req.Base.Artifact.Skill.Body), req.Base.Artifact.Skill.Body, encoded), nil
}

func decodeProviderProposal(raw string) (providerProposalPayload, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if newline := strings.IndexByte(raw, '\n'); newline >= 0 {
			raw = raw[newline+1:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
	}
	start, end := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return providerProposalPayload{}, fmt.Errorf("%w: optimizer returned no JSON object", ErrInvalidProposal)
	}
	var payload providerProposalPayload
	decoder := json.NewDecoder(strings.NewReader(raw[start : end+1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return providerProposalPayload{}, fmt.Errorf("%w: decode optimizer response: %v", ErrInvalidProposal, err)
	}
	if strings.TrimSpace(payload.CandidateBody) == "" || len(payload.Edits) == 0 {
		return providerProposalPayload{}, fmt.Errorf("%w: optimizer response requires candidate_body and edits", ErrInvalidProposal)
	}
	return payload, nil
}

func providerCost(usage provider.Usage, pricing *provider.Pricing) Cost {
	return Cost{InputTokens: int64(usage.PromptTokens), OutputTokens: int64(usage.CompletionTokens), Amount: pricing.Cost(&usage)}
}
