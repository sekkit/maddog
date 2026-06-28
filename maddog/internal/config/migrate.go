package config

import (
	"fmt"
	"strings"
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

// MigrateLegacyIfNeeded used to import old Maddog config into the current user
// config. Maddog now intentionally keeps its configuration isolated from an
// installed DeepSeek Maddog, so startup must not read or copy Maddog-owned
// files. Kept as a no-op for callers compiled against the old migration hook.
func MigrateLegacyIfNeeded() (*MigrationResult, error) {
	if userConfigPath() == "" {
		return nil, nil
	}
	return nil, nil
}
