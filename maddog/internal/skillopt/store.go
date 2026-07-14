package skillopt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var validRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// JSONRunStore persists one atomically replaced JSON checkpoint per run.
type JSONRunStore struct {
	dir string
	mu  sync.Mutex
}

func NewJSONRunStore(dir string) *JSONRunStore {
	return &JSONRunStore{dir: dir}
}

func (s *JSONRunStore) Create(ctx context.Context, run *Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil || !validRunID.MatchString(run.ID) {
		return fmt.Errorf("invalid run ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create skillopt store: %w", err)
	}
	path := s.path(run.ID)
	if _, err := os.Stat(path); err == nil {
		return ErrRunExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect skillopt checkpoint: %w", err)
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	run.UpdatedAt = run.CreatedAt
	run.Checkpoint = 1
	return writeAtomic(path, run)
}

func (s *JSONRunStore) Load(ctx context.Context, id string) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validRunID.MatchString(id) {
		return nil, ErrRunNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(id)
}

func (s *JSONRunStore) Save(ctx context.Context, run *Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if run == nil || !validRunID.MatchString(run.ID) {
		return fmt.Errorf("invalid run ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.loadUnlocked(run.ID)
	if err != nil {
		return err
	}
	// A cancellation request can race with an engine checkpoint. It is sticky
	// and must survive a save from the engine's older in-memory copy.
	if current.CancelRequested {
		run.CancelRequested = true
	}
	if current.Checkpoint > run.Checkpoint {
		run.Checkpoint = current.Checkpoint
	}
	run.Checkpoint++
	run.UpdatedAt = time.Now().UTC()
	return writeAtomic(s.path(run.ID), run)
}

func (s *JSONRunStore) Status(ctx context.Context, id string) (RunStatus, error) {
	run, err := s.Load(ctx, id)
	if err != nil {
		return "", err
	}
	return run.Status, nil
}

func (s *JSONRunStore) Cancel(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validRunID.MatchString(id) {
		return ErrRunNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.loadUnlocked(id)
	if err != nil {
		return err
	}
	if run.Status == StatusCompleted || run.Status == StatusCanceled {
		return nil
	}
	run.CancelRequested = true
	run.Checkpoint++
	run.UpdatedAt = time.Now().UTC()
	return writeAtomic(s.path(id), run)
}

// Cleanup removes terminal checkpoints older than before. Active/paused runs
// and promotions that have not been rolled back are retained regardless of age.
func (s *JSONRunStore) Cleanup(ctx context.Context, before time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validRunID.MatchString(id) {
			continue
		}
		run, err := s.loadUnlocked(id)
		if err != nil || !terminalStatus(run.Status) {
			continue
		}
		if run.Promotion != nil && !run.Promotion.PromotedAt.IsZero() && !run.Promotion.RolledBack {
			continue
		}
		updated := run.UpdatedAt
		if updated.IsZero() {
			if info, infoErr := entry.Info(); infoErr == nil {
				updated = info.ModTime()
			}
		}
		if updated.IsZero() || !updated.Before(before) {
			continue
		}
		if err := os.Remove(s.path(id)); err != nil {
			return removed, err
		}
		removed = append(removed, id)
	}
	sort.Strings(removed)
	return removed, nil
}

func (s *JSONRunStore) loadUnlocked(id string) (*Run, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read skillopt checkpoint: %w", err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("decode skillopt checkpoint: %w", err)
	}
	if run.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported skillopt schema version %d", run.SchemaVersion)
	}
	return &run, nil
}

func (s *JSONRunStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func writeAtomic(path string, value any) (retErr error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode skillopt checkpoint: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skillopt-*.tmp")
	if err != nil {
		return fmt.Errorf("create skillopt checkpoint: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := tmp.Close(); retErr == nil && closeErr != nil {
				retErr = closeErr
			}
		}
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure skillopt checkpoint: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write skillopt checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync skillopt checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close skillopt checkpoint: %w", err)
	}
	closed = true
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace skillopt checkpoint: %w", err)
	}
	return nil
}
