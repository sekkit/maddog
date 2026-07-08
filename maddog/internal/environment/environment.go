package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"maddog/internal/config"
)

const (
	ToolStatusOK       = "ok"
	ToolStatusMissing  = "missing"
	ToolStatusMismatch = "mismatch"

	ToolSourceOverride  = "override"
	ToolSourceRegistry  = "registry"
	ToolSourceGoBin     = "gobin"
	ToolSourceGoPathBin = "gopath-bin"
	ToolSourcePath      = "path"
)

type Registry struct {
	Version     int                   `json:"version"`
	ProjectRoot string                `json:"project_root,omitempty"`
	Host        HostSnapshot          `json:"host"`
	Tools       map[string]ToolRecord `json:"tools,omitempty"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type HostSnapshot struct {
	OS        string `json:"os,omitempty"`
	Arch      string `json:"arch,omitempty"`
	Home      string `json:"home,omitempty"`
	PathHash  string `json:"path_hash,omitempty"`
	GoBin     string `json:"go_bin,omitempty"`
	GoPathBin string `json:"gopath_bin,omitempty"`
}

type ToolRecord struct {
	Name       string          `json:"name"`
	Selected   string          `json:"selected,omitempty"`
	Version    string          `json:"version,omitempty"`
	Expected   string          `json:"expected,omitempty"`
	Source     string          `json:"source,omitempty"`
	Status     string          `json:"status,omitempty"`
	CheckedAt  time.Time       `json:"checked_at,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
	Candidates []ToolCandidate `json:"candidates,omitempty"`
}

type ToolCandidate struct {
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type ToolResult struct {
	Record       ToolRecord `json:"record"`
	RegistryPath string     `json:"registry_path,omitempty"`
}

type toolSpec struct {
	Name            string
	ExpectedVersion func(string) string
	Candidates      func(ToolRecord) []candidateSpec
	VersionArgs     []string
	VersionParser   func(string) string
}

type candidateSpec struct {
	Path   string
	Source string
}

func Load(projectRoot string) (*Registry, error) {
	root := normalizeRoot(projectRoot)
	path := config.ProjectEnvironmentRegistryPath(root)
	if path == "" {
		return defaultRegistry(root), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultRegistry(root), nil
		}
		return nil, err
	}
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, err
	}
	if reg.Tools == nil {
		reg.Tools = map[string]ToolRecord{}
	}
	if reg.ProjectRoot == "" {
		reg.ProjectRoot = root
	}
	return &reg, nil
}

