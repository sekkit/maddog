package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"maddog/internal/agent/testutil"
	"maddog/internal/event"
	"maddog/internal/evidence"
	"maddog/internal/provider"
)

func TestNativeAdvisorIterationsEmitIndependentUsageWithoutDoubleCountingBlocks(t *testing.T) {
	blockA := json.RawMessage(`{ "type": "advisor_tool_result", "tool_use_id": "advisor-a", "content": [{"type":"text","text":"A"}] }`)
	blockB := json.RawMessage(`{"type":"advisor_tool_result","tool_use_id":"advisor-b","content":[{"type":"text","text":"B"}]}`)
	usage := &provider.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		FinishReason:     "stop",
		Iterations: []provider.UsageIteration{
			{Type: "advisor_message", Model: "advisor-a-model", InputTokens: 10, OutputTokens: 4, CacheCreationInputTokens: 3, CacheReadInputTokens: 5},
			{Type: "advisor_message", Model: "advisor-b-model", InputTokens: 7, OutputTokens: 2, CacheCreationInputTokens: 1, CacheReadInputTokens: 6},
			{Type: "message", Model: "executor-model", InputTokens: 100, OutputTokens: 20},
		},
	}
	mp := testutil.NewMock("anthropic", testutil.Turn{Chunks: []provider.Chunk{
		{Type: provider.ChunkNativeBlock, NativeBlock: blockA},
		{Type: provider.ChunkNativeBlock, NativeBlock: blockA},
		{Type: provider.ChunkNativeBlock, NativeBlock: blockB},
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkUsage, Usage: usage},
		{Type: provider.ChunkDone},
	}})
	sink := &recordSink{}
	pricing := &provider.Pricing{Input: 5, Output: 15, CacheHit: 0.5, Currency: "$"}
	a := New(mp, echoRegistry(), NewSession(""), Options{
		Advisor:              AdvisorConfig{MaxUsesPerTurn: 4, MaxUsesPerSession: 6},
		NativeAdvisor:        &provider.NativeAdvisorConfig{Model: "configured-advisor", MaxUses: 4},
		NativeAdvisorPricing: pricing,
	}, sink)

	if err := a.Run(context.Background(), "review this security-sensitive change"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.advisorTurnUses != 2 || a.advisorSessionUses != 2 {
		t.Fatalf("native advisor uses = turn %d session %d, want 2/2", a.advisorTurnUses, a.advisorSessionUses)
	}
	advisorEvents := sink.kinds(event.Advisor)
	if len(advisorEvents) != 2 || advisorEvents[1].Advisor.UsesThisTurn != 2 || advisorEvents[1].Advisor.UsesThisSession != 2 {
		t.Fatalf("advisor events = %+v, want two cumulative native uses", advisorEvents)
	}

	var advisorUsage []event.Event
	for _, ev := range sink.kinds(event.Usage) {
		if ev.UsageSource == event.UsageSourceAdvisor {
			advisorUsage = append(advisorUsage, ev)
		}
	}
	if len(advisorUsage) != 2 {
		t.Fatalf("advisor usage events = %d, want one per advisor iteration", len(advisorUsage))
	}
	first := advisorUsage[0]
	if first.Profile == nil || first.Profile.Role != event.UsageSourceAdvisor || first.Profile.Model != "advisor-a-model" {
		t.Fatalf("advisor usage profile = %+v", first.Profile)
	}
	if first.Pricing != pricing || first.Usage.PromptTokens != 18 || first.Usage.CompletionTokens != 4 || first.Usage.TotalTokens != 22 || first.Usage.CacheHitTokens != 5 || first.Usage.CacheMissTokens != 13 {
		t.Fatalf("first advisor usage = %+v pricing=%p, want mapped iteration and configured pricing %p", first.Usage, first.Pricing, pricing)
	}

	messages := a.Session().Messages
	last := messages[len(messages)-1]
	if len(last.NativeBlocks) != 3 || !bytes.Equal(last.NativeBlocks[0], blockA) || !bytes.Equal(last.NativeBlocks[2], blockB) {
		t.Fatalf("native blocks were not preserved verbatim: %q", last.NativeBlocks)
	}
}

