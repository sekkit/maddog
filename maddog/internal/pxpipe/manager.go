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
	"syscall"
	"time"

	"maddog/internal/config"
	"maddog/internal/event"
)

const (
	DefaultHost   = "127.0.0.1"
	DefaultPort   = 47821
	DefaultModels = "claude-fable-5,gpt-5.6"

	StateNotInstalled    = "not-installed"
	StateNotRunning      = "not-running"
	StateRunningManaged  = "running-managed"
	StateRunningExternal = "running-unmanaged"
	StateUnhealthy       = "unhealthy"
)

// Manager owns discovery, health, and lifecycle for the local pxpipe process.
type Manager struct {
	HTTPClient     *http.Client
	LookPath       func(string) (string, error)
	CommandContext func(context.Context, string, ...string) *exec.Cmd
	StatePath      string
}

// StartOptions describes a managed pxpipe launch.
type StartOptions struct {
	Host              string
	Port              int
	LogPath           string
	Models            string
	AnthropicUpstream string
	OpenAIUpstream    string
	Config            *config.Config
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
	st := Status{
		Host:         opt.Host,
		Port:         opt.Port,
		DashboardURL: dashboardURL(opt.Host, opt.Port),
		LogPath:      opt.LogPath,
		StatePath:    m.statePath(),
	}
	if binary, command, err := m.resolveCommand(); err == nil {
		st.Installed = true
		st.Binary = binary
		st.Command = command
	}
	sf, _ := m.readState()
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
	opt = normalizeOptions(opt)
	if st := m.Status(ctx, opt); st.Healthy {
		return st, nil
	}
	binary, command, err := m.resolveCommand()
	if err != nil {
		st := m.Status(ctx, opt)
		st.State = StateNotInstalled
		st.Error = "pxpipe not found on PATH; install pxpipe-proxy or Node/npx"
		return st, errors.New(st.Error)
	}
	env := BuildEnv(opt)
	if err := os.MkdirAll(filepath.Dir(opt.LogPath), 0o755); err != nil {
		return Status{}, err
	}
	logFile, err := os.OpenFile(opt.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()

	cmd := m.commandContext(ctx, command[0], command[1:]...)
	cmd.Env = mergeEnv(os.Environ(), env)
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
	if err := m.writeState(sf); err != nil {
		_ = cmd.Process.Kill()
		return Status{}, err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if healthy, _ := m.health(ctx, opt.Host, opt.Port); healthy {
			st := m.Status(ctx, opt)
			st.Binary = binary
			st.Command = command
			st.Env = env
			return st, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	st := m.Status(ctx, opt)
	st.State = StateUnhealthy
	st.PID = cmd.Process.Pid
	st.Binary = binary
	st.Command = command
	st.Env = env
	st.Error = "pxpipe process started but dashboard did not become healthy"
	return st, nil
}

// Stop terminates a managed pxpipe process recorded in the state file.
func (m *Manager) Stop(ctx context.Context, opt StartOptions) (Status, error) {
	opt = normalizeOptions(opt)
	sf, err := m.readState()
	if err != nil || sf.PID == 0 {
		return m.Status(ctx, opt), nil
	}
	proc, err := os.FindProcess(sf.PID)
	if err == nil {
		_ = proc.Kill()
	}
	_ = os.Remove(m.statePath())
	st := m.Status(ctx, opt)
	st.PID = sf.PID
	return st, nil
}

// BuildEnv returns the exact non-secret environment variables for a managed
// pxpipe process.
func BuildEnv(opt StartOptions) map[string]string {
	opt = normalizeOptions(opt)
	anthropic, openai := opt.AnthropicUpstream, opt.OpenAIUpstream
	if opt.Config != nil {
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
		opt.LogPath = filepath.Join(defaultDataDir(), "pxpipe", "events.jsonl")
	}
	return opt
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

func (m *Manager) resolveCommand() (binary string, command []string, err error) {
	lookPath := m.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if p, err := lookPath("pxpipe"); err == nil {
		return p, []string{p}, nil
	}
	if p, err := lookPath("npx"); err == nil {
		return p, []string{p, "pxpipe-proxy"}, nil
	}
	return "", nil, exec.ErrNotFound
}

func (m *Manager) readState() (stateFile, error) {
	var sf stateFile
	b, err := os.ReadFile(m.statePath())
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
	path := m.statePath()
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
	return filepath.Join(defaultDataDir(), "pxpipe", "state.json")
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
