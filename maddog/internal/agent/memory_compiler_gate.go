package agent

import (
	"context"
	"strings"
	"time"

	"maddog/internal/memorycompiler"
)

// SetMemoryCompiler swaps the Memory v5 runtime used for future turns.
func (a *Agent) SetMemoryCompiler(rt *memorycompiler.Runtime) {
	if a == nil {
		return
	}
	a.memoryCompilerMu.Lock()
	a.memoryCompiler = rt
	a.compilerTurn = nil
	a.memoryCompilerMu.Unlock()
	a.resetMemoryCompilerInjectionGate()
	a.clearClassifierCache()
}

func (a *Agent) memoryCompilerRuntime() *memorycompiler.Runtime {
	if a == nil {
		return nil
	}
	a.memoryCompilerMu.RLock()
	defer a.memoryCompilerMu.RUnlock()
	return a.memoryCompiler
}

func shouldStartMemoryCompiler(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	return strings.TrimSpace(StripTransientUserBlocks(input)) != ""
}

func shouldInjectMemoryCompilerContractForInput(input string) bool {
	if !shouldStartMemoryCompiler(input) {
		return false
	}
	ok, err := newHeuristicClassifier().IsTask(context.Background(), input)
	return err == nil && ok
}

func (a *Agent) tryMarkMemoryCompilerInjected(now time.Time) bool {
	if a == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	a.compilerInjectionMu.Lock()
	defer a.compilerInjectionMu.Unlock()
	if a.compilerInjectionCount >= memoryCompilerInjectionMax {
		return false
	}
	if !a.lastCompilerInjectedAt.IsZero() && now.Sub(a.lastCompilerInjectedAt) < memoryCompilerInjectionCooldown {
		return false
	}
	a.lastCompilerInjectedAt = now
	a.compilerInjectionCount++
	return true
}

func (a *Agent) resetMemoryCompilerInjectionGate() {
	if a == nil {
		return
	}
	a.compilerInjectionMu.Lock()
	a.lastCompilerInjectedAt = time.Time{}
	a.compilerInjectionCount = 0
	a.compilerInjectionMu.Unlock()
}

func (a *Agent) clearClassifierCache() {
	if a == nil || a.classifier == nil {
		return
	}
	if classifier, ok := a.classifier.(*llmClassifier); ok && classifier.cache != nil {
		classifier.cache.Clear()
	}
}