func TestNativeAdvisorResultBlocksCountOnceAcrossResponses(t *testing.T) {
	block := json.RawMessage(`{"type":"advisor_tool_result","tool_use_id":"stable-id","content":[{"type":"text","text":"advice"}]}`)
	turn := func(text string) testutil.Turn {
		return testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkNativeBlock, NativeBlock: block},
			{Type: provider.ChunkNativeBlock, NativeBlock: block},
			{Type: provider.ChunkText, Text: text},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, FinishReason: "stop"}},
			{Type: provider.ChunkDone},
		}}
	}
	mp := testutil.NewMock("anthropic", turn("first"), turn("second"))
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{
		Advisor:       AdvisorConfig{MaxUsesPerTurn: 3, MaxUsesPerSession: 4},
		NativeAdvisor: &provider.NativeAdvisorConfig{Model: "advisor", MaxUses: 3},
	}, sink)

	if err := a.Run(context.Background(), "first turn"); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	if err := a.Run(context.Background(), "second turn"); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if a.advisorTurnUses != 0 || a.advisorSessionUses != 1 {
		t.Fatalf("deduplicated result-block uses = turn %d session %d, want 0/1 after second turn", a.advisorTurnUses, a.advisorSessionUses)
	}
	if got := len(sink.kinds(event.Advisor)); got != 1 {
		t.Fatalf("native advisor events = %d, want repeated result id counted once", got)
	}
	var advisorUsage int
	for _, ev := range sink.kinds(event.Usage) {
		if ev.UsageSource == event.UsageSourceAdvisor {
			advisorUsage++
		}
	}
	if advisorUsage != 1 {
		t.Fatalf("result-only advisor usage events = %d, want 1", advisorUsage)
	}
	restored := New(testutil.NewMock("restored"), echoRegistry(), a.Session(), Options{
		Advisor:       AdvisorConfig{MaxUsesPerTurn: 3, MaxUsesPerSession: 1},
		NativeAdvisor: &provider.NativeAdvisorConfig{Model: "advisor", MaxUses: 3},
	}, event.Discard)
	if restored.advisorSessionUses != 1 || restored.nativeAdvisorForRequest() != nil {
		t.Fatalf("restored session native budget = uses %d config %+v, want historical result to exhaust cap", restored.advisorSessionUses, restored.nativeAdvisorForRequest())
	}
}

func TestPauseTurnContinuesWithoutUserMessageOrToolRound(t *testing.T) {
	block := json.RawMessage(`{ "type":"advisor_tool_result", "tool_use_id":"pause-1", "content":[{"type":"text","text":"continue"}] }`)
	mp := testutil.NewMock("anthropic",
		testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkNativeBlock, NativeBlock: block},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3, FinishReason: "pause_turn"}},
			{Type: provider.ChunkDone},
		}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "echo-1", Name: "echo", Arguments: `{"text":"ok"}`}}},
		testutil.Turn{Text: "finished"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MaxSteps:      1,
		Advisor:       AdvisorConfig{MaxUsesPerTurn: 3, MaxUsesPerSession: 3},
		NativeAdvisor: &provider.NativeAdvisorConfig{Model: "advisor", MaxUses: 3},
	}, event.Discard)

	if err := a.Run(context.Background(), "do the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if mp.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want pause continuation + one tool round + final", mp.CallCount())
	}
	reqs := mp.Requests()
	continued := reqs[1]
	last := continued.Messages[len(continued.Messages)-1]
	if last.Role != provider.RoleAssistant || len(last.NativeBlocks) != 1 || !bytes.Equal(last.NativeBlocks[0], block) {
		t.Fatalf("pause continuation did not end in the verbatim assistant block: %+v", last)
	}
	if continued.NativeAdvisor == nil || continued.NativeAdvisor.MaxUses != 2 {
		t.Fatalf("pause continuation native advisor budget = %+v, want remaining uses 2", continued.NativeAdvisor)
	}
	userMessages := 0
	for _, msg := range continued.Messages {
		if msg.Role == provider.RoleUser {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("pause continuation user messages = %d, want only the original task", userMessages)
	}
}

