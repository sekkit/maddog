package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandDirsIncludeConventions verifies command discovery covers the
// cross-tool convention dirs (so .claude/commands etc. migrate in) and that the
// canonical .maddog project dir is highest priority (last, since command.Load
// lets a later dir win on a name clash).
func TestCommandDirsIncludeConventions(t *testing.T) {
	dirs := CommandDirs()
	joined := strings.Join(dirs, "\n")
	for _, want := range []string{
		filepath.Join(".claude", "commands"),
		filepath.Join(".agents", "commands"),
		filepath.Join(".agent", "commands"),
		filepath.Join(".maddog", "commands"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("CommandDirs missing %q\ngot:\n%s", want, joined)
		}
	}
	// The project's .maddog/commands must be the highest-priority (last) entry.
	if last := dirs[len(dirs)-1]; last != filepath.Join(".maddog", "commands") {
		t.Errorf("project .maddog/commands should be highest priority (last), got %q", last)
	}
	if strings.Contains(joined, filepath.Join(".reasonix", "commands")) {
		t.Errorf("CommandDirs should not include original Reasonix command dirs:\n%s", joined)
	}
}
