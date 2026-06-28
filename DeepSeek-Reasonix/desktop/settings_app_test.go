package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

func TestWithFreshSystemPromptReplacesExistingSystemMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "old", ReasoningContent: "stale", ReasoningSignature: "sig", ToolCalls: []provider.ToolCall{{ID: "call", Name: "noop"}}, ToolCallID: "tool", Name: "name"},
		{Role: provider.RoleUser, Content: "hello"},
	}

	got := withFreshSystemPrompt(msgs, "new")
	if got[0].Content != "new" {
		t.Fatalf("system prompt = %q, want new", got[0].Content)
	}
	if got[0].ReasoningContent != "" || got[0].ReasoningSignature != "" || len(got[0].ToolCalls) != 0 || got[0].ToolCallID != "" || got[0].Name != "" {
		t.Fatalf("system metadata should be cleared, got %+v", got[0])
	}
	if got[1].Content != "hello" {
		t.Fatalf("non-system message changed: %+v", got[1])
	}
	if msgs[0].Content != "old" {
		t.Fatalf("input slice was mutated: %+v", msgs[0])
	}
}

func TestWithFreshSystemPromptPrependsMissingSystemMessage(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hello"}}

	got := withFreshSystemPrompt(msgs, "new")
	if len(got) != 2 || got[0].Role != provider.RoleSystem || got[0].Content != "new" {
		t.Fatalf("expected prepended system prompt, got %+v", got)
	}
	if got[1].Content != "hello" {
		t.Fatalf("existing user message changed: %+v", got[1])
	}
}

func TestProviderViewFromEntry_FiltersNonChatModels(t *testing.T) {
	p := config.ProviderEntry{
		Name: "mimo-api",
		Models: []string{
			"mimo-v2", "mimo-v2-pro",
			"mimo-v2-asr", "mimo-v2-tts",
			"mimo-v2-tts-voiceclone", "mimo-v2-tts-voicedesign",
		},
	}
	view := providerViewFromEntry(p, true, false, nil)
	want := []string{"mimo-v2", "mimo-v2-pro"}
	if !reflect.DeepEqual(view.Models, want) {
		t.Errorf("ProviderView.Models = %v, want %v", view.Models, want)
	}
}