func TestPauseTurnLoopIsBounded(t *testing.T) {
	turns := make([]testutil.Turn, 0, maxPauseTurnContinuations+1)
	for i := 0; i <= maxPauseTurnContinuations; i++ {
		block := json.RawMessage(fmt.Sprintf(`{"type":"advisor_tool_result","tool_use_id":"pause-%d","content":[]}`, i))
		turns = append(turns, testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkNativeBlock, NativeBlock: block},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 1, FinishReason: "pause_turn"}},
			{Type: provider.ChunkDone},
		}})
	}
	mp := testutil.NewMock("anthropic", turns...)
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MaxSteps:      1,
		Advisor:       AdvisorConfig{MaxUsesPerTurn: 10, MaxUsesPerSession: 10},
		NativeAdvisor: &provider.NativeAdvisorConfig{Model: "advisor", MaxUses: 10},
	}, event.Discard)

	err := a.Run(context.Background(), "continue native advisor")
	if err == nil || !strings.Contains(err.Error(), "pause_turn repeated") {
		t.Fatalf("Run error = %v, want bounded pause_turn failure", err)
	}
	if mp.CallCount() != maxPauseTurnContinuations+1 {
		t.Fatalf("provider calls = %d, want bounded at %d", mp.CallCount(), maxPauseTurnContinuations+1)
	}
	userMessages := 0
	for _, msg := range a.Session().Messages {
		if msg.Role == provider.RoleUser {
			userMessages++
		}
	}
	if userMessages != 1 {
		t.Fatalf("bounded pause loop inserted user messages: %+v", a.Session().Messages)
	}
}

func TestAdvisorStrategyDomainAndCuratedContext(t *testing.T) {
	t.Run("stable strategy is sent with configured advisor", func(t *testing.T) {
		mp := testutil.NewMock("default", testutil.Turn{Text: "done"})
		a := New(mp, echoRegistry(), NewSession("base system"), Options{
			Advisor:       AdvisorConfig{MaxUsesPerTurn: 1},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) { return "unused", nil },
		}, event.Discard)
		if err := a.Run(context.Background(), "simple task"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		req := mp.LastRequest()
		if req == nil || len(req.Messages) == 0 || req.Messages[0].Role != provider.RoleSystem || !strings.Contains(req.Messages[0].Content, AdvisorStrategyPrompt) {
			t.Fatalf("request missing stable advisor strategy: %+v", req)
		}
	})

	t.Run("domain inference and focus", func(t *testing.T) {
		cases := []struct {
			text string
			want AdvisorDomain
		}{
			{"design service boundaries for a migration", AdvisorDomainArchitecture},
			{"audit authorization and secret handling", AdvisorDomainSecurity},
			{"reduce latency after benchmark regression", AdvisorDomainPerformance},
			{"debug a flaky parser crash", AdvisorDomainDebugging},
			{"refactor for maintainability and test coverage", AdvisorDomainCodeQuality},
			{"summarize the release notes", AdvisorDomainGeneral},
		}
		for _, tc := range cases {
			if got := inferAdvisorDomain(tc.text); got != tc.want {
				t.Errorf("inferAdvisorDomain(%q) = %q, want %q", tc.text, got, tc.want)
			}
		}
		task := FormatAdvisorTask(AdvisorRequest{Question: "Audit auth", Domain: AdvisorDomainSecurity})
		if !strings.Contains(task, "Domain: security") || !strings.Contains(task, "trust boundaries") {
			t.Fatalf("security advisor task missing focus hint:\n%s", task)
		}
	})

	t.Run("context keeps only bounded decision and result evidence", func(t *testing.T) {
		session := NewSession("SYSTEM_SECRET_SHOULD_NOT_LEAK")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "Preserve the public API while fixing the parser."})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "Decision: keep the compatibility adapter.", ToolCalls: []provider.ToolCall{{Name: "write_file", Arguments: `{"secret":"TOOL_ARGS_SHOULD_NOT_LEAK"}`}}})
		for i := 0; i < 5; i++ {
			session.Add(provider.Message{Role: provider.RoleTool, Name: "read_file", Content: fmt.Sprintf("result-%d", i)})
		}
		session.Add(provider.Message{Role: provider.RoleTool, Name: "bash", Content: "error: parser test failed"})
		a := New(testutil.NewMock("mock"), echoRegistry(), session, Options{
			Advisor: AdvisorConfig{MaxUsesPerTurn: 1, MaxContextMessages: 20, MaxContextChars: 1000},
		}, event.Discard)
		a.advisorTurnInput = "Fix the parser without changing the public API."
		ctx := a.curateAdvisorContext(evidence.FailureSignal{ErrorStreak: 1, LastErrorTool: "bash"})
		for _, forbidden := range []string{"SYSTEM_SECRET_SHOULD_NOT_LEAK", "TOOL_ARGS_SHOULD_NOT_LEAK", "result-0", "result-1"} {
			if strings.Contains(ctx, forbidden) {
				t.Fatalf("curated context leaked %q:\n%s", forbidden, ctx)
			}
		}
		for _, want := range []string{"Task and constraints", "Recent decisions", "Recent errors", "result-4"} {
			if !strings.Contains(ctx, want) {
				t.Fatalf("curated context missing %q:\n%s", want, ctx)
			}
		}
		a.advisor.MaxContextChars = 160
		bounded := a.curateAdvisorContext(evidence.FailureSignal{ErrorStreak: 1, LastErrorTool: "bash"})
		if len(bounded) > 160 || !utf8.ValidString(bounded) {
			t.Fatalf("bounded context length/UTF-8 invalid: len=%d valid=%v %q", len(bounded), utf8.ValidString(bounded), bounded)
		}
	})
}

