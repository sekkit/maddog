package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv loads KEY=value files into the process environment without
// importing project .env files. Project .env values are returned as a scoped
// expansion map for MCP/plugin fields; provider credentials come only from the
// Maddog-owned credentials file.
func loadDotEnv() {
	projectEnv := loadDotEnvForRoot(".")
	loadProjectEnvKeysThatShadowHome(".env", projectEnv)
}

// loadDotEnvForRoot reads a root's .env file into a scoped expansion map, then
// pins Maddog's global credentials file into the process environment.
func loadDotEnvForRoot(root string) map[string]string {
	dotEnvPath := ".env"
	if root != "" && root != "." {
		dotEnvPath = filepath.Join(root, ".env")
	}
	projectEnv := readDotEnvFileMap(dotEnvPath, allowProjectExpansionEnv)
	if current := UserCredentialsPath(); current != "" {
		loadDotEnvFileAs(current, CredentialSource{Kind: CredentialSourceCredentials, Path: current})
	}
	for _, path := range legacyCredentialsPaths() {
		loadDotEnvFileAs(path, CredentialSource{Kind: CredentialSourceLegacy, Path: path})
	}
	return projectEnv
}

func allowProjectExpansionEnv(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return !strings.HasPrefix(key, "MADDOG_")
}

func loadProjectEnvKeysThatShadowHome(projectPath string, projectEnv map[string]string) {
	if len(projectEnv) == 0 {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	homeEnv := readDotEnvFileMap(filepath.Join(home, ".env"), nil)
	for key, value := range projectEnv {
		if _, shadowsHome := homeEnv[key]; !shadowsHome {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			recordExistingCredentialSource(key)
			continue
		}
		if err := os.Setenv(key, value); err == nil {
			recordCredentialSource(key, value, CredentialSource{Kind: CredentialSourceProjectEnv, Path: projectPath})
		}
	}
}

func legacyCredentialsPaths() []string {
	current := UserCredentialsPath()
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if current != "" && samePath(path, current) {
			return
		}
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if dir := legacyOSSupportDir(); dir != "" {
		add(filepath.Join(dir, "credentials"))
	}
	if dir := userSupportDir(); dir != "" {
		add(filepath.Join(dir, "credentials"))
	}
	for _, cfg := range legacyXDGConfigPaths() {
		add(filepath.Join(filepath.Dir(cfg), "credentials"))
	}
	return paths
}

func loadDotEnvFileAs(path string, source CredentialSource) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists && source.Kind != CredentialSourceCredentials {
			recordExistingCredentialSource(key)
			continue
		}
		if err := os.Setenv(key, val); err == nil && source.Kind != "" {
			source.Path = path
			recordCredentialSource(key, val, source)
		}
	}
}

func readDotEnvFileMap(path string, allow func(string) bool) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || allow != nil && !allow(key) {
			continue
		}
		out[key] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envFileValue(path, wantKey string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != wantKey {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		return val, true
	}
	return "", false
}