func TestSettingsIncludesProviderProfilesWithoutSecrets(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("OPENAI_MAIN_KEY", "sk-openai-secret")
	t.Setenv("ANTHROPIC_FRONTIER_KEY", "sk-anthropic-secret")

	cfg := config.Default()
	cfg.DefaultModel = "openai-main/gpt-4o"
	cfg.Agent.FrontierModel = "anthropic-frontier/claude-sonnet-4"
	cfg.Agent.FrontierBudget = 123456
	cfg.Agent.SubagentModel = "icodeeasy-small/qwen2.5-coder"
	cfg.Agent.SubagentModels = map[string]string{
		"advisor": "anthropic-frontier/claude-sonnet-4",
		"maker":   "openai-main/gpt-4o",
		"checker": "anthropic-frontier/claude-sonnet-4",
	}
	cfg.Providers = []config.ProviderEntry{
		{Name: "openai-main", Kind: "openai", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-4o"}, APIKeyEnv: "OPENAI_MAIN_KEY"},
		{Name: "anthropic-frontier", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet-4"}, APIKeyEnv: "ANTHROPIC_FRONTIER_KEY"},
		{Name: "icodeeasy-small", Kind: "openai", BaseURL: "https://gateway.icodeeasy.com/v1", Models: []string{"qwen2.5-coder"}, APIKeyEnv: "ICODEEASY_KEY"},
	}
	cfg.Desktop.ProviderAccess = []string{"openai-main", "anthropic-frontier", "icodeeasy-small"}
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	view := NewApp().Settings()
	openai := findProviderView(view.Providers, "openai-main")
	if openai == nil || !containsString(openai.Roles, "default") || !containsString(openai.Roles, "maker") {
		t.Fatalf("openai provider profile = %+v", openai)
	}
	if openai.RoleModels["default"] != "openai-main/gpt-4o" || openai.Gateway != "official_openai" || openai.CredentialStatus != "configured" {
		t.Fatalf("openai provider profile fields = %+v", openai)
	}
	frontier := findProviderView(view.Providers, "anthropic-frontier")
	if frontier == nil || !containsString(frontier.Roles, "frontier") || !containsString(frontier.Roles, "advisor") || !frontier.BudgetEligible {
		t.Fatalf("frontier provider profile = %+v", frontier)
	}
	small := findProviderView(view.Providers, "icodeeasy-small")
	if small == nil || small.Gateway != "icodeeasy" || small.CredentialStatus != "missing" || !small.SmallModelEligible {
		t.Fatalf("small provider profile = %+v", small)
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-openai-secret") || strings.Contains(string(raw), "sk-anthropic-secret") {
		t.Fatalf("settings JSON leaked credential: %s", raw)
	}
}

func TestSettingsSavesOfficialAuthProfileAndBearerTokenEnv(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("OPENAI_ACCESS_TOKEN", "sk-official-secret")
	cfg := config.Default()
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	err := app.SaveProvider(ProviderView{
		Name:                  "openai-official",
		Kind:                  "openai",
		BaseURL:               "https://api.openai.com/v1",
		Models:                []string{"gpt-5"},
		Default:               "gpt-5",
		APIKeyEnv:             "OLD_OPENAI_API_KEY",
		AuthType:              "official_auth",
		BearerTokenEnv:        "OPENAI_ACCESS_TOKEN",
		OfficialAuthProfileID: "openai-desktop",
	})
	if err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	saved := config.LoadForEdit(config.UserConfigPath())
	entry, ok := saved.Provider("openai-official")
	if !ok {
		t.Fatalf("openai-official missing after save: %+v", saved.Providers)
	}
	if entry.BearerTokenEnv != "OPENAI_ACCESS_TOKEN" || entry.OfficialAuthProfileID != "openai-desktop" {
		t.Fatalf("saved auth fields = %+v", entry)
	}
	view := app.Settings()
	p := findProviderView(view.Providers, "openai-official")
	if p == nil {
		t.Fatalf("settings provider missing: %+v", view.Providers)
	}
	if p.AuthMode != "official_auth" || p.CredentialEnv != "OPENAI_ACCESS_TOKEN" || p.OfficialAuthProfileID != "openai-desktop" {
		t.Fatalf("settings auth fields = %+v", p)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-official-secret") {
		t.Fatalf("settings JSON leaked token: %s", raw)
	}
}

func TestSettingsBearerAuthDoesNotDisplayOldAPIKeyEnvAsCredential(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name:           "openai-bearer",
		Kind:           "openai",
		BaseURL:        "https://api.openai.com/v1",
		Models:         []string{"gpt-5"},
		APIKeyEnv:      "OLD_OPENAI_API_KEY",
		AuthType:       "bearer",
		BearerTokenEnv: "OPENAI_ACCESS_TOKEN",
	}}
	cfg.Desktop.ProviderAccess = []string{"openai-bearer"}
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	view := NewApp().Settings()
	p := findProviderView(view.Providers, "openai-bearer")
	if p == nil {
		t.Fatalf("openai-bearer missing: %+v", view.Providers)
	}
	if p.CredentialEnv != "OPENAI_ACCESS_TOKEN" {
		t.Fatalf("CredentialEnv = %q, want bearer token env", p.CredentialEnv)
	}
}

func TestSettingsProviderProfilesReportUnresolvedRoleWarnings(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Agent.FrontierModel = "missing-frontier/claude"
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	view := NewApp().Settings()
	if len(view.ProviderProfileWarnings) == 0 {
		t.Fatalf("ProviderProfileWarnings = none, want unresolved frontier warning")
	}
	if view.ProviderProfileWarnings[0].Role != "frontier" || view.ProviderProfileWarnings[0].Ref != "missing-frontier/claude" {
		t.Fatalf("ProviderProfileWarnings = %+v", view.ProviderProfileWarnings)
	}
}

func TestSettingsExposesAndUpdatesContextPolicy(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Agent.ContextPolicy = "aggressive"
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteFile(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if got := app.Settings().ContextPolicy; got != "aggressive" {
		t.Fatalf("Settings.ContextPolicy = %q, want aggressive", got)
	}
	if err := app.SetContextPolicy("off"); err != nil {
		t.Fatalf("SetContextPolicy: %v", err)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).ContextPolicy(); got != "off" {
		t.Fatalf("saved context_policy = %q, want off", got)
	}
	if err := app.SetContextPolicy("invalid"); err == nil {
		t.Fatal("SetContextPolicy should reject invalid policy")
	}
}

func findProviderView(items []ProviderView, name string) *ProviderView {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestFetchProviderModelsFiltersNonChatModels(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "mimo-v2.5-pro", "object": "model"},
				{"id": "mimo-v2.5-asr", "object": "model"},
				{"id": "mimo-v2.5-tts", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	got, err := NewApp().FetchProviderModels(ProviderView{
		Name:      "mimo-api",
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_PROVIDER_KEY",
	})
	if err != nil {
		t.Fatalf("FetchProviderModels: %v", err)
	}
	want := []string{"mimo-v2.5-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FetchProviderModels = %v, want %v", got, want)
	}
}

func TestFetchProviderModelsUsesBearerAuthFromSettingsView(t *testing.T) {
	t.Setenv("OLD_OPENAI_API_KEY", "old-api-key")
	t.Setenv("OPENAI_ACCESS_TOKEN", "official-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer official-token" {
			http.Error(w, "bad bearer", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "gpt-5"}, {"id": "text-embedding-3-small"}},
		})
	}))
	defer srv.Close()

	got, err := NewApp().FetchProviderModels(ProviderView{
		Name:           "openai-official",
		Kind:           "openai",
		BaseURL:        srv.URL,
		APIKeyEnv:      "OLD_OPENAI_API_KEY",
		AuthType:       "bearer",
		BearerTokenEnv: "OPENAI_ACCESS_TOKEN",
	})
	if err != nil {
		t.Fatalf("FetchProviderModels: %v", err)
	}
	want := []string{"gpt-5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FetchProviderModels = %v, want %v", got, want)
	}
}

