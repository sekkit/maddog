package config

import "path/filepath"

func legacyConfigPath() string {
	if home := MaddogHomeDir(); home != "" {
		return filepath.Join(home, "config.json")
	}
	return ""
}

func loadLegacyMCP(path string) []PluginEntry {
	return nil
}
