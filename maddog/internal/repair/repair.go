// Package repair contains the offline recovery boundary used by Maddog.
// It deliberately has no desktop, network, plugin, or MCP dependencies.
package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Capability names an operation controlled by a recovery policy.
type Capability string

const (
	// CapabilityWebview permits starting the desktop webview.
	CapabilityWebview Capability = "webview"
	// CapabilityPlugins permits loading plugins.
	CapabilityPlugins Capability = "plugins"
	// CapabilityMCP permits starting MCP integrations.
	CapabilityMCP Capability = "mcp"
	// CapabilityHooks permits running hooks.
	CapabilityHooks Capability = "hooks"
	// CapabilityBots permits starting bot integrations.
	CapabilityBots Capability = "bots"
	// CapabilitySessions permits opening user sessions.
	CapabilitySessions Capability = "sessions"
	// CapabilitySkills permits loading skills.
	CapabilitySkills Capability = "skills"
	// CapabilitySidecars permits starting sidecar processes.
	CapabilitySidecars Capability = "sidecars"
	// CapabilityMemoryLearning permits persistent memory learning.
	CapabilityMemoryLearning Capability = "memory-learning"
	// CapabilityModelUpgrades permits model upgrade operations.
	CapabilityModelUpgrades Capability = "model-upgrades"
	// CapabilityNetwork permits network access.
	CapabilityNetwork Capability = "network"
	// CapabilityBuiltinConfig permits reading built-in configuration.
	CapabilityBuiltinConfig Capability = "builtin-config"
	// CapabilityManualApproval permits actions explicitly approved by the user.
	CapabilityManualApproval Capability = "manual-approval"
	// CapabilitySandbox permits sandboxed recovery operations.
	CapabilitySandbox Capability = "sandbox"
)

// Policy defines the capabilities available to a process mode.
type Policy struct{ allowed map[Capability]bool }

// Allows reports whether the policy permits c.
func (p Policy) Allows(c Capability) bool { return p.allowed[c] }

// SafeModePolicy returns the minimal capability set allowed during recovery.
func SafeModePolicy() Policy {
	return Policy{allowed: map[Capability]bool{
		CapabilityBuiltinConfig: true, CapabilityManualApproval: true, CapabilitySandbox: true,
	}}
}

// NormalPolicy returns the complete capability set used outside Safe Mode.
func NormalPolicy() Policy {
	p := SafeModePolicy()
	for _, c := range []Capability{CapabilityWebview, CapabilityPlugins, CapabilityMCP, CapabilityHooks, CapabilityBots, CapabilitySessions, CapabilitySkills, CapabilitySidecars, CapabilityMemoryLearning, CapabilityModelUpgrades, CapabilityNetwork} {
		p.allowed[c] = true
	}
	return p
}

// ProcessGate keeps Safe Mode state in memory. A caller must create one per
// process; no environment or global mutable switch can leak between launches.
type ProcessGate struct {
	safe   bool
	policy Policy
}

// NewProcessGate creates a process-local capability gate for normal or Safe Mode.
func NewProcessGate(safe bool) *ProcessGate {
	if safe {
		return &ProcessGate{safe: true, policy: SafeModePolicy()}
	}
	return &ProcessGate{policy: NormalPolicy()}
}

// SafeMode reports whether the gate enforces the Safe Mode policy.
func (g *ProcessGate) SafeMode() bool { return g != nil && g.safe }

// Allows reports whether the gate permits c.
func (g *ProcessGate) Allows(c Capability) bool { return g != nil && g.policy.Allows(c) }

// Repairer performs serialized repairs within a Maddog-owned root.
type Repairer struct {
	root, backupRoot string
	mu               sync.Mutex
}

// Receipt records enough information to roll back a completed replacement.
type Receipt struct {
	Target, Backup string
	Existed        bool
	Digest         string
	CreatedAt      time.Time
}

// NewRepairer creates a repair boundary rooted at root.
func NewRepairer(root string) *Repairer {
	return &Repairer{root: root, backupRoot: filepath.Join(root, ".maddog", "repair-backups")}
}

func (r *Repairer) owned(rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("repair path is not Maddog-owned")
	}
	root, err := filepath.Abs(r.root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, target)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repair path is not Maddog-owned")
	}
	// Resolve every existing component. This catches Unix symlinks and Windows
	// junctions/reparse points before a missing leaf can be created through them.
	cur := root
	for _, part := range strings.Split(relCheck, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if _, err := os.Lstat(cur); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		real, err := filepath.EvalSymlinks(cur)
		if err != nil {
			return "", err
		}
		realRel, err := filepath.Rel(root, real)
		if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) || filepath.Clean(real) != filepath.Clean(cur) {
			return "", fmt.Errorf("repair path is not Maddog-owned")
		}
	}
	return target, nil
}

// Replace atomically replaces a Maddog-owned relative path and returns a rollback receipt.
func (r *Repairer) Replace(rel string, data []byte) (Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, err := r.owned(rel)
	if err != nil {
		return Receipt{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Receipt{}, err
	}
	if err := os.MkdirAll(r.backupRoot, 0o700); err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{Target: target, CreatedAt: time.Now().UTC(), Digest: SHA256(data)}
	if old, err := os.ReadFile(target); err == nil {
		receipt.Existed = true
		receipt.Backup = filepath.Join(r.backupRoot, fmt.Sprintf("%d-%x.bak", receipt.CreatedAt.UnixNano(), sha256.Sum256([]byte(target))))
		if err := os.WriteFile(receipt.Backup, old, 0o600); err != nil {
			return Receipt{}, err
		}
	} else if !os.IsNotExist(err) {
		return Receipt{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".repair-*")
	if err != nil {
		return Receipt{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Receipt{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// Rollback restores the target described by receipt to its pre-repair state.
func (r *Repairer) Rollback(receipt Receipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if receipt.Target == "" {
		return fmt.Errorf("repair receipt is empty")
	}
	root, err := filepath.Abs(r.root)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(receipt.Target)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("repair path is not Maddog-owned")
	}
	if !receipt.Existed {
		err := os.Remove(target)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if receipt.Backup == "" {
		return fmt.Errorf("repair receipt has no backup")
	}
	backupRoot, err := filepath.Abs(r.backupRoot)
	if err != nil {
		return err
	}
	backup, err := filepath.Abs(receipt.Backup)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(backupRoot, backup); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("repair backup is not Maddog-owned")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.Rename(backup, target)
}

// SHA256 returns the lowercase hexadecimal SHA-256 digest of data.
func SHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
