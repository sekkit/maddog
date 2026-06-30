package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// MigrationResult summarizes a legacy import for callers compiled against the
// previous migration API. MigrateLegacyIfNeeded now returns nil.
type MigrationResult struct {
	From     string
	To       string
	KeyToEnv bool
	Plugins  int
	Warnings []string
}

// MCPGlobalMigrationResult summarizes the v1.9.1 MCP backfill that lifts MCP
// servers from legacy and project-local sources into the user-global config.
type MCPGlobalMigrationResult struct {
	To      string
	Added   int
	Sources int
}

func (r *MigrationResult) Notice() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrated your previous configuration: %s -> %s", r.From, r.To)
	if r.Plugins > 0 {
		fmt.Fprintf(&b, " (%d MCP server(s))", r.Plugins)
	}
	if r.KeyToEnv {
		b.WriteString("; API key saved to Maddog's credentials store")
	}
	b.WriteString(". The old files were left untouched.")
	for _, w := range r.Warnings {
		b.WriteString("\n  note: " + w)
	}
	return b.String()
}

// MigrateLegacyIfNeeded used to import old Reasonix config into the current user
// config. Maddog now intentionally keeps its configuration isolated from an
// installed DeepSeek Reasonix, so startup must not read or copy Reasonix-owned
// files. Kept as a no-op for callers compiled against the old migration hook.
func MigrateLegacyIfNeeded() (*MigrationResult, error) {
	if userConfigPath() == "" {
		return nil, nil
	}
	return nil, nil
}