func TestSaveProviderFiltersNonChatModels(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:      "mimo-api",
		Kind:      "openai",
		BaseURL:   "https://api.xiaomimimo.com/v1",
		Models:    []string{"mimo-v2.5-asr", "mimo-v2.5-pro", "mimo-v2.5-tts"},
		Default:   "mimo-v2.5-asr",
		APIKeyEnv: "MIMO_API_KEY",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("saved provider not found")
	}
	want := []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
	if !reflect.DeepEqual(got.ModelList(), want) {
		t.Errorf("saved provider models = %v, want %v", got.ModelList(), want)
	}
	if got.DefaultModel() != "mimo-v2.5-pro" {
		t.Errorf("saved provider default = %q, want mimo-v2.5-pro", got.DefaultModel())
	}
}

func TestOfficialMimoAPITemplateIncludesVisionModels(t *testing.T) {
	entries, keyEnv, err := officialProviderTemplate("mimo-api")
	if err != nil {
		t.Fatalf("officialProviderTemplate: %v", err)
	}
	if keyEnv != "MIMO_API_KEY" || len(entries) != 1 {
		t.Fatalf("template = %v/%q, want one MIMO_API_KEY entry", entries, keyEnv)
	}
	got := entries[0]
	for _, model := range []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"} {
		if !got.HasModel(model) {
			t.Fatalf("mimo-api models = %v, missing %s", got.ModelList(), model)
		}
	}
	if got.DefaultModel() != "mimo-v2.5-pro" {
		t.Fatalf("mimo-api default = %q, want mimo-v2.5-pro", got.DefaultModel())
	}
}

func TestSetAgentParamsPersistsStepLimitsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetAgentParams(0.35, 37, 9, "custom system"); err != nil {
		t.Fatalf("SetAgentParams: %v", err)
	}

	view := app.Settings()
	if view.Agent.MaxSteps != 37 || view.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("Settings().Agent = %+v, want maxSteps=37 plannerMaxSteps=9", view.Agent)
	}
	if view.Agent.Temperature != 0.35 || view.Agent.SystemPrompt != "custom system" {
		t.Fatalf("Settings().Agent did not preserve other agent params: %+v", view.Agent)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.MaxSteps != 37 || cfg.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("saved config agent steps = max:%d planner:%d, want 37/9", cfg.Agent.MaxSteps, cfg.Agent.PlannerMaxSteps)
	}
	if cfg.Agent.Temperature != 0.35 || cfg.Agent.SystemPrompt != "custom system" {
		t.Fatalf("saved config did not preserve other agent params: %+v", cfg.Agent)
	}
}

func TestSetReasoningLanguagePersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetReasoningLanguage("zh"); err != nil {
		t.Fatalf("SetReasoningLanguage: %v", err)
	}

	view := app.Settings()
	if view.Agent.ReasoningLanguage != "zh" {
		t.Fatalf("Settings().Agent.ReasoningLanguage = %q, want zh", view.Agent.ReasoningLanguage)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.ReasoningLanguage != "zh" || cfg.ReasoningLanguage() != "zh" {
		t.Fatalf("saved reasoning language = %q/%q, want zh", cfg.Agent.ReasoningLanguage, cfg.ReasoningLanguage())
	}
}