func TestAdvisorExplicitSkipIsPerTurnAndHardSuppressesBothPaths(t *testing.T) {
	t.Run("native", func(t *testing.T) {
		mp := testutil.NewMock("anthropic", testutil.Turn{Text: "skipped"}, testutil.Turn{Text: "enabled"})
		a := New(mp, echoRegistry(), NewSession(""), Options{
			Advisor:       AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 2},
			NativeAdvisor: &provider.NativeAdvisorConfig{Model: "advisor", MaxUses: 1},
		}, event.Discard)
		if err := a.Run(context.Background(), "Do this, but skip advisor."); err != nil {
			t.Fatalf("skip Run: %v", err)
		}
		if err := a.Run(context.Background(), "Now use the normal policy."); err != nil {
			t.Fatalf("enabled Run: %v", err)
		}
		reqs := mp.Requests()
		if reqs[0].NativeAdvisor != nil || reqs[1].NativeAdvisor == nil {
			t.Fatalf("native advisor per-turn suppression = first %+v second %+v", reqs[0].NativeAdvisor, reqs[1].NativeAdvisor)
		}
	})

	t.Run("fallback", func(t *testing.T) {
		mp := testutil.NewMock("default", testutil.Turn{Text: "skipped"}, testutil.Turn{Text: "enabled"})
		a := New(mp, echoRegistry(), NewSession(""), Options{
			Advisor:       AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 2},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) { return "unused", nil },
		}, event.Discard)
		if err := a.Run(context.Background(), "Do this without advisor."); err != nil {
			t.Fatalf("skip Run: %v", err)
		}
		if err := a.Run(context.Background(), "Now use the normal policy."); err != nil {
			t.Fatalf("enabled Run: %v", err)
		}
		reqs := mp.Requests()
		if hasToolSchema(reqs[0].Tools, "advisor") || !hasToolSchema(reqs[1].Tools, "advisor") {
			t.Fatalf("fallback advisor per-turn schemas = first %+v second %+v", reqs[0].Tools, reqs[1].Tools)
		}
	})

	t.Run("automatic consultation", func(t *testing.T) {
		defaultProv := testutil.NewMock("default", testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "fail", Name: "write_file", Arguments: `{}`}}})
		frontierProv := testutil.NewMock("frontier", testutil.Turn{Text: "frontier"})
		reg := echoRegistry()
		reg.Add(failTool{name: "write_file"})
		advisorCalls := 0
		a := New(defaultProv, reg, NewSession(""), Options{
			UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
			FrontierProvider: frontierProv,
			Advisor:          AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 1},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
				advisorCalls++
				return "should not run", nil
			},
		}, event.Discard)
		if err := a.Run(context.Background(), "Fix it, but do not consult advisor."); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if advisorCalls != 0 || defaultProv.CallCount() != 1 || frontierProv.CallCount() != 1 {
			t.Fatalf("skip routing calls = advisor %d default %d frontier %d", advisorCalls, defaultProv.CallCount(), frontierProv.CallCount())
		}
	})
}

