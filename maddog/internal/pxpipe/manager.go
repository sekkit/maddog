// Package pxpipe manages an optional local pxpipe sidecar.
package pxpipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"maddog/internal/config"
	"maddog/internal/event"
	"maddog/internal/secrets"
)

const (
	DefaultHost   = "127.0.0.1"
	DefaultPort   = 47821
	DefaultModels = "claude-fable-5,gpt-5.6"

	// NpxPackage is pinned so first-use installation is reproducible rather than
	// silently following whichever pxpipe release happens to be latest.
	NpxPackage            = "pxpipe-proxy@0.8.0"
	DefaultStartupTimeout = 30 * time.Second
	DefaultStartupPoll    = 100 * time.Millisecond

	StateNotInstalled    = "not-installed"
	StateNotRunning      = "not-running"
	StateRunningManaged  = "running-managed"
	StateRunningExternal = "running-unmanaged"
	StateUnhealthy       = "unhealthy"
)

// Manager owns discovery, health, and lifecycle for the local pxpipe process.
// lifecycleMu prevents concurrent UI/session builders from racing to spawn two
// sidecars for the same default loopback endpoint.
var lifecycleMu sync.Mutex

type Manager struct {
	HTTPClient          *http.Client
	LookPath            func(string) (string, error)
	CommandContext      func(context.Context, string, ...string) *exec.Cmd
	StatePath           string
	StartupTimeout      time.Duration
	StartupPollInterval time.Duration
}

// StartOptions describes a managed pxpipe launch.
type StartOptions struct {
	Host              string
	Port              int
	LogPath           string
	Models            string
	AnthropicUpstream string
	OpenAIUpstream    string
	Provider          string
	Config            *config.Config
	instanceMatched   bool
}

// Status is safe to print as JSON; it intentionally omits secrets and request
// content.
type Status struct {
	State             string               `json:"state"`
	Installed         bool                 `json:"installed"`
	Binary            string               `json:"binary,omitempty"`
	Command           []string             `json:"command,omitempty"`
	Host              string               `json:"host"`
	Port              int                  `json:"port"`
	DashboardURL      string               `json:"dashboard_url"`
	LogPath           string               `json:"log_path,omitempty"`
	StatePath         string               `json:"state_path,omitempty"`
	PID               int                  `json:"pid,omitempty"`
	Managed           bool                 `json:"managed"`
	Healthy           bool                 `json:"healthy"`
	Models            string               `json:"models,omitempty"`
	AnthropicUpstream string               `json:"anthropic_upstream,omitempty"`
	OpenAIUpstream    string               `json:"openai_upstream,omitempty"`
	Error             string               `json:"error,omitempty"`
	Env               map[string]string    `json:"env,omitempty"`
	EventSummary      *event.PxpipeSummary `json:"event_summary,omitempty"`
}