func TestSetReasoningLanguageUpdatesLiveTabControllers(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "maddog.toml"), []byte("[agent]\nreasoning_language = \"en\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	userCtrl := control.New(control.Options{ReasoningLanguage: "auto"})
	projectCtrl := control.New(control.Options{ReasoningLanguage: "auto"})
	app.tabs = map[string]*WorkspaceTab{
		"user": {
			ID:          "user",
			Scope:       "global",
			Ctrl:        userCtrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
		"project": {
			ID:            "project",
			Scope:         "project",
			WorkspaceRoot: projectRoot,
			Ctrl:          projectCtrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "user"

	if err := app.SetReasoningLanguage("zh"); err != nil {
		t.Fatalf("SetReasoningLanguage: %v", err)
	}

	userComposed := userCtrl.Compose("hi")
	if !strings.Contains(userComposed, "Simplified Chinese") {
		t.Fatalf("user-level tab Compose = %q, want zh reasoning language", userComposed)
	}
	projectComposed := projectCtrl.Compose("hi")
	if !strings.Contains(projectComposed, "use English") {
		t.Fatalf("project override tab Compose = %q, want en reasoning language", projectComposed)
	}
}

func TestSetDesktopCheckUpdatesPersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if !app.Settings().CheckUpdates {
		t.Fatal("Settings().CheckUpdates default = false, want true")
	}
	if err := app.SetDesktopCheckUpdates(false); err != nil {
		t.Fatalf("SetDesktopCheckUpdates: %v", err)
	}
	view := app.Settings()
	if view.CheckUpdates {
		t.Fatal("Settings().CheckUpdates = true, want false")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Desktop.CheckUpdates == nil || *cfg.Desktop.CheckUpdates {
		t.Fatalf("desktop.check_updates = %+v, want false", cfg.Desktop.CheckUpdates)
	}
	if cfg.DesktopCheckUpdates() {
		t.Fatal("DesktopCheckUpdates() = true, want false")
	}
}

func TestSaveHooksSettingsPreservesUnknownSettingsKeys(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := hook.GlobalSettingsPath("")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","hooks":{"Stop":[{"command":"old"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if err := app.SaveHooksSettings("global", []HookConfigView{{
		Event:   string(hook.PreToolUse),
		Match:   "bash",
		Command: "echo guard",
	}}); err != nil {
		t.Fatalf("SaveHooksSettings: %v", err)
	}

	var raw map[string]json.RawMessage
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["theme"]) != `"dark"` {
		t.Fatalf("theme key was not preserved: %s", raw["theme"])
	}
	view := app.HooksSettings("global")
	if len(view.Hooks) != 1 || view.Hooks[0].Event != string(hook.PreToolUse) || view.Hooks[0].Command != "echo guard" {
		t.Fatalf("HooksSettings = %+v, want saved PreToolUse hook", view)
	}
}

func TestProjectHooksSettingsUseActiveWorkspaceRootAndTrust(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	project := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: project, Ready: true},
	}
	app.activeTabID = "project"

	if err := app.SaveHooksSettings("project", []HookConfigView{{
		Event:       string(hook.Stop),
		Command:     "echo done",
		Description: "Turn done",
	}}); err != nil {
		t.Fatalf("SaveHooksSettings(project): %v", err)
	}
	if err := app.TrustProjectHooks(); err != nil {
		t.Fatalf("TrustProjectHooks: %v", err)
	}
	if !hook.IsTrusted(project, home) {
		t.Fatal("project hooks were not trusted")
	}
	view := app.HooksSettings("project")
	if view.Scope != "project" || view.ProjectRoot != project || !view.Trusted {
		t.Fatalf("project hook view metadata = %+v", view)
	}
	if len(view.Hooks) != 1 || view.Hooks[0].Event != string(hook.Stop) || view.Hooks[0].Description != "Turn done" {
		t.Fatalf("project hooks = %+v", view.Hooks)
	}
	if _, err := os.Stat(filepath.Join(project, ".maddog", "settings.json")); err != nil {
		t.Fatalf("project hooks settings file missing: %v", err)
	}
}

func TestTrustProjectHooksForRootUsesDisplayedProjectRoot(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"a": {ID: "a", Scope: "project", WorkspaceRoot: projectA, Ready: true},
		"b": {ID: "b", Scope: "project", WorkspaceRoot: projectB, Ready: true},
	}
	app.activeTabID = "b"

	if err := app.TrustProjectHooksForRoot(projectA); err != nil {
		t.Fatalf("TrustProjectHooksForRoot: %v", err)
	}
	if !hook.IsTrusted(projectA, home) {
		t.Fatal("displayed project root was not trusted")
	}
	if hook.IsTrusted(projectB, home) {
		t.Fatal("active project root was trusted instead of displayed project root")
	}
}

func TestSaveHooksSettingsForRootUsesDisplayedProjectRoot(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"a": {ID: "a", Scope: "project", WorkspaceRoot: projectA, Ready: true},
		"b": {ID: "b", Scope: "project", WorkspaceRoot: projectB, Ready: true},
	}
	app.activeTabID = "b"

	if err := app.SaveHooksSettingsForRoot("project", projectA, []HookConfigView{{
		Event:   string(hook.Stop),
		Command: "echo done",
	}}); err != nil {
		t.Fatalf("SaveHooksSettingsForRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectA, ".maddog", "settings.json")); err != nil {
		t.Fatalf("displayed project root settings missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectB, ".maddog", "settings.json")); err == nil {
		t.Fatal("active project root was written instead of displayed project root")
	}
}