func TestFallbackAdvisorRetryAndFailureRouting(t *testing.T) {
	t.Run("successful correction stays on default", func(t *testing.T) {
		defaultProv := testutil.NewMock("default",
			testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "fail", Name: "write_file", Arguments: `{}`}}},
			testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "recover", Name: "echo", Arguments: `{"text":"fixed"}`}}},
			testutil.Turn{Text: "fixed on default"},
		)
		frontierProv := testutil.NewMock("frontier")
		reg := echoRegistry()
		reg.Add(failTool{name: "write_file"})
		a := New(defaultProv, reg, NewSession(""), Options{
			UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
			FrontierProvider: frontierProv,
			Advisor:          AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 1},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
				return "1. Correct the arguments.\nRisks: verify the result.", nil
			},
		}, event.Discard)
		if err := a.Run(context.Background(), "fix it"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if defaultProv.CallCount() != 3 || frontierProv.CallCount() != 0 {
			t.Fatalf("provider calls = default %d frontier %d, want corrected on default", defaultProv.CallCount(), frontierProv.CallCount())
		}
	})

	t.Run("consultation failure upgrades immediately", func(t *testing.T) {
		defaultProv := testutil.NewMock("default", testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "fail", Name: "write_file", Arguments: `{}`}}})
		frontierProv := testutil.NewMock("frontier", testutil.Turn{Text: "frontier"})
		reg := echoRegistry()
		reg.Add(failTool{name: "write_file"})
		sink := &recordSink{}
		a := New(defaultProv, reg, NewSession(""), Options{
			UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
			FrontierProvider: frontierProv,
			Advisor:          AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 1},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
				return "", errors.New("advisor unavailable")
			},
		}, sink)
		if err := a.Run(context.Background(), "fix it"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if defaultProv.CallCount() != 1 || frontierProv.CallCount() != 1 {
			t.Fatalf("provider calls = default %d frontier %d, want immediate upgrade", defaultProv.CallCount(), frontierProv.CallCount())
		}
		advisorEvents := sink.kinds(event.Advisor)
		if len(advisorEvents) != 1 || advisorEvents[0].Level != event.LevelWarn {
			t.Fatalf("advisor failure events = %+v", advisorEvents)
		}
	})

	t.Run("difficult decision remains advisor only", func(t *testing.T) {
		defaultProv := testutil.NewMock("default", testutil.Turn{Text: "default decision"})
		frontierProv := testutil.NewMock("frontier")
		advisorCalls := 0
		a := New(defaultProv, echoRegistry(), NewSession(""), Options{
			UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
			FrontierProvider: frontierProv,
			Advisor:          AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 1},
			AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
				advisorCalls++
				return "1. Choose the reversible option.\nRisks: migration cost.", nil
			},
		}, event.Discard)
		a.RecordControlSignal(evidence.FailureSignal{DifficultDecision: true, DecisionSummary: "choose a migration boundary"})
		if err := a.Run(context.Background(), "continue"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if advisorCalls != 1 || defaultProv.CallCount() != 1 || frontierProv.CallCount() != 0 {
			t.Fatalf("difficult decision calls = advisor %d default %d frontier %d", advisorCalls, defaultProv.CallCount(), frontierProv.CallCount())
		}
	})
}

