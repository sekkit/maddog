package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"maddog/internal/processlock"
)

type startupState struct {
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	LastLaunch          time.Time `json:"lastLaunch"`
	ProbationUntil      time.Time `json:"probationUntil"`
	Phase               string    `json:"phase"`
}

// GuardRelaunchExitCode asks the packaged guard to start a fresh payload while
// retaining the desktop runtime lease across the handoff.
const GuardRelaunchExitCode = 75

// StartupStatus is the persisted launch-health summary exposed to callers.
type StartupStatus struct {
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	Phase               string    `json:"phase"`
	LastLaunch          time.Time `json:"lastLaunch"`
}

// StartupGuard records launch health and selects Safe Mode after repeated failures.
type StartupGuard struct {
	path      string
	threshold int
	probation time.Duration
	now       func() time.Time
}

// NewStartupGuard creates a guard backed by path with bounded failure defaults.
func NewStartupGuard(path string, threshold int, probation time.Duration) *StartupGuard {
	if threshold < 1 {
		threshold = 3
	}
	if probation <= 0 {
		probation = 2 * time.Minute
	}
	return &StartupGuard{path: path, threshold: threshold, probation: probation, now: time.Now}
}

func (g *StartupGuard) read() startupState {
	raw, err := os.ReadFile(g.path)
	if err != nil {
		return startupState{}
	}
	var s startupState
	if json.Unmarshal(raw, &s) != nil {
		return startupState{}
	}
	return s
}
func (g *StartupGuard) write(s startupState) error {
	if err := os.MkdirAll(filepath.Dir(g.path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(g.path), ".startup-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, g.path)
}

func (g *StartupGuard) withLock(fn func(startupState) (startupState, error)) error {
	if err := os.MkdirAll(filepath.Dir(g.path), 0o700); err != nil {
		return err
	}
	lock, acquired, err := processlock.Try(g.path + ".lock")
	if err != nil {
		return fmt.Errorf("startup state locked: %w", err)
	}
	if !acquired {
		return fmt.Errorf("startup state locked by another process")
	}
	defer lock.Release()
	next, err := fn(g.read())
	if err != nil {
		return err
	}
	return g.write(next)
}

// Begin records a launch before optional components are initialized. Reaching
// the threshold enters Safe Mode for this process and a short probation window.
func (g *StartupGuard) Begin() (bool, error) {
	var safe bool
	err := g.withLock(func(s startupState) (startupState, error) {
		now := g.now()
		if s.Phase == "healthy" || s.Phase == "clean" || (!s.ProbationUntil.IsZero() && now.After(s.ProbationUntil)) {
			s.ConsecutiveFailures = 0
		}
		s.ConsecutiveFailures++
		s.LastLaunch = now
		s.Phase = "launching"
		safe = s.ConsecutiveFailures >= g.threshold
		if safe {
			s.ProbationUntil = now.Add(g.probation)
		}
		return s, nil
	})
	return safe, err
}
func (g *StartupGuard) mark(phase string, reset bool) error {
	return g.withLock(func(s startupState) (startupState, error) {
		s.Phase = phase
		if reset {
			s.ConsecutiveFailures = 0
			s.ProbationUntil = time.Time{}
		}
		return s, nil
	})
}

// MarkReady records that the payload initialized its required components.
func (g *StartupGuard) MarkReady() error { return g.mark("ready", false) }

// MarkHealthy records a healthy launch and clears the failure count.
func (g *StartupGuard) MarkHealthy() error { return g.mark("healthy", true) }

// MarkClean records a clean shutdown and clears the failure count.
func (g *StartupGuard) MarkClean() error { return g.mark("clean", true) }

// Status returns the current persisted startup health.
func (g *StartupGuard) Status() StartupStatus {
	s := g.read()
	return StartupStatus{ConsecutiveFailures: s.ConsecutiveFailures, Phase: s.Phase, LastLaunch: s.LastLaunch}
}

// Reset clears startup failures and records a clean state.
func (g *StartupGuard) Reset() error {
	return g.withLock(func(startupState) (startupState, error) { return startupState{Phase: "clean"}, nil })
}