type stateFile struct {
	PID               int       `json:"pid"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	LogPath           string    `json:"log_path"`
	Models            string    `json:"models"`
	AnthropicUpstream string    `json:"anthropic_upstream,omitempty"`
	OpenAIUpstream    string    `json:"openai_upstream,omitempty"`
	StartedAt         time.Time `json:"started_at"`
}

// NewManager returns a manager using the real OS process and PATH.
func NewManager() *Manager {
	return &Manager{}
}

// Status checks installation and loopback health without starting anything.
func (m *Manager) Status(ctx context.Context, opt StartOptions) Status {
	opt = normalizeOptions(opt)
	statePath := m.statePathFor(opt)
	st := Status{
		Host:         opt.Host,
		Port:         opt.Port,
		DashboardURL: dashboardURL(opt.Host, opt.Port),
		LogPath:      opt.LogPath,
		StatePath:    statePath,
	}
	if binary, command, err := m.resolveCommand(autoInstallEnabled(opt)); err == nil {
		st.Installed = true
		st.Binary = binary
		st.Command = command
	}
	sf, _ := m.readStateAt(statePath)
	healthy, err := m.health(ctx, opt.Host, opt.Port)
	st.Healthy = healthy
	if healthy {
		if sf.PID > 0 && sf.Port == opt.Port && sf.Host == opt.Host {
			st.State = StateRunningManaged
			st.Managed = true
			st.PID = sf.PID
			st.LogPath = valueOr(st.LogPath, sf.LogPath)
			st.Models = sf.Models
			st.AnthropicUpstream = sf.AnthropicUpstream
			st.OpenAIUpstream = sf.OpenAIUpstream
		} else {
			st.State = StateRunningExternal
		}
		attachEventSummary(&st)
		return st
	}
	if st.Installed && portOpen(ctx, opt.Host, opt.Port) {
		st.State = StateUnhealthy
		if err != nil {
			st.Error = fmt.Sprintf("port %d is bound but pxpipe dashboard is not healthy: %v", opt.Port, err)
		} else {
			st.Error = fmt.Sprintf("port %d is bound but pxpipe dashboard is not healthy", opt.Port)
		}
		attachEventSummary(&st)
		return st
	}
	if err != nil {
		st.Error = err.Error()
	}
	if st.Installed {
		st.State = StateNotRunning
	} else {
		st.State = StateNotInstalled
	}
	attachEventSummary(&st)
	return st
}

// Start launches pxpipe unless a server is already responding on the configured
// loopback address.
func (m *Manager) Start(ctx context.Context, opt StartOptions) (Status, error) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	opt = normalizeOptions(opt)
	if st := m.Status(ctx, opt); st.Healthy {
		return st, nil
	}
	binary, command, err := m.resolveCommand(autoInstallEnabled(opt))
	if err != nil {
		st := m.Status(ctx, opt)
		st.State = StateNotInstalled
		st.Error = "pxpipe runtime is unavailable; install Node once or enable pxpipe.auto_install"
		return st, errors.New(st.Error)
	}
	env := BuildEnv(opt)
	if err := os.MkdirAll(filepath.Dir(opt.LogPath), 0o755); err != nil {
		return Status{}, err
	}
	// pxpipe owns PXPIPE_LOG as JSONL event storage. Keep launcher stdout/stderr
	// separate so npm progress and upstream error diagnostics cannot corrupt the
	// event stream or be surfaced as gateway telemetry.
	runnerLogPath := filepath.Join(filepath.Dir(opt.LogPath), "runner.log")
	logFile, err := os.OpenFile(runnerLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()

	cmd := m.commandContext(ctx, command[0], command[1:]...)
	cmd.Env = mergeEnv(secrets.ProcessEnv(), env)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return Status{}, err
	}
	sf := stateFile{
		PID:               cmd.Process.Pid,
		Host:              opt.Host,
		Port:              opt.Port,
		LogPath:           opt.LogPath,
		Models:            env["PXPIPE_MODELS"],
		AnthropicUpstream: env["ANTHROPIC_UPSTREAM"],
		OpenAIUpstream:    env["OPENAI_UPSTREAM"],
		StartedAt:         time.Now().UTC(),
	}
	if err := m.writeStateAt(m.statePathFor(opt), sf); err != nil {
		_ = terminateProcessTree(ctx, cmd.Process.Pid)
		return Status{}, err
	}
	deadline := time.Now().Add(m.startupTimeout())
	for time.Now().Before(deadline) {
		if healthy, _ := m.health(ctx, opt.Host, opt.Port); healthy {
			st := m.Status(ctx, opt)
			st.Binary = binary
			st.Command = command
			st.Env = env
			return st, nil
		}
		time.Sleep(m.startupPollInterval())
	}
	st := m.Status(ctx, opt)
	st.State = StateUnhealthy
	st.PID = cmd.Process.Pid
	st.Binary = binary
	st.Command = command
	st.Env = env
	st.Error = "pxpipe process started but dashboard did not become healthy before startup timeout"
	return st, errors.New(st.Error)
}

// Stop terminates a managed pxpipe process recorded in the state file.
func (m *Manager) Stop(ctx context.Context, opt StartOptions) (Status, error) {
	opt = normalizeOptions(opt)
	statePath := m.statePathFor(opt)
	sf, err := m.readStateAt(statePath)
	if err != nil || sf.PID == 0 {
		return m.Status(ctx, opt), nil
	}
	if err := terminateProcessTree(ctx, sf.PID); err != nil {
		st := m.Status(ctx, opt)
		st.PID = sf.PID
		st.Error = err.Error()
		return st, err
	}
	_ = os.Remove(statePath)
	st := m.Status(ctx, opt)
	st.PID = sf.PID
	return st, nil
}

// BuildEnv returns the exact non-secret environment variables for a managed
// pxpipe process.
func BuildEnv(opt StartOptions) map[string]string {
	opt = normalizeOptions(opt)
	anthropic, openai := opt.AnthropicUpstream, opt.OpenAIUpstream
	if opt.Config != nil && !opt.instanceMatched {
		derivedAnthropic, derivedOpenAI := deriveUpstreams(opt.Config)
		if strings.TrimSpace(anthropic) == "" {
			anthropic = derivedAnthropic
		}
		if strings.TrimSpace(openai) == "" {
			openai = derivedOpenAI
		}
	}
	env := map[string]string{
		"HOST":          opt.Host,
		"PORT":          strconv.Itoa(opt.Port),
		"PXPIPE_LOG":    opt.LogPath,
		"PXPIPE_MODELS": opt.Models,
	}
	if strings.TrimSpace(anthropic) != "" {
		env["ANTHROPIC_UPSTREAM"] = strings.TrimSpace(anthropic)
	}
	if strings.TrimSpace(openai) != "" {
		env["OPENAI_UPSTREAM"] = strings.TrimSpace(openai)
	}
	return env
}

func normalizeOptions(opt StartOptions) StartOptions {
	if cfg := opt.Config; cfg != nil {
		if instance, ok := providerInstance(cfg.Pxpipe.Instances, opt.Provider); ok {
			opt.instanceMatched = true
			if strings.TrimSpace(opt.Host) == "" {
				opt.Host = instance.Host
			}
			if opt.Port == 0 {
				opt.Port = instance.Port
			}
			if strings.TrimSpace(opt.Models) == "" && len(instance.Models) > 0 {
				opt.Models = strings.Join(instance.Models, ",")
			}
			if strings.TrimSpace(opt.AnthropicUpstream) == "" {
				opt.AnthropicUpstream = instance.AnthropicUpstream
			}
			if strings.TrimSpace(opt.OpenAIUpstream) == "" {
				opt.OpenAIUpstream = instance.OpenAIUpstream
			}
		} else {
			if strings.TrimSpace(opt.Host) == "" {
				opt.Host = cfg.Pxpipe.Host
			}
			if opt.Port == 0 {
				opt.Port = cfg.Pxpipe.Port
			}
			if strings.TrimSpace(opt.Models) == "" && len(cfg.Pxpipe.Models) > 0 {
				opt.Models = strings.Join(cfg.Pxpipe.Models, ",")
			}
			if strings.TrimSpace(opt.AnthropicUpstream) == "" {
				opt.AnthropicUpstream = cfg.Pxpipe.AnthropicUpstream
			}
			if strings.TrimSpace(opt.OpenAIUpstream) == "" {
				opt.OpenAIUpstream = cfg.Pxpipe.OpenAIUpstream
			}
		}
	}
	if strings.TrimSpace(opt.Host) == "" {
		opt.Host = DefaultHost
	}
	if opt.Port == 0 {
		opt.Port = DefaultPort
	}
	if strings.TrimSpace(opt.Models) == "" {
		opt.Models = DefaultModels
	}
	if strings.TrimSpace(opt.LogPath) == "" {
		opt.LogPath = filepath.Join(defaultInstanceDir(opt.Port), "events.jsonl")
	}
	return opt
}

func providerInstance(instances []config.PxpipeInstanceConfig, providerName string) (config.PxpipeInstanceConfig, bool) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return config.PxpipeInstanceConfig{}, false
	}
	for _, instance := range instances {
		for _, candidate := range instance.Providers {
			if strings.EqualFold(strings.TrimSpace(candidate), providerName) {
				return instance, true
			}
		}
	}
	return config.PxpipeInstanceConfig{}, false
}

func autoInstallEnabled(opt StartOptions) bool {
	return opt.Config == nil || opt.Config.Pxpipe.AutoInstall
}

func (m *Manager) startupTimeout() time.Duration {
	if m.StartupTimeout > 0 {
		return m.StartupTimeout
	}
	return DefaultStartupTimeout
}

func (m *Manager) startupPollInterval() time.Duration {
	if m.StartupPollInterval > 0 {
		return m.StartupPollInterval
	}
	return DefaultStartupPoll
}

func attachEventSummary(st *Status) {
	if st == nil || strings.TrimSpace(st.LogPath) == "" {
		return
	}
	summary, err := ReadEventSummary(st.LogPath)
	if err != nil {
		return
	}
	if summary.Requests == 0 && summary.Malformed == 0 {
		return
	}
	st.EventSummary = &summary
}

func deriveUpstreams(cfg *config.Config) (anthropic, openai string) {
	for _, p := range cfg.Providers {
		if strings.HasPrefix(p.Name, "pxpipe-") || isLoopbackURL(p.BaseURL) || strings.TrimSpace(p.BaseURL) == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(p.Kind)) {
		case "anthropic":
			if anthropic == "" {
				anthropic = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
			}
		case "openai":
			if openai == "" {
				openai = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
			}
		}
	}
	return anthropic, openai
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func (m *Manager) health(ctx context.Context, host string, port int) (bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, dashboardURL(host, port), nil)
	if err != nil {
		return false, err
	}
	resp, err := m.httpClient().Do(req)
	if err != nil {
		return false, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return false, fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}
	return true, nil
}

func portOpen(ctx context.Context, host string, port int) bool {
	dialCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *Manager) resolveCommand(autoInstall bool) (binary string, command []string, err error) {
	lookPath := m.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if p, err := lookPath("pxpipe"); err == nil {
		return p, []string{p}, nil
	}
	if autoInstall {
		if p, err := lookPath("npx"); err == nil {
			// `--yes` makes first-use package provisioning non-interactive; the
			// versioned package makes the sidecar reproducible and testable.
			return p, []string{p, "--yes", NpxPackage}, nil
		}
	}
	return "", nil, exec.ErrNotFound
}

func (m *Manager) readState() (stateFile, error) {
	return m.readStateAt(m.statePath())
}

func (m *Manager) readStateAt(path string) (stateFile, error) {
	var sf stateFile
	b, err := os.ReadFile(path)
	if err != nil {
		return sf, err
	}
	if err := json.Unmarshal(b, &sf); err != nil {
		return stateFile{}, err
	}
	if sf.PID > 0 && !processAlive(sf.PID) {
		return stateFile{}, os.ErrProcessDone
	}
	return sf, nil
}

func (m *Manager) writeState(sf stateFile) error {
	return m.writeStateAt(m.statePath(), sf)
}

func (m *Manager) writeStateAt(path string, sf stateFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (m *Manager) statePath() string {
	if strings.TrimSpace(m.StatePath) != "" {
		return m.StatePath
	}
	return filepath.Join(defaultInstanceDir(DefaultPort), "state.json")
}

func (m *Manager) statePathFor(opt StartOptions) string {
	if strings.TrimSpace(m.StatePath) != "" {
		return m.StatePath
	}
	return filepath.Join(defaultInstanceDir(opt.Port), "state.json")
}

func (m *Manager) httpClient() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

func (m *Manager) commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	if m.CommandContext != nil {
		return m.CommandContext(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...)
}

func terminateProcessTree(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		// npx.cmd launches node.exe through cmd.exe. Killing only cmd.exe leaves
		// the proxy listening, so taskkill's /T is required for managed shutdown.
		cmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if out, err := cmd.CombinedOutput(); err != nil {
			message := strings.TrimSpace(string(out))
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("stop managed pxpipe process tree: %s", message)
		}
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("stop managed pxpipe process: %w", err)
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func defaultDataDir() string {
	if dir := config.MemoryUserDir(); strings.TrimSpace(dir) != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "maddog")
}

func defaultInstanceDir(port int) string {
	base := filepath.Join(defaultDataDir(), "pxpipe")
	if port == 0 || port == DefaultPort {
		return base
	}
	return filepath.Join(base, strconv.Itoa(port))
}

func dashboardURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/", host, port)
}

func mergeEnv(base []string, add map[string]string) []string {
	out := make([]string, 0, len(base)+len(add))
	seen := map[string]bool{}
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, replace := add[key]; replace {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	for k, v := range add {
		if seen[k] {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