func TestNativeAdvisorRoutingRequestsNativeConsultWithoutFallbackDoubleCall(t *testing.T) {
	defaultProv := testutil.NewMock("anthropic",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "fail", Name: "write_file", Arguments: `{}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "recover", Name: "echo", Arguments: `{"text":"fixed"}`}}},
		testutil.Turn{Text: "fixed on default"},
	)
	frontierProv := testutil.NewMock("frontier")
	reg := echoRegistry()
	reg.Add(failTool{name: "write_file"})
	fallbackCalls := 0
	a := New(defaultProv, reg, NewSession(""), Options{
		UpgradePolicy:        ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
		FrontierProvider:     frontierProv,
		Advisor:              AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 2},
		NativeAdvisor:        &provider.NativeAdvisorConfig{Model: "claude-opus-4-8", MaxUses: 1},
		NativeAdvisorPricing: &provider.Pricing{Input: 5, Output: 25},
		AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
			fallbackCalls++
			return "fallback should not run", nil
		},
	}, event.Discard)

	if err := a.Run(context.Background(), "fix it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fallbackCalls != 0 || frontierProv.CallCount() != 0 || defaultProv.CallCount() != 3 {
		t.Fatalf("calls = fallback %d frontier %d default %d, want native nudge and default recovery", fallbackCalls, frontierProv.CallCount(), defaultProv.CallCount())
	}
	reqs := defaultProv.Requests()
	if reqs[1].NativeAdvisor == nil {
		t.Fatal("native advisor was not exposed on the advised retry")
	}
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != provider.RoleUser || !strings.HasPrefix(last.Content, nativeAdvisorNudgePrefix) {
		t.Fatalf("native retry did not receive a consultation nudge: %+v", last)
	}
}

func TestInitialRoutingAppliesAdvisorBeforeFrontier(t *testing.T) {
	defaultProv := testutil.NewMock("default", testutil.Turn{Text: "resolved on default"})
	frontierProv := testutil.NewMock("frontier")
	advisorCalls := 0
	a := New(defaultProv, echoRegistry(), NewSession(""), Options{
		UpgradePolicy:    ThresholdUpgradePolicy{Threshold: 1, TargetModel: "frontier"},
		FrontierProvider: frontierProv,
		Advisor:          AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 2},
		AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) {
			advisorCalls++
			return "1. Break the acceptance loop.\nRisks: verify the new condition.", nil
		},
	}, event.Discard)
	a.RecordControlSignal(evidence.FailureSignal{GoalAcceptanceLoop: 1})

	if err := a.Run(context.Background(), "continue safely"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if advisorCalls != 1 || defaultProv.CallCount() != 1 || frontierProv.CallCount() != 0 {
		t.Fatalf("calls = advisor %d default %d frontier %d, want advised default attempt", advisorCalls, defaultProv.CallCount(), frontierProv.CallCount())
	}
	var sawGuidance bool
	for _, msg := range defaultProv.Requests()[0].Messages {
		if msg.Role == provider.RoleUser && strings.HasPrefix(msg.Content, "Advisor guidance") {
			sawGuidance = true
		}
	}
	if !sawGuidance {
		t.Fatalf("default request did not include initial advisor guidance: %+v", defaultProv.Requests()[0].Messages)
	}
}

func TestFallbackAdvisorSessionBudgetRestoresFromGuidance(t *testing.T) {
	session := NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "Advisor guidance (review):\n\nUse the safer path."})
	a := New(testutil.NewMock("default"), echoRegistry(), session, Options{
		Advisor:       AdvisorConfig{MaxUsesPerTurn: 1, MaxUsesPerSession: 1},
		AdvisorRunner: func(context.Context, AdvisorRequest) (string, error) { return "unused", nil },
	}, event.Discard)
	if a.advisorSessionUses != 1 {
		t.Fatalf("restored fallback advisor uses = %d, want 1", a.advisorSessionUses)
	}
	turn, sessionRemaining := a.advisorRemaining()
	if turn != 0 || sessionRemaining != 0 {
		t.Fatalf("restored fallback budget = turn %d session %d, want exhausted", turn, sessionRemaining)
	}
}

func hasToolSchema(schemas []provider.ToolSchema, name string) bool {
	for _, schema := range schemas {
		if schema.Name == name {
			return true
		}
	}
	return false
}
