package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv loads KEY=value files into the process environment without
// overriding variables that are already set (first file to set a key wins).
// Order: a project ./.env (read-only back-compat, so a manual project override
// takes precedence), then the Maddog-owned global credentials file in the user
// config dir (where setup writes keys, so they resolve from any directory without
// ever touching a project's own .env). Existing environment variables always win.
func loadDotEnv() {
	loadDotEnvForRoot(".")
}

// loadDotEnvForRoot loads a root's .env file (if present) before the Maddog
// credentials file. When root is "." it behaves like loadDotEnv().
func loadDotEnvForRoot(root string) {
	dotEnvPath := ".env"
	if root != "" && root != "." {
		dotEnvPath = filepath.Join(root, ".env")
	}
	loadDotEnvFile(dotEnvPath)
	if p := UserCredentialsPath(); p != "" {
		loadDotEnvFile(p)
	}
}

// loadDotEnvFile reads one .env file (if present) and sets any keys not already
// present in the environment. Lenient, zero-dependency parsing.
func loadDotEnvFile(path string) {
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
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
