// Package secrets owns process-wide child environment isolation.
package secrets

import (
	"os"
	"runtime"
	"strings"
	"sync"
)

var credentialEnvKeys = struct {
	sync.RWMutex
	keys map[string]struct{}
}{keys: map[string]struct{}{}}

// RegisterCredentialEnvKeys adds credential names to the process-lifetime
// denylist. A union is required because concurrent workspaces may use different
// providers while sharing one Maddog process.
func RegisterCredentialEnvKeys(keys []string) {
	credentialEnvKeys.Lock()
	defer credentialEnvKeys.Unlock()
	for _, key := range keys {
		if key = credentialEnvKey(key); key != "" {
			credentialEnvKeys.keys[key] = struct{}{}
		}
	}
}

func credentialEnvKey(key string) string {
	key = strings.TrimSpace(key)
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func registeredCredentialEnvKey(key string) bool {
	credentialEnvKeys.RLock()
	defer credentialEnvKeys.RUnlock()
	_, ok := credentialEnvKeys.keys[credentialEnvKey(key)]
	return ok
}

// ProcessEnv returns a child-process environment without values loaded from
// Maddog's credential store. It never mutates os.Environ's backing storage.
func ProcessEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || registeredCredentialEnvKey(key) {
			continue
		}
		out = append(out, entry)
	}
	return out
}
