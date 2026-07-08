package pxpipe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"maddog/internal/config"
)

func TestBuildEnvDefaultsLoopbackAndDerivedUpstreams(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = append([]config.ProviderEntry{
		{Name: "claude-upstream", Kind: "anthropic", BaseURL: "https://anthropic.example"},
	}, cfg.Providers...)

	env := BuildEnv(StartOptions{
		Config:  cfg,
		LogPath: "/tmp/pxpipe-events.jsonl",
	})

	if env["HOST"] != DefaultHost {
		t.Fatalf("HOST = %q, want %q", env["HOST"], DefaultHost)
	}
	if env["PORT"] != strconv.Itoa(DefaultPort) {
		t.Fatalf("PORT = %q, want %d", env["PORT"], DefaultPort)
	}
	if env["PXPIPE_MODELS"] != DefaultModels {
		t.Fatalf("PXPIPE_MODELS = %q, want %q", env["PXPIPE_MODELS"], DefaultModels)
	}
	if env["ANTHROPIC_UPSTREAM"] != "https://anthropic.example" {
		t.Fatalf("ANTHROPIC_UPSTREAM = %q", env["ANTHROPIC_UPSTREAM"])
	}
	if env["OPENAI_UPSTREAM"] != "https://api.deepseek.com" {
		t.Fatalf("OPENAI_UPSTREAM = %q", env["OPENAI_UPSTREAM"])
	}
	if _, ok := env["OPENAI_API_KEY"]; ok {
		t.Fatal("BuildEnv must not inject API keys")
	}
}

func TestBuildEnvExplicitUpstreamsWin(t *testing.T) {
	env := BuildEnv(StartOptions{
		Host:              "127.0.0.1",
		Port:              49152,
		LogPath:           "/tmp/pxpipe.jsonl",
		Models:            "off",
		Config:            config.Default(),
		AnthropicUpstream: "https://anthropic.override",
		OpenAIUpstream:    "https://openai.override/v1",
	})
	if env["ANTHROPIC_UPSTREAM"] != "https://anthropic.override" {
		t.Fatalf("ANTHROPIC_UPSTREAM = %q", env["ANTHROPIC_UPSTREAM"])
	}
	if env["OPENAI_UPSTREAM"] != "https://openai.override/v1" {
		t.Fatalf("OPENAI_UPSTREAM = %q", env["OPENAI_UPSTREAM"])
	}
	if env["PXPIPE_MODELS"] != "off" {
		t.Fatalf("PXPIPE_MODELS = %q", env["PXPIPE_MODELS"])
	}
}

func TestStatusDetectsRunningUnmanagedDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pxpipe"))
	}))
	defer srv.Close()
	host, port := splitServerURL(t, srv.URL)

	m := &Manager{
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	}
	st := m.Status(context.Background(), StartOptions{Host: host, Port: port})
	if st.State != StateRunningExternal || !st.Healthy || st.Managed {
		t.Fatalf("status = %+v, want running unmanaged healthy", st)
	}
}

func TestStatusDetectsRunningManagedDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pxpipe"))
	}))
	defer srv.Close()
	host, port := splitServerURL(t, srv.URL)
	m := &Manager{
		StatePath: t.TempDir() + "/state.json",
		LookPath:  func(string) (string, error) { return "/bin/pxpipe", nil },
	}
	if err := m.writeState(stateFile{PID: os.Getpid(), Host: host, Port: port, LogPath: "/tmp/pxpipe.jsonl", Models: "claude-fable-5"}); err != nil {
		t.Fatal(err)
	}

	st := m.Status(context.Background(), StartOptions{Host: host, Port: port})
	if st.State != StateRunningManaged || !st.Managed || st.PID != os.Getpid() || st.Models != "claude-fable-5" {
		t.Fatalf("status = %+v, want running managed", st)
	}
}

func TestStatusReportsUnhealthyWhenPortIsBoundByNonHTTPProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("not http\r\n"))
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	m := &Manager{
		LookPath: func(string) (string, error) { return "/bin/pxpipe", nil },
	}

	st := m.Status(context.Background(), StartOptions{Host: "127.0.0.1", Port: port})
	if st.State != StateUnhealthy || !strings.Contains(st.Error, "port") || !strings.Contains(st.Error, "bound") {
		t.Fatalf("status = %+v, want unhealthy bound-port error", st)
	}
}

func TestStatusReportsNotInstalledWhenMissingAndNotRunning(t *testing.T) {
	m := &Manager{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})},
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	}
	st := m.Status(context.Background(), StartOptions{})
	if st.State != StateNotInstalled || st.Installed || st.Healthy {
		t.Fatalf("status = %+v, want not installed", st)
	}
}

func TestStatusIncludesSafeEventSummary(t *testing.T) {
	logPath := t.TempDir() + "/events.jsonl"
	if err := os.WriteFile(logPath, []byte(`{"path":"/v1/messages","status":200,"compressed":true,"image_count":2,"baseline_tokens":900,"baseline_probe_status":"ok","req_body_sample_b64":"secret prompt sk-live"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})},
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	}

	st := m.Status(context.Background(), StartOptions{LogPath: logPath})
	if st.EventSummary == nil || st.EventSummary.Compressed != 1 || st.EventSummary.Images != 2 {
		t.Fatalf("event summary = %+v, want compressed/images aggregate", st.EventSummary)
	}
	rendered, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "secret prompt") || strings.Contains(string(rendered), "sk-live") {
		t.Fatalf("status leaked pxpipe raw body sample: %s", rendered)
	}
}

func splitServerURL(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return strings.Trim(u.Hostname(), "[]"), port
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