func Refresh(projectRoot string) (*Registry, error) {
	root := normalizeRoot(projectRoot)
	reg, err := Load(root)
	if err != nil {
		return nil, err
	}
	reg.Version = 1
	reg.ProjectRoot = root
	reg.Host = collectHostSnapshot()
	if reg.Tools == nil {
		reg.Tools = map[string]ToolRecord{}
	}
	for _, spec := range supportedTools(root) {
		reg.Tools[spec.Name] = resolveToolSpec(spec, reg.Tools[spec.Name])
	}
	reg.UpdatedAt = time.Now().UTC()
	if err := save(root, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

func ResolveTool(projectRoot, name string) (ToolResult, error) {
	root := normalizeRoot(projectRoot)
	reg, err := Refresh(root)
	if err != nil {
		return ToolResult{}, err
	}
	key := strings.ToLower(strings.TrimSpace(name))
	rec, ok := reg.Tools[key]
	if !ok {
		return ToolResult{}, fmt.Errorf("environment: tool %q is not supported", name)
	}
	return ToolResult{Record: rec, RegistryPath: config.ProjectEnvironmentRegistryPath(root)}, nil
}

func ListTools(projectRoot string) ([]ToolResult, error) {
	root := normalizeRoot(projectRoot)
	reg, err := Refresh(root)
	if err != nil {
		return nil, err
	}
	out := make([]ToolResult, 0, len(reg.Tools))
	for _, spec := range supportedTools(root) {
		out = append(out, ToolResult{Record: reg.Tools[spec.Name], RegistryPath: config.ProjectEnvironmentRegistryPath(root)})
	}
	return out, nil
}

func defaultRegistry(projectRoot string) *Registry {
	return &Registry{Version: 1, ProjectRoot: normalizeRoot(projectRoot), Tools: map[string]ToolRecord{}}
}

func save(projectRoot string, reg *Registry) error {
	path := config.ProjectEnvironmentRegistryPath(projectRoot)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func supportedTools(projectRoot string) []toolSpec {
	return []toolSpec{
		{Name: "go", ExpectedVersion: func(string) string { return "" }, Candidates: goCandidates, VersionArgs: []string{"version"}, VersionParser: parseGoVersion},
		{Name: "pxpipe", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("pxpipe"), VersionArgs: []string{"--version"}, VersionParser: parseFirstLine},
		{Name: "npx", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("npx"), VersionArgs: []string{"--version"}, VersionParser: parseSimpleVersion},
		{Name: "pnpm", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("pnpm"), VersionArgs: []string{"--version"}, VersionParser: parseSimpleVersion},
		{Name: "wails", ExpectedVersion: expectedWailsVersion, Candidates: wailsCandidates, VersionArgs: []string{"version"}, VersionParser: parseWailsVersion},
		{Name: "create-dmg", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("create-dmg"), VersionArgs: []string{"--version"}, VersionParser: parseFirstLine},
		{Name: "nfpm", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("nfpm"), VersionArgs: []string{"version"}, VersionParser: parseSimpleVersion},
		{Name: "makensis", ExpectedVersion: func(string) string { return "" }, Candidates: genericPathCandidates("makensis"), VersionArgs: []string{"-VERSION"}, VersionParser: parseSimpleVersion},
	}
}

func resolveToolSpec(spec toolSpec, prior ToolRecord) ToolRecord {
	rec := ToolRecord{Name: spec.Name, Expected: spec.ExpectedVersion("."), CheckedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for _, candidate := range spec.Candidates(prior) {
		candidate.Path = strings.TrimSpace(candidate.Path)
		if candidate.Path == "" || seen[candidate.Path] {
			continue
		}
		seen[candidate.Path] = true
		cand := inspectExecutable(candidate.Path, candidate.Source, spec.VersionArgs, spec.VersionParser)
		rec.Candidates = append(rec.Candidates, cand)
	}
	for _, cand := range rec.Candidates {
		if !cand.OK {
			continue
		}
		rec.Selected = cand.Path
		rec.Version = cand.Version
		rec.Source = cand.Source
		rec.Status = ToolStatusOK
		if rec.Expected != "" && !versionMatches(cand.Version, rec.Expected) {
			rec.Status = ToolStatusMismatch
			rec.LastError = fmt.Sprintf("resolved %s %s but expected %s", spec.Name, cand.Version, rec.Expected)
		}
		return rec
	}
	rec.Status = ToolStatusMissing
	if len(rec.Candidates) > 0 {
		rec.LastError = rec.Candidates[len(rec.Candidates)-1].Error
	} else {
		rec.LastError = spec.Name + " executable not found"
	}
	return rec
}

func goCandidates(prior ToolRecord) []candidateSpec {
	var out []candidateSpec
	if override := strings.TrimSpace(os.Getenv("MADDOG_GO_PATH")); override != "" {
		out = append(out, candidateSpec{Path: override, Source: ToolSourceOverride})
	}
	if strings.TrimSpace(prior.Selected) != "" {
		out = append(out, candidateSpec{Path: prior.Selected, Source: ToolSourceRegistry})
	}
	if p, err := exec.LookPath(exeName("go")); err == nil {
		out = append(out, candidateSpec{Path: p, Source: ToolSourcePath})
	}
	return out
}

func genericPathCandidates(name string) func(ToolRecord) []candidateSpec {
	envName := "MADDOG_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(name)) + "_PATH"
	return func(prior ToolRecord) []candidateSpec {
		var out []candidateSpec
		if override := strings.TrimSpace(os.Getenv(envName)); override != "" {
			out = append(out, candidateSpec{Path: override, Source: ToolSourceOverride})
		}
		if strings.TrimSpace(prior.Selected) != "" {
			out = append(out, candidateSpec{Path: prior.Selected, Source: ToolSourceRegistry})
		}
		if p, err := exec.LookPath(exeName(name)); err == nil {
			out = append(out, candidateSpec{Path: p, Source: ToolSourcePath})
		}
		return out
	}
}

func wailsCandidates(prior ToolRecord) []candidateSpec {
	var out []candidateSpec
	if override := strings.TrimSpace(os.Getenv("MADDOG_WAILS_PATH")); override != "" {
		out = append(out, candidateSpec{Path: override, Source: ToolSourceOverride})
	}
	if strings.TrimSpace(prior.Selected) != "" {
		out = append(out, candidateSpec{Path: prior.Selected, Source: ToolSourceRegistry})
	}
	if gobin := strings.TrimSpace(goEnv("GOBIN")); gobin != "" {
		out = append(out, candidateSpec{Path: filepath.Join(gobin, exeName("wails")), Source: ToolSourceGoBin})
	}
	if gopath := strings.TrimSpace(goEnv("GOPATH")); gopath != "" {
		out = append(out, candidateSpec{Path: filepath.Join(gopath, "bin", exeName("wails")), Source: ToolSourceGoPathBin})
	}
	if p, err := exec.LookPath(exeName("wails")); err == nil {
		out = append(out, candidateSpec{Path: p, Source: ToolSourcePath})
	}
	return out
}

func inspectExecutable(path, source string, versionArgs []string, parse func(string) string) ToolCandidate {
	cand := ToolCandidate{Path: path, Source: source}
	info, err := os.Stat(path)
	if err != nil {
		cand.Error = err.Error()
		return cand
	}
	if info.IsDir() {
		cand.Error = "path is a directory"
		return cand
	}
	cmd := exec.Command(path, versionArgs...)
	out, err := cmd.CombinedOutput()
	cand.Version = parse(string(out))
	if err != nil {
		cand.Error = strings.TrimSpace(string(out))
		if cand.Error == "" {
			cand.Error = err.Error()
		}
		return cand
	}
	cand.OK = true
	return cand
}

func expectedWailsVersion(projectRoot string) string {
	path := filepath.Join(normalizeRoot(projectRoot), "desktop", "go.mod")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "github.com/wailsapp/wails/v2 ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseWailsVersion(out string) string {
	for _, field := range strings.Fields(strings.TrimSpace(out)) {
		if strings.HasPrefix(field, "v") && len(field) > 1 {
			return field
		}
	}
	return strings.TrimSpace(out)
}

func parseGoVersion(out string) string {
	fields := strings.Fields(strings.TrimSpace(out))
	for _, field := range fields {
		if strings.HasPrefix(field, "go1.") {
			return field
		}
	}
	return strings.TrimSpace(out)
}

func parseSimpleVersion(out string) string {
	return strings.TrimSpace(parseFirstLine(out))
}

func parseFirstLine(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return out[:i]
	}
	return out
}

func versionMatches(actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	return actual != "" && expected != "" && actual == expected
}

func normalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

func collectHostSnapshot() HostSnapshot {
	home, _ := os.UserHomeDir()
	sum := sha256.Sum256([]byte(os.Getenv("PATH")))
	gobin := strings.TrimSpace(goEnv("GOBIN"))
	gopath := strings.TrimSpace(goEnv("GOPATH"))
	gopathBin := ""
	if gopath != "" {
		gopathBin = filepath.Join(gopath, "bin")
	}
	return HostSnapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, Home: home, PathHash: hex.EncodeToString(sum[:]), GoBin: gobin, GoPathBin: gopathBin}
}

func goEnv(name string) string {
	cmd := exec.Command("go", "env", name)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
