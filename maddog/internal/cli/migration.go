package cli

import (
	"os"

	"maddog/internal/config"
)

func migrateLegacyConfigForCLI() {
	// Intentionally a no-op: Maddog no longer imports the legacy DeepSeek Maddog
	// config at CLI startup. Workspace-scoped MCP backfill happens only after the
	// command has established its current working directory.
}

func migrateMCPConfigForCLIWorkspace() {
	projectPath := config.ProjectConfigFilename
	if _, err := os.Stat(projectPath); err != nil {
		return
	}
	projectCfg := config.LoadForEdit(projectPath)
	if projectCfg == nil || len(projectCfg.Plugins) == 0 {
		return
	}
	userPath := config.UserConfigPath()
	if userPath == "" {
		return
	}
	userCfg := config.LoadForEdit(userPath)
	changed := false
	for _, plugin := range projectCfg.Plugins {
		if err := userCfg.UpsertPlugin(plugin); err == nil {
			changed = true
		}
	}
	if changed {
		_ = userCfg.SaveTo(userPath)
	}
}
