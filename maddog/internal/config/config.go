// Package config loads Maddog's runtime configuration from TOML. Resolution order:
// flag > project ./maddog.toml > user ~/.config/maddog/config.toml > built-in defaults.
// Secrets come from the environment via api_key_env and are never stored in
// config files.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"maddog/internal/fileutil"
	"maddog/internal/netclient"
	"maddog/internal/provider"
)

var validSkillName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

const (
	AppName               = "maddog"
	AppDisplayName        = "Maddog"
	ProjectConfigFilename = "maddog.toml"
	ProjectConventionDir  = ".maddog"
)

// IsValidSkillName reports whether name is a usable skill identifier.
func IsValidSkillName(name string) bool { return validSkillName.MatchString(name) }

// SkillNameKey normalizes a skill identifier for config comparisons.
func SkillNameKey(name string) string {
	name = strings.TrimSpace(name)
	if !IsValidSkillName(name) {
		return ""
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

// Config is Maddog's runtime configuration.
type Config struct {
	ConfigVersion     int                     `toml:"config_version"`
	DefaultModel      string                  `toml:"default_model"`
	Language          string                  `toml:"language"` // ui/model language tag (e.g. "zh"); empty = auto-detect from $LANG / $MADDOG_LANG
	CredentialsStore  string                  `toml:"credentials_store"`
	Environment       EnvironmentConfig       `toml:"environment"`
	UI                UIConfig                `toml:"ui"`
	Desktop           DesktopConfig           `toml:"desktop"`
	Notifications     NotificationsConfig     `toml:"notifications"`
	Agent             AgentConfig             `toml:"agent"`
	Providers         []ProviderEntry         `toml:"providers"`
	Tools             ToolsConfig             `toml:"tools"`
	Permissions       PermissionsConfig       `toml:"permissions"`
	Sandbox           SandboxConfig           `toml:"sandbox"`
	Network           NetworkConfig           `toml:"network"`
	Plugins           []PluginEntry           `toml:"plugins"`
	Skills            SkillsConfig            `toml:"skills"`
	Codegraph         CodegraphConfig         `toml:"codegraph"`
	CodeIntelligence  CodeIntelligenceConfig  `toml:"code_intelligence"`
	BuiltInMCP        BuiltInMCPConfig        `toml:"builtin_mcp"`
	BuiltInMCPUpdates BuiltInMCPUpdatesConfig `toml:"builtin_mcp_updates"`
	Statusline        StatuslineConfig        `toml:"statusline"`
	LSP               LSPConfig               `toml:"lsp"`
	Bot               BotConfig               `toml:"bot"`

	providerSources          map[string]providerSourceScope
	shadowedProjectProviders []ProviderEntry
	expansionEnv             map[string]string
}

type providerSourceScope string

const (
	providerSourceUser    providerSourceScope = "user"
	providerSourceProject providerSourceScope = "project"
)

// UIConfig controls CLI presentation-only settings. Desktop appearance is kept in
// DesktopConfig so desktop preferences cannot alter terminal output or prompts.
type UIConfig struct {
	Theme          string `toml:"theme"`           // auto|dark|light; empty resolves to auto
	ThemeStyle     string `toml:"theme_style"`     // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	ShortcutLayout string `toml:"shortcut_layout"` // classic|desktop; accepted for compatibility
	CloseBehavior  string `toml:"close_behavior"`  // legacy desktop close behavior; prefer desktop.close_behavior
	ShowReasoning  bool   `toml:"show_reasoning"`  // Ctrl+O / /verbose: show thinking text in CLI; false = collapsed
	CursorShape    string `toml:"cursor_shape"`    // block|underline|bar; empty defaults to underline
}

// DesktopConfig controls desktop-only UI preferences. It is intentionally
// separate from top-level language and [ui] so desktop choices do not affect CLI
// language, terminal colours, or provider-visible prompt/request data.
type DesktopConfig struct {
	Language                string   `toml:"language"`                   // auto|en|zh; empty/auto = browser/OS auto-detect
	LayoutStyle             string   `toml:"layout_style"`               // workbench|creation; classic is a legacy alias for workbench
	WindowChrome            string   `toml:"window_chrome"`              // native|custom; custom uses frameless self-drawn window controls
	Theme                   string   `toml:"theme"`                      // auto|dark|light; empty resolves to auto
	ThemeStyle              string   `toml:"theme_style"`                // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	CloseBehavior           string   `toml:"close_behavior"`             // quit|background; desktop window close behavior
	DisplayMode             string   `toml:"display_mode"`               // standard|compact (legacy "minimal" maps to compact); transcript display mode
	StatusBarStyle          string   `toml:"status_bar_style"`           // icon|text; desktop status bar metric labels
	StatusBarItems          []string `toml:"status_bar_items"`           // ordered visible desktop status bar items
	DefaultToolApprovalMode string   `toml:"default_tool_approval_mode"` // ask|auto|yolo; default for newly-created desktop sessions
	CheckUpdates            *bool    `toml:"check_updates"`              // startup update checks; nil keeps the default enabled
	Telemetry               *bool    `toml:"telemetry"`                  // anonymous launch ping (install id + version + OS); nil keeps the default enabled
	Metrics                 *bool    `toml:"metrics"`                    // aggregate desktop metrics (anonymous signal/bucket counts; no content); nil keeps the default enabled
	ProviderAccess          []string `toml:"provider_access"`            // desktop-only list of provider entries shown in Settings > Model > Access
	ExpandThinking          bool     `toml:"expand_thinking"`            // true = show reasoning text expanded by default; false = collapsed
}

// NotificationsConfig controls optional system notifications for CLI chat/run.
type NotificationsConfig struct {
	Enabled         bool `toml:"enabled"`
	TurnDone        bool `toml:"turn_done"`
	ApprovalRequest bool `toml:"approval_request"`
	AskRequest      bool `toml:"ask_request"`
}

// EnvironmentConfig controls the stable startup environment block injected into
// the model-facing prompt. Enabled nil means the default (enabled); Tools maps a
// tool name to an explicit executable path when PATH probing is not enough.
type EnvironmentConfig struct {
	Enabled *bool             `toml:"enabled"`
	Tools   map[string]string `toml:"tools"`
}

// EnvironmentEnabled reports whether startup environment probing should feed the
// cache-stable system prompt.
func (c *Config) EnvironmentEnabled() bool {
	return c == nil || c.Environment.Enabled == nil || *c.Environment.Enabled
}

// UITheme normalizes ui.theme to a supported value.
func (c *Config) UITheme() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.Theme)) {
	case "dark":
		return "dark"
	case "light":
		return "light"
	default:
		return "auto"
	}
}

// UIThemeStyle normalizes ui.theme_style. Empty means "pick the default style
// for the resolved light/dark shell".
func (c *Config) UIThemeStyle() string {
	return normalizeThemeStyle(c.UI.ThemeStyle)
}

// UIShortcutLayout normalizes the legacy CLI shortcut layout setting. It is kept
// for compatibility; Shift+Tab toggles Plan and Ctrl+Y toggles YOLO in both
// layouts.
func (c *Config) UIShortcutLayout() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.ShortcutLayout)) {
	case "desktop", "dual", "dual-axis", "dual_axis":
		return "desktop"
	default:
		return "classic"
	}
}

// UICursorShape normalizes ui.cursor_shape. Defaults to "underline" to avoid
// block-cursor visual corruption with CJK wide characters in the textarea
// (Bubble Tea real-cursor + CJK column-counting drift). Valid values:
// "block", "underline", "bar".
func (c *Config) UICursorShape() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.CursorShape)) {
	case "block":
		return "block"
	case "bar":
		return "bar"
	default:
		return "underline"
	}
}

func normalizeThemeStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "graphite", "aurora", "slate", "carbon", "nocturne", "amber", "ember", "midnight", "sandstone", "porcelain", "linen", "glacier":
		return strings.ToLower(strings.TrimSpace(style))
	default:
		return ""
	}
}

func normalizeDesktopLayoutStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "classic", "workbench", "workspace":
		return "workbench"
	case "creation":
		return "creation"
	default:
		return "workbench"
	}
}

func normalizeDesktopWindowChrome(chrome string) string {
	switch strings.ToLower(strings.TrimSpace(chrome)) {
	case "custom", "frameless", "self-drawn", "self_drawn", "selfdrawn":
		return "custom"
	default:
		return "native"
	}
}

func normalizeCloseBehavior(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quit", "exit":
		return "quit"
	default:
		return "background"
	}
}

// DesktopLanguage normalizes the desktop UI language. Empty means auto-detect
// from the browser/OS locale; it deliberately does not read top-level language,
// which is used by the CLI/model-facing runtime.
func (c *Config) DesktopLanguage() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Language)) {
	case "en":
		return "en"
	case "zh":
		return "zh"
	default:
		return ""
	}
}

// DesktopTheme normalizes desktop.theme. New desktop users default to the OS
// automatic graphite product look; an explicit auto/light/dark is preserved.
func (c *Config) DesktopTheme() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Theme)) {
	case "auto":
		return "auto"
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return "auto"
	}
}

// DesktopThemeStyle normalizes desktop.theme_style. Empty means the frontend
// chooses the default style for the resolved desktop theme.
func (c *Config) DesktopThemeStyle() string {
	return normalizeThemeStyle(c.Desktop.ThemeStyle)
}

// DesktopLayoutStyle normalizes the desktop layout style. New installs and
// legacy classic configs both resolve to the v1.13-aligned workbench layout.
func (c *Config) DesktopLayoutStyle() string {
	if strings.EqualFold(strings.TrimSpace(c.Desktop.ThemeStyle), "workbench") && strings.TrimSpace(c.Desktop.LayoutStyle) == "" {
		return "workbench"
	}
	return normalizeDesktopLayoutStyle(c.Desktop.LayoutStyle)
}

// DesktopWindowChrome normalizes the native-vs-self-drawn desktop window chrome
// choice. Native is the v1.13-aligned default; custom requires a restart because
// Wails' Frameless option is set when the window is created.
func (c *Config) DesktopWindowChrome() string {
	return normalizeDesktopWindowChrome(c.Desktop.WindowChrome)
}

func (c *Config) DesktopUsesCustomWindowChrome() bool {
	return c.DesktopWindowChrome() == "custom"
}

// DesktopCloseBehavior normalizes the desktop close-window preference. It falls
// back to the legacy ui.close_behavior value for configs written before [desktop]
// existed.
func (c *Config) DesktopCloseBehavior() string {
	if strings.TrimSpace(c.Desktop.CloseBehavior) != "" {
		return normalizeCloseBehavior(c.Desktop.CloseBehavior)
	}
	return normalizeCloseBehavior(c.UI.CloseBehavior)
}

// UICloseBehavior is the legacy name for DesktopCloseBehavior.
func (c *Config) UICloseBehavior() string {
	return c.DesktopCloseBehavior()
}

// DesktopDisplayMode normalizes the transcript display mode. Default is
// "standard" (flat rendering, no folding).
func (c *Config) DesktopDisplayMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.DisplayMode)) {
	case "standard":
		return "standard"
	case "compact", "minimal":
		return "compact"
	default:
		return "standard"
	}
}

// NormalizeToolApprovalMode returns the canonical desktop/session tool approval
// posture. Unknown or missing values fall back to ask for safety.
func NormalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "auto"
	case "yolo", "full", "full-access", "bypass":
		return "yolo"
	default:
		return "ask"
	}
}

// DesktopDefaultToolApprovalMode is the Ask/Auto/YOLO default used only when
// creating a new desktop session. Existing tabs and restored sessions keep their
// own persisted runtime state.
func (c *Config) DesktopDefaultToolApprovalMode() string {
	if c == nil {
		return "ask"
	}
	return NormalizeToolApprovalMode(c.Desktop.DefaultToolApprovalMode)
}

// DesktopStatusBarStyle normalizes the desktop status bar metric label style.
// Default is "text"; explicit "icon" preserves the user's compact choice.
func (c *Config) DesktopStatusBarStyle() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.StatusBarStyle)) {
	case "icon":
		return "icon"
	case "text":
		return "text"
	default:
		return "text"
	}
}

var defaultDesktopStatusBarItems = []string{
	"model",
	"workspace",
	"git_branch",
	"cache",
	"cache_avg",
	"session_tokens",
	"turn_tokens",
	"turn_cost",
	"session_turns",
	"context",
	"compact",
	"cost",
	"balance",
}

var knownDesktopStatusBarItems = map[string]bool{
	"model":           true,
	"cache":           true,
	"cache_avg":       true,
	"session_tokens":  true,
	"turn_tokens":     true,
	"turn_cost":       true,
	"session_turns":   true,
	"context":         true,
	"compact":         true,
	"cost":            true,
	"balance":         true,
	"provider":        true,
	"frontier_budget": true,
	"provider_health": true,
	"rate_limit":      true,
}

// DefaultDesktopStatusBarItems returns the default ordered visible desktop
// status bar items.
func DefaultDesktopStatusBarItems() []string {
	return append([]string(nil), defaultDesktopStatusBarItems...)
}

// DesktopStatusBarItems normalizes the ordered visible desktop status bar items.
// An unset or empty list uses the default full set; explicit non-empty lists
// preserve user order and omit hidden items.
func (c *Config) DesktopStatusBarItems() []string {
	return normalizeDesktopStatusBarItems(c.Desktop.StatusBarItems)
}

func normalizeDesktopStatusBarItems(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		id := strings.TrimSpace(raw)
		if !knownDesktopStatusBarItems[id] || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	if len(out) == 0 {
		return DefaultDesktopStatusBarItems()
	}
	return out
}

// DesktopCheckUpdates reports whether the desktop should check for updates on
// startup. Missing configs default to true so existing users keep update notices.
func (c *Config) DesktopCheckUpdates() bool {
	if c == nil || c.Desktop.CheckUpdates == nil {
		return true
	}
	return *c.Desktop.CheckUpdates
}

// ColdResumePruneEnabled reports whether stale tool results are elided when a
// session resumes past the provider cache window. Default true (cheaper cold
// restart); users keep full history by disabling it.
func (c *Config) ColdResumePruneEnabled() bool {
	if c == nil || c.Agent.ColdResumePrune == nil {
		return true
	}
	return *c.Agent.ColdResumePrune
}

// ResponseLanguage normalizes the top-level language preference for final
// answers. Empty means auto: replies follow the current user turn.
func (c *Config) ResponseLanguage() string {
	if c == nil {
		return "auto"
	}
	return NormalizeLanguage(c.Language)
}

// NormalizeLanguage returns one of auto|zh|en for UI/default reply language settings.
func NormalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto", "detect", "default":
		return "auto"
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// ReasoningLanguage normalizes agent.reasoning_language. Empty means auto:
// visible reasoning follows the conversation language already described by the
// stable LanguagePolicy. Legacy "default" is treated as auto.
func (c *Config) ReasoningLanguage() string {
	if c == nil {
		return "auto"
	}
	return NormalizeReasoningLanguage(c.Agent.ReasoningLanguage)
}

// NormalizeReasoningLanguage returns one of auto|zh|en.
func NormalizeReasoningLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto", "follow", "conversation", "detect", "default", "model", "model-default", "model_default", "provider":
		return "auto"
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// DesktopTelemetry reports whether the desktop sends the anonymous launch ping.
// It carries no conversation, key, or file data — see desktop/README.md.
func (c *Config) DesktopTelemetry() bool {
	if c == nil || c.Desktop.Telemetry == nil {
		return true
	}
	return *c.Desktop.Telemetry
}

// DesktopMetrics reports whether the desktop sends aggregate desktop metrics —
// anonymous (signal, bucket) counters, never content. Default on.
func (c *Config) DesktopMetrics() bool {
	if c == nil || c.Desktop.Metrics == nil {
		return true
	}
	return *c.Desktop.Metrics
}

// LSPConfig governs the optional Language Server Protocol tools (lsp_definition,
// lsp_references, lsp_hover, lsp_diagnostics). Enabled defaults to true; the
// servers themselves are never bundled — each resolves on PATH and the tool
// returns an install hint when it is missing, so the capability is dormant until
// the user installs a server. Servers overrides or extends the built-in language
// → server map, keyed by language id (e.g. "go", "rust", "python").
type LSPConfig struct {
	Enabled bool                 `toml:"enabled"`
	Servers map[string]LSPServer `toml:"servers"`
}

// LSPServer overrides a built-in language's server or, when keyed by a new
// language, adds one. An empty field falls back to the built-in default for that
// language; Extensions is required when adding a language the built-ins don't
// cover (e.g. ".ex" for Elixir) so files route to it.
type LSPServer struct {
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Env         map[string]string `toml:"env"`
	LanguageID  string            `toml:"language_id"`
	Extensions  []string          `toml:"extensions"`
	InstallHint string            `toml:"install_hint"`
}

// StatuslineConfig configures a custom status line. Command, when set, is run at
// startup and after each turn; its first line of stdout replaces the built-in
// status data row. A JSON payload (model, context tokens, cwd) is fed on stdin.
type StatuslineConfig struct {
	Command string `toml:"command"`
}

// CodegraphConfig governs the built-in CodeGraph MCP server — symbol/call-graph
// code intelligence (tree-sitter + SQLite) that gives the agent codegraph_*
// search / context / explore / trace / node tools. Enabled defaults to true so
// upgrades keep it for existing configs; first-run scaffolds write enabled =
// false so only brand-new users start without it. AutoInstall (default true)
// lets maddog fetch the CodeGraph runtime into its cache when CodeGraph is
// enabled but missing; set false to require an explicit `maddog codegraph
// install` (e.g. for air-gapped or headless runs). Path overrides binary
// resolution; empty resolves the cache, then a `codegraph` on PATH, then a
// bundle beside the executable. CodeGraph always starts in the background when
// enabled; legacy tier values are ignored and removed during config load.
type CodegraphConfig struct {
	Enabled     bool   `toml:"enabled"`
	AutoInstall bool   `toml:"auto_install"`
	Path        string `toml:"path"`
	Tier        string `toml:"tier"`
}

func (c CodegraphConfig) ShouldAutoStart() bool {
	return c.Enabled
}

func (c CodegraphConfig) ResolvedTier() string {
	return "background"
}

// CodeIntelligenceConfig declares optional external code-intelligence backends.
// The built-in CodeGraph backend remains configured by [codegraph] and is always
// considered separately so an external MCP cannot silently replace it.
type CodeIntelligenceConfig struct {
	Backends []CodeIntelligenceBackendConfig `toml:"backends"`
}

// CodeIntelligenceBackendConfig maps an MCP server's tools onto Maddog's
// abstract code-intelligence capabilities. Enabled nil means enabled; false
// keeps the backend visible to management surfaces but out of the usable set.
type CodeIntelligenceBackendConfig struct {
	Name    string            `toml:"name"`
	Kind    string            `toml:"kind"`
	Server  string            `toml:"server"`
	Enabled *bool             `toml:"enabled"`
	Tools   map[string]string `toml:"tools"`
}

func (c CodeIntelligenceBackendConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// BuiltInMCPConfig controls Maddog-shipped MCP servers that require no user
// server definition. They are off by default and become provider-visible only
// after the user enables them.
type BuiltInMCPConfig struct {
	TimeEnabled     bool `toml:"time_enabled"`
	Context7Enabled bool `toml:"context7_enabled"`
}

func (c BuiltInMCPConfig) Enabled(name string) bool {
	switch name {
	case "time":
		return c.TimeEnabled
	case "context7":
		return c.Context7Enabled
	default:
		return false
	}
}

func (c *BuiltInMCPConfig) SetEnabled(name string, enabled bool) bool {
	switch name {
	case "time":
		c.TimeEnabled = enabled
		return true
	case "context7":
		c.Context7Enabled = enabled
		return true
	default:
		return false
	}
}

func (c BuiltInMCPConfig) EnabledNames() []string {
	var out []string
	if c.TimeEnabled {
		out = append(out, "time")
	}
	if c.Context7Enabled {
		out = append(out, "context7")
	}
	return out
}

const (
	BuiltInMCPUpdateModeOff             = "off"
	BuiltInMCPUpdateModeNotify          = "notify"
	BuiltInMCPUpdateModeDownload        = "download"
	BuiltInMCPUpdateModeAutoNextSession = "auto_next_session"

	defaultBuiltInMCPUpdateInterval = 24 * time.Hour
)

// BuiltInMCPUpdatesConfig controls background update checks for Maddog-owned
// built-in MCP runtimes. The default is notify-only so startup never silently
// changes provider-visible MCP tool schemas.
type BuiltInMCPUpdatesConfig struct {
	Mode          string `toml:"mode"`
	CheckInterval string `toml:"check_interval"`
}

func (c BuiltInMCPUpdatesConfig) ResolvedMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case BuiltInMCPUpdateModeOff:
		return BuiltInMCPUpdateModeOff
	case BuiltInMCPUpdateModeDownload:
		return BuiltInMCPUpdateModeDownload
	case BuiltInMCPUpdateModeAutoNextSession:
		return BuiltInMCPUpdateModeAutoNextSession
	default:
		return BuiltInMCPUpdateModeNotify
	}
}

func (c BuiltInMCPUpdatesConfig) CheckIntervalDuration() time.Duration {
	raw := strings.TrimSpace(c.CheckInterval)
	if raw == "" {
		return defaultBuiltInMCPUpdateInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultBuiltInMCPUpdateInterval
	}
	return d
}

func (c BuiltInMCPUpdatesConfig) ResolvedCheckInterval() string {
	return c.CheckIntervalDuration().String()
}

// BotConfig 控制多渠道 IM bot 消息网关。
type BotConfig struct {
	Enabled          bool                  `toml:"enabled"`
	Model            string                `toml:"model"` // 用于 bot 的模型名，空则用 default_model
	ToolApprovalMode string                `toml:"tool_approval_mode"`
	MaxSteps         int                   `toml:"max_steps"`
	DebounceMs       int                   `toml:"debounce_ms"` // 消息合并窗口，毫秒
	Allowlist        BotAllowlist          `toml:"allowlist"`
	QQ               QQBotConfig           `toml:"qq"`
	Feishu           FeishuBotConfig       `toml:"feishu"`
	Weixin           WeixinBotConfig       `toml:"weixin"`
	Connections      []BotConnectionConfig `toml:"connections"`
}

// BotAllowlist 控制哪些用户可以使用 bot。
type BotAllowlist struct {
	Enabled      bool     `toml:"enabled"`
	AllowAll     bool     `toml:"allow_all"`
	QQUsers      []string `toml:"qq_users"`
	FeishuUsers  []string `toml:"feishu_users"`
	WeixinUsers  []string `toml:"weixin_users"`
	QQGroups     []string `toml:"qq_groups"`
	FeishuGroups []string `toml:"feishu_groups"`
	WeixinGroups []string `toml:"weixin_groups"`
}

// QQBotConfig QQ 官方 Bot API v2 配置。
type QQBotConfig struct {
	Enabled      bool   `toml:"enabled"`
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"` // 环境变量名，如 QQ_BOT_APP_SECRET
	Sandbox      bool   `toml:"sandbox"`        // true 使用 QQ 沙箱 API / gateway
}

// FeishuBotConfig 飞书自建应用 Bot 配置。
type FeishuBotConfig struct {
	Enabled           bool   `toml:"enabled"`
	Domain            string `toml:"domain"` // feishu（默认）| lark
	AppID             string `toml:"app_id"`
	AppSecretEnv      string `toml:"app_secret_env"`     // 如 FEISHU_BOT_APP_SECRET
	VerificationToken string `toml:"verification_token"` // 事件订阅验证 token
	Mode              string `toml:"mode"`               // webhook（默认）| websocket
	WebhookPort       int    `toml:"webhook_port"`       // webhook 模式端口
	RequireMention    bool   `toml:"require_mention"`
}

// WeixinBotConfig 微信 iLink Bot 配置。
type WeixinBotConfig struct {
	Enabled   bool   `toml:"enabled"`
	AccountID string `toml:"account_id"`
	TokenEnv  string `toml:"token_env"` // 环境变量名，如 WEIXIN_BOT_TOKEN
	APIBase   string `toml:"api_base"`  // iLink API base URL
}

// BotConnectionConfig is the desktop-friendly connection record for IM bot
// channels. It keeps install/runtime state separate from legacy per-provider
// knobs so the UI can expose a simple "connect first" flow while old configs
// keep working.
type BotConnectionConfig struct {
	ID               string                        `toml:"id"`
	Provider         string                        `toml:"provider"` // qq|feishu|weixin
	Domain           string                        `toml:"domain"`   // feishu|lark|weixin|qq
	Label            string                        `toml:"label"`
	Enabled          bool                          `toml:"enabled"`
	Status           string                        `toml:"status"` // disconnected|pending|connected|error
	Model            string                        `toml:"model"`
	ToolApprovalMode string                        `toml:"tool_approval_mode"`
	WorkspaceRoot    string                        `toml:"workspace_root"`
	Credential       BotConnectionCredential       `toml:"credential"`
	SessionMappings  []BotConnectionSessionMapping `toml:"session_mappings"`
	LastError        string                        `toml:"last_error"`
	CreatedAt        string                        `toml:"created_at"`
	UpdatedAt        string                        `toml:"updated_at"`
}

type BotConnectionCredential struct {
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"`
	AccountID    string `toml:"account_id"`
	TokenEnv     string `toml:"token_env"`
}

type BotConnectionSessionMapping struct {
	RemoteID      string `toml:"remote_id"`
	SessionID     string `toml:"session_id"`
	SessionSource string `toml:"session_source"`
	ChatType      string `toml:"chat_type"`
	UserID        string `toml:"user_id"`
	ThreadID      string `toml:"thread_id"`
	Scope         string `toml:"scope"`
	WorkspaceRoot string `toml:"workspace_root"`
	UpdatedAt     string `toml:"updated_at"`
}

// ServeConfig controls the HTTP serve frontend security settings.
type ServeConfig struct {
	// AuthMode selects the authentication mode for the HTTP serve frontend.
	// "none" (default): no authentication.
	// "token": a pre-shared token in the URL query string.
	// "password": a login page with bcrypt password verification.
	AuthMode string `toml:"auth_mode"`
	// Token is a pre-shared token for auth_mode = "token". When empty, a
	// cryptographically random token is generated at startup and printed.
	Token string `toml:"token"`
	// PasswordHash is a bcrypt hash of the password for auth_mode = "password".
	// Generate one with: maddog serve --hash-password --password '...'
	PasswordHash string `toml:"password_hash"`
	// BehindProxy indicates the server sits behind a trusted reverse proxy
	// (nginx, Caddy, Cloudflare, etc.) that sets X-Forwarded-For and
	// X-Forwarded-Proto headers. When true, those headers are used for
	// rate-limiting and Secure-cookie decisions. When false (default), they
	// are ignored — an attacker can otherwise forge them.
	BehindProxy bool `toml:"behind_proxy"`
}

// NetworkConfig controls ordinary outbound HTTP traffic such as model providers,
// wallet-balance lookups, updater checks, CodeGraph downloads, and web_fetch.
// web_fetch reuses these proxy settings while keeping its own SSRF-guarded
// dialer.
type NetworkConfig struct {
	// ProxyMode is "auto" (default; environment proxy for now), "env", "custom",
	// or "off". auto leaves room for OS proxy detection later without changing the
	// config shape.
	ProxyMode string `toml:"proxy_mode"`
	// ProxyURL is an advanced custom override such as "socks5://127.0.0.1:7890".
	// When set and proxy_mode = "custom", it wins over the structured proxy table.
	ProxyURL string `toml:"proxy_url"`
	// NoProxy is honored for custom proxies. Env/auto modes use NO_PROXY from the
	// process environment instead.
	NoProxy string             `toml:"no_proxy"`
	Proxy   NetworkProxyConfig `toml:"proxy"`
}

// NetworkProxyConfig is the structured custom-proxy editor shape. Password is
// optional and supports ${VAR} expansion, so users can avoid storing it literally.
type NetworkProxyConfig struct {
	Type     string `toml:"type"` // http|https|socks5|socks5h
	Server   string `toml:"server"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// NetworkProxySpec returns the expanded proxy settings used by netclient.
func (c *Config) NetworkProxySpec() netclient.ProxySpec {
	return netclient.ProxySpec{
		Mode:        c.Network.ProxyMode,
		URL:         c.expandVars(c.Network.ProxyURL),
		NoProxy:     c.expandVars(c.Network.NoProxy),
		Type:        c.Network.Proxy.Type,
		Server:      c.expandVars(c.Network.Proxy.Server),
		Port:        c.Network.Proxy.Port,
		Username:    c.expandVars(c.Network.Proxy.Username),
		Password:    c.expandVars(c.Network.Proxy.Password),
		DirectHosts: c.directProxyHosts(),
	}
}

// directProxyHosts collects the base_url hosts of providers marked no_proxy, so
// netclient bypasses the proxy for them without knowing any provider by name.
//
// Only for an auto-detected proxy (auto/env): that proxy is typically a
// GFW-circumvention one not meant for domestic endpoints (e.g. mimo), so keep
// them direct. An explicit proxy_mode = "custom" is the user saying "route
// everything through this" — e.g. a mandatory corporate proxy — so honor it for
// every provider; a custom-proxy user who wants a host direct uses
// network.no_proxy instead (#3635).
func (c *Config) directProxyHosts() []string {
	if c.NetworkProxyMode() == netclient.ModeCustom {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range c.Providers {
		if !p.NoProxy {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(p.BaseURL))
		if err != nil {
			continue
		}
		if h := u.Hostname(); h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// NetworkProxyMode normalizes network.proxy_mode to a known value.
func (c *Config) NetworkProxyMode() string {
	return netclient.NormalizeMode(c.Network.ProxyMode)
}

// SkillsConfig configures skill discovery. Paths adds extra "custom"-scope skill
// roots — each a directory of SKILL.md / <name>.md playbooks — scanned between
// the project roots (.maddog/.agents/.agent/.claude under the workspace) and
// the global roots. ExcludedPaths hides matching discovery roots without deleting
// folders. ~, relative paths, and ${VAR} expansion are supported. DisabledSkills
// hides named skills from the agent prompt, slash invocation, and skill tools
// while keeping them manageable.
type SkillsConfig struct {
	Paths                []string `toml:"paths"`
	ExcludedPaths        []string `toml:"excluded_paths"`
	DisabledSkills       []string `toml:"disabled_skills"`
	MaxDepth             int      `toml:"max_depth"`
	RuntimeOrchestration bool     `toml:"runtime_orchestration"`
	DynamicSkills        bool     `toml:"dynamic_skills"`
}

// SkillCustomPaths returns the configured custom skill roots with ${VAR}
// expanded; empty entries are dropped.
func (c *Config) SkillCustomPaths() []string {
	var out []string
	for _, p := range c.Skills.Paths {
		if p = c.expandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillExcludedPaths returns configured skill roots that should be hidden from
// discovery, with ${VAR} expanded and empty entries dropped.
func (c *Config) SkillExcludedPaths() []string {
	var out []string
	for _, p := range c.Skills.ExcludedPaths {
		if p = c.expandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillMaxDepth bounds nested skill discovery. Depth 3 favors bundled skill
// packs while Store keeps nested markdown safe by requiring descriptions.
func (c *Config) SkillMaxDepth() int {
	const (
		defaultDepth = 3
		maxDepth     = 5
	)
	if c == nil || c.Skills.MaxDepth == 0 {
		return defaultDepth
	}
	if c.Skills.MaxDepth < 1 {
		return 1
	}
	if c.Skills.MaxDepth > maxDepth {
		return maxDepth
	}
	return c.Skills.MaxDepth
}

// DisabledSkillNames returns valid disabled skill identifiers, preserving the
// first spelling and dropping duplicates/empty entries.
func (c *Config) DisabledSkillNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range c.Skills.DisabledSkills {
		name = strings.TrimSpace(name)
		if !IsValidSkillName(name) {
			continue
		}
		key := SkillNameKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// IsSkillDisabled reports whether name is configured as disabled.
func (c *Config) IsSkillDisabled(name string) bool {
	key := SkillNameKey(name)
	if key == "" {
		return false
	}
	for _, disabled := range c.DisabledSkillNames() {
		if SkillNameKey(disabled) == key {
			return true
		}
	}
	return false
}

// SandboxConfig bounds the blast radius of tool calls (Phase 0: file-writer
// confinement). WorkspaceRoot is the directory the built-in file writers
// (write_file / edit_file / multi_edit / move_file) may modify; empty means the
// current working directory, so writes stay inside the project by default.
// AllowWrite lists extra directories writers may also touch (e.g. a sibling repo
// or a temp dir). ForbidRead lists directories the agent may not read or list at all
// (e.g. ~/.ssh for secrets). Both support ${VAR} / ${VAR:-default} expansion. Reads are
// unrestricted; confining `bash` is Phase 1 (OS-level sandbox).
type SandboxConfig struct {
	WorkspaceRoot string   `toml:"workspace_root"`
	AllowWrite    []string `toml:"allow_write"`
	ForbidRead    []string `toml:"forbid_read"`
	// Bash is the OS-sandbox mode for the bash tool: "enforce" (default) jails
	// each command, "off" runs it unconfined. Phase 1; macOS only for now, with
	// a graceful fallback elsewhere (see internal/sandbox).
	Bash string `toml:"bash"`
	// Network allows network egress from inside the bash sandbox. Defaults true
	// so module/package downloads keep working; the boundary is then writes.
	Network bool `toml:"network"`
}

// WriteRoots returns the directories file-writer tools may modify: the
// workspace root (defaulting to the current working directory when unset), plus
// any AllowWrite extras, with ${VAR} expanded. The roots are returned as given
// (relative or absolute); the confiner resolves them to absolute, symlink-free
// paths. The result is always non-empty, so confinement is on by default.
func (c *Config) WriteRoots() []string {
	return c.WriteRootsForRoot(".")
}

// WriteRootsForRoot is like WriteRoots but falls back to fallbackRoot when the
// config doesn't explicitly set a workspace_root. Desktop tabs pass their
// project root here so tool confinement is correct without changing cwd.
func (c *Config) WriteRootsForRoot(fallbackRoot string) []string {
	root := c.expandVars(c.Sandbox.WorkspaceRoot)
	if root == "" {
		root = fallbackRoot
		if root == "" || root == "." {
			if wd, err := os.Getwd(); err == nil {
				root = wd
			} else {
				root = "."
			}
		}
	}
	roots := []string{root}
	for _, d := range c.Sandbox.AllowWrite {
		if d = c.expandVars(d); d != "" {
			roots = append(roots, d)
		}
	}
	return roots
}

// ForbidReadRoots returns the directories the agent is forbidden from reading
// or listing, with ${VAR} expanded. Relative roots are resolved against the
// current working directory; the confiner resolves them to symlink-free paths.
// Empty when no forbid_read entries are configured.
func (c *Config) ForbidReadRoots() []string {
	return c.ForbidReadRootsForRoot(".")
}

// ForbidReadRootsForRoot is like ForbidReadRoots but uses fallbackRoot when
// resolving relative paths (for desktop tabs that pass their project root).
func (c *Config) ForbidReadRootsForRoot(fallbackRoot string) []string {
	root := fallbackRoot
	if root == "" || root == "." {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		} else {
			root = "."
		}
	}
	roots := make([]string, 0, len(c.Sandbox.ForbidRead))
	for _, d := range c.Sandbox.ForbidRead {
		if d = c.expandVars(d); d != "" {
			if !filepath.IsAbs(d) {
				d = filepath.Join(root, d)
			}
			roots = append(roots, d)
		}
	}
	return roots
}

// BashMode normalises the bash-sandbox mode: only an explicit "off" disables
// it; empty or any other value resolves to "enforce", so the sandbox is on by
// default and fails safe.
func (c *Config) BashMode() string {
	if c.Sandbox.Bash == "off" {
		return "off"
	}
	return "enforce"
}

// AgentConfig configures the harness loop. PlannerModel is optional: when set
// to another provider's name it enables two-model collaboration, where the
// planner handles low-frequency planning in its own session (kept separate so
// each model's prompt prefix stays cache-stable). SubagentModel is the optional
// default for runAs=subagent skills; SubagentModels overrides it per skill name.
type AgentConfig struct {
	SystemPrompt              string            `toml:"system_prompt"`
	SystemPromptFile          string            `toml:"system_prompt_file"`
	MaxSteps                  int               `toml:"max_steps"`         // tool-call rounds per turn; 0 = unlimited
	PlannerMaxSteps           int               `toml:"planner_max_steps"` // planner read-only tool-call rounds; 0 = unlimited
	Temperature               float64           `toml:"temperature"`
	PlannerModel              string            `toml:"planner_model"`
	SubagentModel             string            `toml:"subagent_model"`
	SubagentModels            map[string]string `toml:"subagent_models"`
	SubagentEffort            string            `toml:"subagent_effort"`
	SubagentEfforts           map[string]string `toml:"subagent_efforts"`
	FrontierModel             string            `toml:"frontier_model"`
	UpgradeThreshold          int               `toml:"upgrade_threshold"`
	FrontierBudget            int64             `toml:"frontier_budget"`
	UpgradeEnabled            bool              `toml:"upgrade_enabled"`
	AdvisorMaxUsesPerTurn     int               `toml:"advisor_max_uses_per_turn"`
	AdvisorMaxUsesPerSession  int               `toml:"advisor_max_uses_per_session"`
	AdvisorMaxContextMessages int               `toml:"advisor_max_context_messages"`
	AdvisorMaxContextChars    int               `toml:"advisor_max_context_chars"`
	AdvisorNativeEnabled      bool              `toml:"advisor_native_enabled"`
	AdvisorNativeMaxTokens    int               `toml:"advisor_native_max_tokens"`
	GuardianModel             string            `toml:"guardian_model"`
	GuardianTemperature       float64           `toml:"guardian_temperature"`
	// OutputStyle selects a persona/tone block folded into the system prompt at
	// startup (a built-in like "explanatory"/"learning"/"concise", or a custom
	// .maddog/output-styles/<name>.md). Empty = the unmodified prompt.
	OutputStyle string `toml:"output_style"`
	// AutoPlan controls whether interactive turns that look multi-step start in
	// plan mode automatically: "off" keeps plan mode manual, "on" enables the
	// approval gate. Legacy "ask" is treated as "on".
	AutoPlan string `toml:"auto_plan"`
	// ReasoningLanguage controls the preferred language for visible reasoning
	// text. Empty/auto follows the conversation language. Applied as transient
	// turn context, not the stable prompt.
	ReasoningLanguage string `toml:"reasoning_language"`
	// AutoPlanClassifier optionally names a provider/model used to classify
	// borderline auto-plan decisions. Empty keeps the zero-cost heuristic path.
	AutoPlanClassifier   string               `toml:"auto_plan_classifier"`
	PlanModeAllowedTools []string             `toml:"plan_mode_allowed_tools"`
	MemoryCompiler       MemoryCompilerConfig `toml:"memory_compiler"`
	// Compaction window fractions: soft = notice only, compact = trigger, force = hard ceiling.
	SoftCompactRatio    float64 `toml:"soft_compact_ratio"`
	ToolResultSnipRatio float64 `toml:"tool_result_snip_ratio"`
	CompactRatio        float64 `toml:"compact_ratio"`
	CompactForceRatio   float64 `toml:"compact_force_ratio"`
	// Keep controls which compactable messages stay verbatim beyond the current
	// user-fact/digest floor and recent tail. Empty uses the conservative default
	// of keeping error tool results.
	Keep       []string `toml:"keep"`
	RecentKeep int      `toml:"recent_keep"`
	// ColdResumePrune elides stale tool results when a session reopens past the
	// provider cache window. nil = default enabled.
	ColdResumePrune *bool `toml:"cold_resume_prune"`
	// ContextCompression controls deterministic compression of high-volume tool
	// outputs before they enter model context.
	ContextCompression ContextCompressionConfig `toml:"context_compression"`
}

type MemoryCompilerConfig struct {
	Enabled *bool `toml:"enabled"`
}

func (c *Config) MemoryCompilerEnabled() bool {
	return c != nil && c.Agent.MemoryCompiler.Enabled != nil && *c.Agent.MemoryCompiler.Enabled
}

type ContextCompressionConfig struct {
	Policy         string `toml:"policy"`
	ThresholdBytes int    `toml:"threshold_bytes"`
	MaxBytes       int    `toml:"max_bytes"`
}

const (
	DefaultContextCompressionPolicy         = "auto"
	DefaultContextCompressionThresholdBytes = 8 * 1024
	DefaultContextCompressionMaxBytes       = 4 * 1024
)

func (c ContextCompressionConfig) EffectivePolicy() string {
	return contextCompressionPolicy(c.Policy)
}

func (c ContextCompressionConfig) EffectiveThresholdBytes() int {
	if c.ThresholdBytes > 0 {
		return c.ThresholdBytes
	}
	return DefaultContextCompressionThresholdBytes
}

func (c ContextCompressionConfig) EffectiveMaxBytes() int {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return DefaultContextCompressionMaxBytes
}

func contextCompressionPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "off", "aggressive":
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return DefaultContextCompressionPolicy
	}
}

// ProviderEntry declares a model provider instance. ContextWindow is the model's
// token budget; the harness compacts older history as a turn's prompt approaches
// it (see agent compaction). 0 disables compaction for the instance.
type ProviderEntry struct {
	Name               string                       `toml:"name"`
	Kind               string                       `toml:"kind"`
	BaseURL            string                       `toml:"base_url"`
	ChatURL            string                       `toml:"chat_url"`
	Model              string                       `toml:"model"`      // a single model (back-compat)
	Models             []string                     `toml:"models"`     // a vendor's model list (one base_url/key, many models)
	ModelsURL          string                       `toml:"models_url"` // auto-fetch models from this URL on startup
	Default            string                       `toml:"default"`    // default model when Models is set (else Models[0])
	APIKeyEnv          string                       `toml:"api_key_env"`
	AuthType           string                       `toml:"auth_type"`            // api_key (default), bearer, or workload_identity
	AuthTokenEnv       string                       `toml:"auth_token_env"`       // bearer/access-token env var; API key auth falls back to api_key_env
	AuthHeader         string                       `toml:"auth_header"`          // optional override; defaults to provider-specific header
	AuthScheme         string                       `toml:"auth_scheme"`          // optional override; bearer modes default to Bearer
	IdentityEnv        string                       `toml:"identity_env"`         // WIF OIDC/JWT assertion env var
	IdentityFile       string                       `toml:"identity_file"`        // WIF OIDC/JWT assertion file
	IdentityProviderID string                       `toml:"identity_provider_id"` // OpenAI WIF provider ID
	SubjectTokenType   string                       `toml:"subject_token_type"`   // OpenAI WIF subject token type; defaults to JWT
	TokenURL           string                       `toml:"token_url"`            // optional WIF token endpoint override
	FederationID       string                       `toml:"federation_rule_id"`
	Organization       string                       `toml:"organization_id"`
	ServiceAcctID      string                       `toml:"service_account_id"`
	WorkspaceID        string                       `toml:"workspace_id"`
	BalanceURL         string                       `toml:"balance_url"` // optional; a provider-specific wallet-balance endpoint (DeepSeek: https://api.deepseek.com/user/balance). Empty = no balance readout.
	ContextWindow      int                          `toml:"context_window"`
	Price              *provider.Pricing            `toml:"price"`
	Prices             map[string]*provider.Pricing `toml:"prices"`
	// Thinking / Effort are provider-kind-specific knobs forwarded to the provider
	// via Config.Extra. The anthropic provider reads Thinking="adaptive" to enable
	// extended thinking and Effort ("low".."max") to tune depth. The
	// openai-compatible provider forwards Effort as reasoning_effort for
	// thinking-capable models; DeepSeek accepts high|max.
	// Empty = provider default.
	Thinking string `toml:"thinking"`
	Effort   string `toml:"effort"`
	// Vision marks the model as accepting image input. When set, images the user
	// attaches are embedded in the request (image_url for openai-kind, base64
	// blocks for anthropic). Off by default: text-only models 400 on image input,
	// and image tokens are heavy — gating keeps text-only flows cheap (the prompt
	// prefix is byte-identical with no image, so the cache is unaffected either way).
	Vision bool `toml:"vision"`
	// VisionModels narrows image input support to specific models in a multi-model
	// provider. This lets one provider expose both text-only and multimodal chat
	// models without enabling image payloads for every model.
	VisionModels []string `toml:"vision_models"`
	// VisionDetail sets the openai image_url detail hint (low|high); empty = auto
	// (the field is omitted). "low" caps an image to a fixed ~85 tokens for cheap
	// coarse reads; ignored by providers without the knob (e.g. anthropic).
	VisionDetail string `toml:"vision_detail"`
	// ReasoningProtocol selects the request shape for OpenAI-compatible reasoning
	// models. Empty/auto uses the model capability registry plus endpoint
	// heuristics; none disables automatic reasoning controls for this provider.
	ReasoningProtocol string `toml:"reasoning_protocol"`
	// WireAPI selects the OpenAI transport wire. Empty/chat uses
	// /chat/completions; responses uses /responses for GPT-5 Codex/frontier
	// models that no longer support the chat endpoint.
	WireAPI string `toml:"wire_api"`
	// SupportedEfforts lists the /effort levels this provider/model exposes.
	// When non-empty, it overrides the built-in defaults derived from
	// Kind/BaseURL and makes /effort configurable. "auto" is the implicit
	// prefix — always accepted. DefaultEffort resolves it; omit DefaultEffort
	// (or set one outside this list) to fall back to SupportedEfforts[0].
	SupportedEfforts []string `toml:"supported_efforts"`

	resolvedAPIKey string
	resolvedSource CredentialSource
	// DefaultEffort is the /effort level used when the user picks "auto" or
	// has not set Effort. Ignored when SupportedEfforts is empty.
	DefaultEffort string `toml:"default_effort"`
	// NoProxy reaches this provider's base_url directly, never through the proxy.
	// For China-only endpoints a foreign-exit proxy resets the TLS handshake (#2803).
	NoProxy bool `toml:"no_proxy"`
}

// ModelList returns the models this provider exposes: the explicit `models` list,
// or the single `model` as a one-element list (back-compat). Empty if neither set.
func (e *ProviderEntry) ModelList() []string {
	if len(e.Models) > 0 {
		return e.Models
	}
	if e.Model != "" {
		return []string{e.Model}
	}
	return nil
}

// IsLikelyChatModel reports whether a model ID looks like a chat/completion
// model rather than a specialised audio/vision/embedding model. It applies a
// conservative name-based heuristic — the OpenAI-compatible /models API does
// not return capability/modality metadata, so this is the most reliable
// fallback until providers add such fields.
//
// The heuristic works in two passes:
//  1. Multi-word substring check for compound terms that span separators
//     (e.g. "text-embedding", "text-to-speech").
//  2. Token-level check: the model ID is split on common separators (- _ . / :)
//     and each token is compared against a set of known non-chat keywords.
//
// "voice" is intentionally absent from the non-chat set because it is too
// broad — legitimate future chat models may include it in their name.
func IsLikelyChatModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	lower := strings.ToLower(model)

	// Pass 1: compound terms that span separator boundaries.
	var compoundNonChat = []string{
		"text-embedding", "text-to-speech", "speech-to-text",
	}
	for _, c := range compoundNonChat {
		if strings.Contains(lower, c) {
			return false
		}
	}

	// Pass 2: token-level check.
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
	})
	var nonChatTokens = map[string]bool{
		"asr": true, "stt": true, "tts": true,
		"whisper": true, "embedding": true,
		"moderation": true, "rerank": true, "dall": true,
		"transcription": true,
	}
	for _, tok := range tokens {
		if nonChatTokens[tok] {
			return false
		}
	}
	return true
}

// ChatModelList returns ModelList filtered to likely chat/completion models.
// Non-chat models (TTS, STT, ASR, embedding, etc.) are excluded so they do
// not appear in the chat model picker. Use ModelList() only when the full
// raw provider model list is needed, such as config serialization, provider
// diagnostics, or model-fetch editing.
func (e *ProviderEntry) ChatModelList() []string {
	raw := e.ModelList()
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if IsLikelyChatModel(m) {
			out = append(out, m)
		}
	}
	return out
}

// DefaultModel returns the provider's default model: the explicit `default`, else
// the first of ModelList.
func (e *ProviderEntry) DefaultModel() string {
	if e.Default != "" {
		return e.Default
	}
	if l := e.ModelList(); len(l) > 0 {
		return l[0]
	}
	return ""
}

// HasModel reports whether m is one of the provider's models.
func (e *ProviderEntry) HasModel(m string) bool {
	for _, x := range e.ModelList() {
		if x == m {
			return true
		}
	}
	return false
}

// PriceForModel returns the configured per-1M-token price for model. Per-model
// prices win; the legacy provider-wide price is a fallback for older configs.
func (e *ProviderEntry) PriceForModel(model string) *provider.Pricing {
	if e == nil {
		return nil
	}
	if e.Prices != nil {
		if p := e.Prices[strings.TrimSpace(model)]; p != nil {
			return clonePricing(p)
		}
	}
	return clonePricing(e.Price)
}

func (e *ProviderEntry) applyModelPrice() {
	if e == nil {
		return
	}
	e.Price = e.PriceForModel(e.Model)
}

func clonePricing(p *provider.Pricing) *provider.Pricing {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// ToolsConfig selects which built-in tools are enabled. Empty means all of them.
type ToolsConfig struct {
	Enabled               []string             `toml:"enabled"`
	BashTimeoutSeconds    *int                 `toml:"bash_timeout_seconds"`
	MCPCallTimeoutSeconds *int                 `toml:"mcp_call_timeout_seconds"`
	BackgroundJobs        BackgroundJobsConfig `toml:"background_jobs"`
	Search                SearchConfig         `toml:"search"`
	Shell                 ShellConfig          `toml:"shell"`
}

const (
	defaultBashTimeoutSeconds             = 120
	defaultMCPCallTimeoutSeconds          = 300
	defaultBackgroundJobStalledWarningSec = 900
	maxBackgroundJobStalledWarningSec     = 86400
)

// BashTimeoutSeconds returns the foreground bash timeout in seconds. An omitted
// config keeps the historical 120s safety cap, explicit 0 disables the
// tool-local cap, and positive values set a custom cap. Negative values fall
// back to the default so a typo cannot silently remove the safety net.
func (c *Config) BashTimeoutSeconds() int {
	if c.Tools.BashTimeoutSeconds == nil || *c.Tools.BashTimeoutSeconds < 0 {
		return defaultBashTimeoutSeconds
	}
	return *c.Tools.BashTimeoutSeconds
}

// MCPCallTimeoutSeconds returns the default MCP JSON-RPC call timeout in
// seconds. Omitted, zero, and negative values keep the built-in safety cap so a
// hung MCP server cannot block a turn indefinitely.
func (c *Config) MCPCallTimeoutSeconds() int {
	if c.Tools.MCPCallTimeoutSeconds == nil || *c.Tools.MCPCallTimeoutSeconds <= 0 {
		return defaultMCPCallTimeoutSeconds
	}
	return *c.Tools.MCPCallTimeoutSeconds
}

// BackgroundJobsConfig tunes parent-created background jobs.
type BackgroundJobsConfig struct {
	StalledWarningSeconds *int `toml:"stalled_warning_seconds"`
}

// BackgroundJobStalledWarningSeconds returns the stalled warning threshold in
// seconds. Omitted/negative values keep the default, explicit 0 disables the
// notice, and oversized values clamp to one day so a typo cannot become
// effectively invisible.
func (c *Config) BackgroundJobStalledWarningSeconds() int {
	if c.Tools.BackgroundJobs.StalledWarningSeconds == nil || *c.Tools.BackgroundJobs.StalledWarningSeconds < 0 {
		return defaultBackgroundJobStalledWarningSec
	}
	if *c.Tools.BackgroundJobs.StalledWarningSeconds > maxBackgroundJobStalledWarningSec {
		return maxBackgroundJobStalledWarningSec
	}
	return *c.Tools.BackgroundJobs.StalledWarningSeconds
}

// SearchConfig tunes the grep tool's engine. Engine is "auto" (default — use
// ripgrep when it's on PATH, else the native Go scanner), "native" (always Go),
// or "rg" (require ripgrep; warn at startup and fall back to native if absent).
// RgPath optionally points at a specific ripgrep binary instead of a PATH lookup.
type SearchConfig struct {
	Engine string `toml:"engine"`
	RgPath string `toml:"rg_path"`
}

// ShellConfig chooses the interpreter the bash tool runs commands under. Prefer
// is "auto" (default — real bash when present, else PowerShell on Windows),
// "bash", or "powershell"/"pwsh" (force it; warn at startup and fall back to
// auto if absent). Path optionally points at a specific shell executable.
type ShellConfig struct {
	Prefer string `toml:"prefer"`
	Path   string `toml:"path"`
}

// PermissionsConfig declares the per-call permission policy (see
// internal/permission). Mode is the fallback decision for writer tools when no
// rule matches ("ask" | "allow" | "deny"; default "ask"); read-only tools always
// fall back to allow. Allow/Ask/Deny are rule lists of the form "ToolName" or
// "ToolName(glob)". Precedence: deny > ask > allow > fallback.
type PermissionsConfig struct {
	Mode  string   `toml:"mode"`
	Allow []string `toml:"allow"`
	Ask   []string `toml:"ask"`
	Deny  []string `toml:"deny"`
}

// PluginEntry declares an external MCP server. Type selects the transport:
// "stdio" (default) launches Command/Args/Env as a subprocess; "http"
// (a.k.a. streamable-http) and "sse" connect to a remote URL with optional
// static Headers. String fields support ${VAR} / ${VAR:-default} expansion so
// secrets (bearer tokens, keys) come from the environment, not the file. The
// fields mirror Claude Code's mcpServers spec, so entries can come from either
// maddog.toml's [[plugins]] or a project-root .mcp.json (see loadMCPJSON).
type PluginEntry struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" (default) | "http" | "sse"
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
	// CallTimeoutSeconds overrides the default per-call deadline for this MCP
	// server. Zero falls back to [tools].mcp_call_timeout_seconds.
	CallTimeoutSeconds int `toml:"call_timeout_seconds"`
	// ToolTimeoutSeconds overrides the per-call deadline for raw MCP tool names
	// from this server. Keys are server-local tool names, not model-visible
	// mcp__server__tool names.
	ToolTimeoutSeconds map[string]int `toml:"tool_timeout_seconds"`
	// TrustedReadOnlyTools names raw MCP tool names that Maddog should treat as
	// trusted read-only for planner / plan-mode / read-only research surfaces.
	// Use this only for tools whose semantics are known to be side-effect free.
	TrustedReadOnlyTools []string `toml:"trusted_read_only_tools"`
	// AutoStart controls whether the server connects during session startup.
	// Nil preserves historical behavior: configured servers start automatically.
	AutoStart *bool `toml:"auto_start"`
	// Tier is a legacy compatibility field. New config rendering omits it; enabled
	// MCP servers connect automatically in the background unless auto_start=false.
	// Historical values are accepted for old files:
	//   "eager"      — blocks startup until the handshake completes; required for
	//                  servers whose tools the system prompt depends on.
	//   "lazy"       — legacy alias for background.
	//   "background" — placeholder + spawn fired at boot but not waited on;
	//                  swap happens once the spawn finishes.
	// Empty defaults to "background" so enabled MCPs connect automatically
	// without blocking chat. Unknown non-empty values fall back to "background".
	Tier         string `toml:"tier"`
	expansionEnv map[string]string
}

func (e PluginEntry) ShouldAutoStart() bool {
	return e.AutoStart == nil || *e.AutoStart
}

// ResolvedTier returns the normalized tier ("eager"|"background") with the
// project default applied. Legacy lazy and unknown values fall back to
// background so enabled MCPs are available without manual connection.
func (e PluginEntry) ResolvedTier() string {
	return resolvedMCPTier(e.Tier)
}

func resolvedMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background", "lazy":
		return "background"
	case "":
		return "background"
	default:
		return "background"
	}
}

func (c *Config) AutoStartPlugins() []PluginEntry {
	out := make([]PluginEntry, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		if p.ShouldAutoStart() {
			out = append(out, p)
		}
	}
	return out
}

// DefaultSystemPrompt is used when config provides none.
const DefaultSystemPrompt = `You are Maddog, a coding agent focused on executing code tasks.
Use the provided tools to read and write files and run shell commands.
Principles: understand the request before acting; verify with tools instead of
guessing; keep changes minimal and correct; briefly summarize what you did.
For multi-step work, track progress with the todo_write tool: lay out the steps,
keep exactly one in_progress, and flip each to completed as you finish it — update
the list as you go, not just at the end.
In plan mode the harness blocks writer tools: do read-only research, then write a
concise plan as your reply and stop. The user is asked to approve before anything
is changed; once approved, work through the steps, updating the task list as you go.`

// UserDecisionPolicy is appended to every system prompt, including user-custom
// prompts, so custom personas cannot accidentally remove the `ask` UI contract.
const UserDecisionPolicy = `User-owned choices: when a real decision belongs to the user — scope, approach, library, risk, manual validation, or any ambiguous or consequential path — and there is no obvious safe default, call the ask tool with 2-4 concrete options so the UI shows a choice. Do not ask in prose, infer a choice from silence, or continue by choosing for the user; do not choose for the user. Tool-approval bypass modes do not answer ask questions or approve plans. If no interactive user is available, the ask tool returns a model-assumption fallback; state that assumption and choose the safest reversible path.`

// LanguagePolicy is the auto fallback appended to the system prompt when no
// concrete UI language is resolved. It is static English text, so it stays part
// of the cache-stable prefix and avoids per-turn language injection.
const LanguagePolicy = `Reply in the same language the user is using in their most recent message: ` +
	`if they write in Chinese answer in Chinese, in English answer in English, and switch ` +
	`whenever they switch. Let this also guide the language you think in. Always keep code, ` +
	`identifiers, file paths, shell commands, and technical terms in their original form — never translate them.`

// Default returns the built-in default configuration.
func Default() *Config {
	return &Config{
		ConfigVersion:    3,
		DefaultModel:     "deepseek-flash",
		CredentialsStore: CredentialsStoreAuto,
		UI:               UIConfig{Theme: "auto"},
		Notifications: NotificationsConfig{
			Enabled:         false,
			TurnDone:        true,
			ApprovalRequest: true,
			AskRequest:      true,
		},
		Agent: AgentConfig{
			SystemPrompt: DefaultSystemPrompt,
			// 0 = no step cap: the agent loops until the model gives a final answer,
			// the user cancels, or the provider errors. Context stays bounded by
			// compaction, not by a round count. Set a positive agent.max_steps only
			// if you want a hard guard against runaway.
			MaxSteps:                  0,
			AutoPlan:                  "off",
			UpgradeEnabled:            true,
			UpgradeThreshold:          3,
			FrontierBudget:            500000,
			AdvisorMaxUsesPerTurn:     1,
			AdvisorMaxUsesPerSession:  10,
			AdvisorMaxContextMessages: 12,
			AdvisorMaxContextChars:    12000,
			AdvisorNativeEnabled:      false,
			AdvisorNativeMaxTokens:    1024,
			SoftCompactRatio:          0.5,
			CompactRatio:              0.8,
			CompactForceRatio:         0.9,
			ContextCompression: ContextCompressionConfig{
				Policy:         DefaultContextCompressionPolicy,
				ThresholdBytes: DefaultContextCompressionThresholdBytes,
				MaxBytes:       DefaultContextCompressionMaxBytes,
			},
		},
		Skills: SkillsConfig{
			RuntimeOrchestration: true,
			DynamicSkills:        false,
		},
		// Mode "ask" with no rules keeps `maddog run` autonomous (no TTY → ask
		// resolves to allow) while `maddog chat` prompts before writers. Users add
		// deny/allow rules to harden or quiet specific tools.
		Permissions: PermissionsConfig{Mode: "ask"},
		// Sandbox on by default: bash is jailed (macOS), network allowed so
		// builds/downloads work. Set bash = "off" to disable. Network=true here
		// so an absent [sandbox] in a user's file keeps egress (zero value would
		// wrongly deny it).
		Sandbox: SandboxConfig{Bash: "enforce", Network: true},
		// CodeGraph code-intelligence defaults on for both first-run and upgrades.
		// AutoInstall fetches the runtime into the cache when enabled and missing.
		Codegraph: CodegraphConfig{Enabled: true, AutoInstall: true},
		// Time is dependency-free and bundled, so expose it by default. Context7
		// can invoke a package runner and remains opt-in.
		BuiltInMCP: BuiltInMCPConfig{TimeEnabled: true},
		BuiltInMCPUpdates: BuiltInMCPUpdatesConfig{
			Mode:          BuiltInMCPUpdateModeNotify,
			CheckInterval: defaultBuiltInMCPUpdateInterval.String(),
		},
		// LSP tools on by default, but dormant until a language server is on PATH;
		// a missing server yields an install hint rather than an error.
		LSP:     LSPConfig{Enabled: true},
		Network: NetworkConfig{ProxyMode: netclient.ModeAuto},
		Bot: BotConfig{
			ToolApprovalMode: "ask",
			MaxSteps:         25,
			DebounceMs:       1500,
			Allowlist:        BotAllowlist{Enabled: true},
			QQ:               QQBotConfig{AppSecretEnv: "QQ_BOT_APP_SECRET"},
			Feishu:           FeishuBotConfig{Domain: "feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET", Mode: "webhook", WebhookPort: 8080, RequireMention: true},
			Weixin:           WeixinBotConfig{AccountID: "default", TokenEnv: "WEIXIN_BOT_TOKEN", APIBase: "https://ilinkai.weixin.qq.com"},
		},
		Providers: []ProviderEntry{
			{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: deepSeekV4FlashPrice()},
			{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: deepSeekV4ProPrice()},
		},
	}
}

/*
// Legacy loader implementation retained only for merge recovery context.
// The active implementation lives in load.go.
// Load builds the configuration: defaults, then user config, then project
// config, then MCP servers from Claude Code's .mcp.json. A .env in the working
// directory is loaded first so api_key_env can resolve.
func Load() (*Config, error) {
	return LoadForRoot(".")
}

// LoadForRoot builds the configuration with project files resolved from root
// instead of the current working directory. When root is "" or ".", it behaves
// like Load(). This is the workspace-aware entry point: desktop tabs use it so
// each project's maddog.toml + .env + .mcp.json are resolved independently
// without changing the process cwd.
func LoadForRoot(root string) (*Config, error) {
	root = resolveRoot(root)
	loadDotEnvForRoot(root)
	cfg := Default()

	var tomlSources []string
	if uc := userConfigPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
	}
	tomlSources = append(tomlSources, projectConfigSourceForRoot(root))
	for _, path := range tomlSources {
		if _, err := os.Stat(path); err == nil {
			if err := migrateLegacyMCPTiersFile(path); err != nil {
				slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
			}
		}
		if err := mergeFile(cfg, path); err != nil {
			return nil, err
		}
	}
	// toml.DecodeFile replaces [[plugins]] wholesale, so cfg.Plugins now holds
	// only the last file's. Re-merge by name across all sources (later wins) so a
	// project maddog.toml doesn't drop the global config's MCP servers.
	plugins, err := mergeTOMLPlugins(tomlSources)
	if err != nil {
		return nil, err
	}
	cfg.Plugins = plugins

	// Claude Code's .mcp.json (project root) is read last and merged into
	// [[plugins]], so a server configured for Claude works here unchanged.
	// maddog.toml wins on a name collision (see mergeMCPJSON).
	mcpFile := mcpJSONFile
	if root != "." {
		mcpFile = filepath.Join(root, mcpJSONFile)
	}
	entries, err := loadMCPJSON(mcpFile)
	if err != nil {
		return nil, err
	}
	cfg.mergeMCPJSON(entries)

	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	return cfg, nil
}

// backfillDeepSeekPro restores deepseek-pro for configs the pre-fix setup wizard
// wrote with only deepseek-v4-flash: a keyless /models probe used to drop the Pro
// SKU, leaving users unable to switch to it. In-memory only — the user's file is
// untouched. Narrowly scoped to the official DeepSeek endpoint (which is known to
// serve pro) so a custom flash-only deployment isn't given an entry that 404s.
func backfillDeepSeekPro(c *Config) {
	const flashModel, proModel = "deepseek-v4-flash", "deepseek-v4-pro"
	var flash *ProviderEntry
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "deepseek-pro" {
			return
		}
		for _, m := range p.ModelList() {
			switch m {
			case proModel:
				return // pro already reachable
			case flashModel:
				if strings.Contains(p.BaseURL, "api.deepseek.com") {
					flash = p
				}
			}
		}
	}
	if flash == nil {
		return
	}
	for _, bp := range Default().Providers {
		if bp.Name == "deepseek-pro" {
			bp.APIKeyEnv = flash.APIKeyEnv
			c.Providers = append(c.Providers, bp)
			return
		}
	}
}

func resolveRoot(root string) string {
	if root == "" || root == "." {
		return "."
	}
	return filepath.Clean(root)
}

// normalizeLegacyEffort migrates the retired DeepSeek effort="off" (the old
// /thinking off that disabled thinking) to the provider default, so a config
// written by an older version keeps loading instead of erroring on a value the
// provider no longer accepts.
func normalizeLegacyEffort(c *Config) {
	for i := range c.Providers {
		if strings.EqualFold(strings.TrimSpace(c.Providers[i].Effort), "off") {
			c.Providers[i].Effort = ""
		}
	}
}

// mergeTOMLPlugins merges [[plugins]] across TOML sources by name (later source wins).
func mergeTOMLPlugins(paths []string) ([]PluginEntry, error) {
	var merged []PluginEntry
	index := map[string]int{}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		for _, p := range f.Plugins {
			p, _ = NormalizePluginCommandLine(p)
			if i, ok := index[p.Name]; ok {
				merged[i] = p
				continue
			}
			index[p.Name] = len(merged)
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// LoadForEdit returns a config to seed the `maddog setup` wizard when reconfiguring:
// the built-in defaults with the file at path (if present) decoded on top, so a
// reconfigure preserves the user's existing providers and agent settings instead
// of resetting to defaults. .env is loaded so api_key_env resolution works while
// the wizard decides which keys are still missing.
func LoadForEdit(path string) *Config {
	loadDotEnv()
	cfg := Default()
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
		}
	}
	if err := mergeFile(cfg, path); err != nil {
		slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	}
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeEffortConfig(cfg)
	return cfg
}

// mergeFile decodes a TOML file onto cfg if it exists. An absent file is not an error.
func mergeFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

// normalizeLegacyMCPTiers keeps loaded legacy config files on the new product
// behavior: enabled MCP servers connect in the background by default, and the
// retired per-server startup tier is no longer a user-facing setting.
func normalizeLegacyMCPTiers(c *Config) {
	if c == nil {
		return
	}
	c.Codegraph.Tier = ""
	for i := range c.Plugins {
		c.Plugins[i].Tier = ""
	}
}

func migrateLegacyMCPTiersFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, changed := stripLegacyMCPTierLines(string(raw))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(next), info.Mode().Perm())
}

func stripLegacyMCPTierLines(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	section := ""
	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if header := tomlSectionHeader(line); header != "" {
			section = header
		}
		if (section == "codegraph" || section == "plugins") && isTOMLKeyAssignment(line, "tier") {
			changed = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed
}

func tomlSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	if i := strings.Index(trimmed, "#"); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	switch trimmed {
	case "[codegraph]":
		return "codegraph"
	case "[[plugins]]":
		return "plugins"
	default:
		return "other"
	}
}

func isTOMLKeyAssignment(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
	return strings.HasPrefix(rest, "=")
}

// normalizeLegacyProviderModels repairs provider entries written by older
// desktop builds that carried the official provider name/endpoint but omitted the
// model field. The repair is intentionally narrow: valid user-provided model
// lists are left untouched, while known official aliases get the model implied by
// their preset name so model pickers and provider validation have an option.
func normalizeLegacyProviderModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if providerHasAnyModel(*p) {
			continue
		}
		if model := legacyOfficialProviderModel(p.Name); model != "" {
			p.Model = model
		}
	}
}

func legacyOfficialProviderModel(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash":
		return "deepseek-v4-flash"
	case "deepseek-pro":
		return "deepseek-v4-pro"
	case "mimo-api", "mimo-pro":
		return "mimo-v2.5-pro"
	case "mimo-flash":
		return "mimo-v2.5"
	default:
		return ""
	}
}

func normalizeDesktopOfficialProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	next := make([]string, 0, len(c.Desktop.ProviderAccess))
	includeMimoFlash := false
	for _, name := range c.Desktop.ProviderAccess {
		if strings.TrimSpace(name) == "mimo-flash" {
			includeMimoFlash = true
		}
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	c.Desktop.ProviderAccess = next
	if seen["deepseek"] {
		ensureDeepSeekOfficialProvider(c)
	}
	if seen["mimo-api"] {
		ensureMimoAPIProvider(c)
	}
	if seen["mimo-token-plan"] {
		ensureMimoTokenPlanProvider(c, includeMimoFlash)
	}
	retargetDesktopOfficialRefs(c, seen)
}

// NormalizeLegacyDesktopProviderAccess seeds the desktop provider-access list
// for configs written before Settings tracked explicit provider access. Callers
// should only use this when they know the TOML did not declare provider_access;
// an explicit empty list means the user removed all access entries.
func NormalizeLegacyDesktopProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) > 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	var access []string
	add := func(name string) {
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		access = append(access, name)
	}
	addRef := func(ref string) {
		if entry, ok := c.ResolveModel(ref); ok {
			if !entry.Configured() {
				return
			}
			add(entry.Name)
		}
	}
	addRef(c.DefaultModel)
	addRef(c.Agent.PlannerModel)
	addRef(c.Agent.SubagentModel)
	addRef(c.Agent.AutoPlanClassifier)
	for _, ref := range c.Agent.SubagentModels {
		addRef(ref)
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Configured() {
			add(p.Name)
		}
	}
	if len(access) == 0 {
		return
	}
	c.Desktop.ProviderAccess = access
	normalizeDesktopOfficialProviderAccess(c)
}

func canonicalDesktopOfficialProviderName(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash", "deepseek-pro":
		return "deepseek"
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		return "mimo-api"
	case "mimo-pro", "mimo-flash":
		return "mimo-token-plan"
	default:
		return strings.TrimSpace(name)
	}
}

// CanonicalDesktopOfficialProviderName returns the Settings Center provider ID
// for built-in official provider aliases.
func CanonicalDesktopOfficialProviderName(name string) string {
	return canonicalDesktopOfficialProviderName(name)
}

func desktopProviderAccessMap(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = canonicalDesktopOfficialProviderName(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ensureDeepSeekOfficialProvider(c *Config) {
	if _, ok := c.Provider("deepseek"); ok {
		return
	}
	entry := ProviderEntry{
		Name:          "deepseek",
		Kind:          "openai",
		BaseURL:       "https://api.deepseek.com",
		Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:       "deepseek-v4-flash",
		APIKeyEnv:     "DEEPSEEK_API_KEY",
		BalanceURL:    "https://api.deepseek.com/user/balance",
		ContextWindow: 1_000_000,
	}
	if old, ok := c.Provider("deepseek-flash"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists([]string{"deepseek-v4-flash", "deepseek-v4-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "deepseek-v4-flash")
	}
	c.Providers = append(c.Providers, entry)
}

func ensureMimoAPIProvider(c *Config) {
	models := []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
	if p, ok := c.Provider("mimo-api"); ok {
		if isOfficialMimoAPIProvider(p) {
			mergeCuratedModelsIntoProvider(p, models, "mimo-v2.5-pro")
		}
		return
	}
	c.Providers = append(c.Providers, ProviderEntry{
		Name:          "mimo-api",
		Kind:          "openai",
		BaseURL:       "https://api.xiaomimimo.com/v1",
		Models:        models,
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	})
}

func ensureMimoTokenPlanProvider(c *Config, includeMimoFlash bool) {
	models := []string{"mimo-v2.5-pro", "mimo-v2.5"}
	if p, ok := c.Provider("mimo-token-plan"); ok {
		if isOfficialMimoTokenPlanProvider(p) {
			mergeCuratedModelsIntoProvider(p, models, "mimo-v2.5-pro")
			clearMixedMimoTokenPlanPrice(p)
		}
		return
	}
	entry := ProviderEntry{
		Name:          "mimo-token-plan",
		Kind:          "openai",
		BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
		Models:        models,
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	}
	if old, ok := c.Provider("mimo-pro"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists(models, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "mimo-v2.5-pro")
	}
	if old, ok := c.Provider("mimo-flash"); includeMimoFlash && ok {
		if !providerHasAnyModel(entry) {
			entry = officialProviderFromLegacy(entry, old)
		}
		entry.Models = mergeModelLists(entry.Models, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, entry.Default)
	}
	clearMixedMimoTokenPlanPrice(&entry)
	c.Providers = append(c.Providers, entry)
}

func isOfficialMimoAPIProvider(e *ProviderEntry) bool {
	return isOpenAIProviderKind(e) && officialMimoHost(e.BaseURL) == "api.xiaomimimo.com"
}

func isOfficialMimoTokenPlanProvider(e *ProviderEntry) bool {
	return isOpenAIProviderKind(e) && officialMimoHost(e.BaseURL) == "token-plan-cn.xiaomimimo.com"
}

func isOpenAIProviderKind(e *ProviderEntry) bool {
	return e != nil && strings.EqualFold(strings.TrimSpace(e.Kind), "openai")
}

func mergeCuratedModelsIntoProvider(e *ProviderEntry, models []string, fallback string) {
	currentDefault := e.Default
	if strings.TrimSpace(currentDefault) == "" {
		currentDefault = e.Model
	}
	e.Models = mergeModelLists(models, e.ModelList())
	e.Default = firstKnownModel(currentDefault, e.Models, fallback)
}

func clearMixedMimoTokenPlanPrice(e *ProviderEntry) {
	if e != nil && e.HasModel("mimo-v2.5-pro") && e.HasModel("mimo-v2.5") {
		e.Price = nil
	}
}

func officialProviderFromLegacy(entry ProviderEntry, old *ProviderEntry) ProviderEntry {
	entry.Kind = old.Kind
	entry.BaseURL = old.BaseURL
	entry.ChatURL = old.ChatURL
	entry.ModelsURL = old.ModelsURL
	entry.APIKeyEnv = old.APIKeyEnv
	entry.AuthType = old.AuthType
	entry.AuthTokenEnv = old.AuthTokenEnv
	entry.AuthHeader = old.AuthHeader
	entry.AuthScheme = old.AuthScheme
	entry.IdentityEnv = old.IdentityEnv
	entry.IdentityFile = old.IdentityFile
	entry.FederationID = old.FederationID
	entry.Organization = old.Organization
	entry.ServiceAcctID = old.ServiceAcctID
	entry.WorkspaceID = old.WorkspaceID
	entry.BalanceURL = old.BalanceURL
	entry.ContextWindow = old.ContextWindow
	entry.Price = old.Price
	entry.Thinking = old.Thinking
	entry.Effort = old.Effort
	entry.ReasoningProtocol = old.ReasoningProtocol
	entry.SupportedEfforts = append([]string(nil), old.SupportedEfforts...)
	entry.DefaultEffort = old.DefaultEffort
	entry.NoProxy = old.NoProxy
	return entry
}

func mergeModelLists(primary, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, model := range list {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func firstKnownModel(current string, models []string, fallback string) string {
	current = strings.TrimSpace(current)
	for _, model := range models {
		if model == current {
			return current
		}
	}
	for _, model := range models {
		if model == fallback {
			return fallback
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func retargetDesktopOfficialRefs(c *Config, access map[string]bool) {
	c.DefaultModel = retargetDesktopOfficialRef(c.DefaultModel, access)
	c.Agent.PlannerModel = retargetDesktopOfficialRef(c.Agent.PlannerModel, access)
	c.Agent.SubagentModel = retargetDesktopOfficialRef(c.Agent.SubagentModel, access)
	c.Agent.AutoPlanClassifier = retargetDesktopOfficialRef(c.Agent.AutoPlanClassifier, access)
	for skill, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[skill] = retargetDesktopOfficialRef(ref, access)
	}
}

func retargetDesktopOfficialRef(ref string, access map[string]bool) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	provider, model, hasModel := strings.Cut(ref, "/")
	switch provider {
	case "deepseek-flash":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-flash"
		}
		return "deepseek/" + model
	case "deepseek-pro":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-pro"
		}
		return "deepseek/" + model
	case "mimo-pro":
		if !access["mimo-token-plan"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		return "mimo-token-plan/" + model
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		if !access["mimo-api"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		return "mimo-api/" + model
	case "mimo-flash":
		if !access["mimo-token-plan"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5"
		}
		return "mimo-token-plan/" + model
	default:
		return ref
	}
}
*/

/*
// Legacy path helpers retained only for merge recovery context.
// The active implementations live in paths.go.
func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AppName, "config.toml")
}

// userConfigDisplayPath is userConfigPath collapsed to a ~-relative form for
// comments rendered into config.toml, so users see the real platform location.
func userConfigDisplayPath() string {
	p := userConfigPath()
	if p == "" {
		return "<os-config-dir>/" + AppName + "/config.toml"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

// UserConfigPath is the user-global config file (~/.config/maddog/config.toml),
// or "" when the user config dir can't be resolved.
func UserConfigPath() string { return userConfigPath() }

// UserCredentialsPath is the Maddog-owned global secrets file, usually beside
// config.toml in the user config dir (e.g. ~/.config/maddog/credentials). It
// holds KEY=value lines loaded into the environment by loadDotEnv. The setup
// wizard writes API keys here, deliberately NOT named .env: keys never land in a
// project's own .env (which can't be selectively gitignored), never get
// committed, and resolve from any working directory. If the platform config dir
// is unavailable, it falls back to ~/.maddog/credentials, then .maddog/credentials.
func UserCredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err == nil {
		return filepath.Join(dir, AppName, "credentials")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "."+AppName, "credentials")
	}
	return filepath.Join(ProjectConventionDir, "credentials")
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user config
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AppName, "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `maddog chat --continue` / `--resume` to find the recent ones. Empty
// if the user config dir can't be resolved — sessions then aren't saved.
func SessionDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AppName, "sessions")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <config root>/projects/<slug>/sessions. Empty when either the config
// root or workspaceRoot doesn't resolve.
func ProjectSessionDir(workspaceRoot string) string {
	base := MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

// WorkspaceSlug flattens an absolute workspace path into the directory name
// used under <config root>/projects.
func WorkspaceSlug(absPath string) string {
	return strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-").Replace(absPath)
}

// CacheDir is the per-user cache root for derived/regenerable artefacts: MCP
// handshake snapshots, plugin startup-latency telemetry. Lives beside the
// existing dirs (UserConfigDir/maddog/...) so the whole Maddog state tree
// shares one root the user can wipe in a single rm. Empty when the OS dir is
// unavailable — callers must tolerate that (caching is best-effort).
func CacheDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AppName, "cache")
}

// MemoryUserDir returns the Maddog user config root (…/maddog), under which
// the user-global MADDOG.md and the per-project auto-memory store live. Empty
// when the user config dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, AppName)
}

// ConventionDirs are the parent directories scanned for agent assets (skills,
// commands), in canonical-first order. .maddog is ours; .agents / .agent /
// .claude let users drop in assets authored for other agent tools without moving
// files. Shared so skills (internal/skill) and commands (CommandDirs) discover
// the same set. Note: hooks are NOT scanned across these — a .claude/settings.json
// uses a different hook schema that can't be parsed as ours, so hooks stay in
// .maddog/settings.json (see internal/hook).
var ConventionDirs = []string{ProjectConventionDir, ".agents", ".agent", ".claude"}

// conventionSubdirsAsc joins sub under each ConventionDir of base, in ascending
// priority (reverse of ConventionDirs) so the canonical .maddog ends up the
// highest-priority entry — command.Load lets a later directory win on a clash.
func conventionSubdirsAsc(base, sub string) []string {
	out := make([]string, 0, len(ConventionDirs))
	for i := len(ConventionDirs) - 1; i >= 0; i-- {
		out = append(out, filepath.Join(base, ConventionDirs[i], sub))
	}
	return out
}

// CommandDirs returns the directories scanned for custom slash commands, lowest
// priority first, so a later (more specific) directory overrides an earlier one
// on a name clash. Order: home-dir convention dirs (~/.claude/commands … ~/.maddog/commands),
// the XDG user dir, then the project's convention dirs (.claude/commands … .maddog/commands).
// Scanning the .claude /
// .agents / .agent dirs lets commands authored for other agent tools (same .md +
// frontmatter format) work here unchanged.
func CommandDirs() []string {
	return CommandDirsForRoot(".")
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global (home/XDG)
// dirs are unchanged — they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	root = resolveRoot(root)
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, conventionSubdirsAsc(home, "commands")...)
	}
	if dir, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(dir, AppName, "commands"))
	}
	dirs = append(dirs, conventionSubdirsAsc(root, "commands")...)
	return dirs
}
*/

// ProjectConfigPathForRoot returns Maddog's canonical project config path.
func ProjectConfigPathForRoot(root string) string {
	root = resolveRoot(root)
	if root == "." {
		return ProjectConfigFilename
	}
	return filepath.Join(root, ProjectConfigFilename)
}

func projectConfigSourceForRoot(root string) string { return ProjectConfigPathForRoot(root) }

/*
// Legacy source-path helpers retained only for merge recovery context.
// The active implementations live in paths.go.
// SourcePath returns the highest-priority config file that exists, or "" if none.
func SourcePath() string {
	return SourcePathForRoot(".")
}

// SourcePathForRoot returns the highest-priority config file that exists under
// root, or "" if none. Equivalent to SourcePath() when root is ".".
func SourcePathForRoot(root string) string {
	root = resolveRoot(root)
	if p := ProjectConfigPathForRoot(root); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if uc := userConfigPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	return ""
}
*/

// WriteFile writes the configuration to path as annotated TOML.
func (c *Config) WriteFile(path string) error {
	return fileutil.AtomicWriteFile(path, []byte(RenderTOMLForScope(c, renderScopeForPath(path))), configFilePerm(path))
}

// Provider returns the named provider entry.
func (c *Config) Provider(name string) (*ProviderEntry, bool) {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

// ResolveModel resolves a model reference to a provider entry whose Model is the
// selected model string (a copy, so the config's lists stay intact). It accepts:
//   - "provider/model" — that exact model under that provider;
//   - a provider name   — the provider's default model;
//   - a bare model name — the (first) provider that lists it.
//
// The returned entry is ready to build a provider from (NewProvider reads .Model),
// so a single "vendor with many models" entry yields one instance per model
// without duplicating base_url/api_key_env. Single-`model` entries still resolve
// by provider name, keeping older configs working unchanged.
func (c *Config) ResolveModel(ref string) (*ProviderEntry, bool) {
	if ref == "" {
		return nil, false
	}
	if access := desktopProviderAccessMap(c.Desktop.ProviderAccess); len(access) > 0 {
		ref = retargetDesktopOfficialRef(ref, access)
	}
	// "provider/model"
	if prov, model, ok := strings.Cut(ref, "/"); ok {
		if e, found := c.Provider(prov); found && e.HasModel(model) {
			cp := *e
			cp.Model = model
			cp.applyModelPrice()
			return &cp, true
		}
	}
	// a provider name → its default model
	if e, found := c.Provider(ref); found {
		cp := *e
		cp.Model = e.DefaultModel()
		cp.applyModelPrice()
		return &cp, true
	}
	// a bare model name → the provider that lists it
	for i := range c.Providers {
		if c.Providers[i].HasModel(ref) {
			cp := c.Providers[i]
			cp.Model = ref
			cp.applyModelPrice()
			return &cp, true
		}
	}
	return nil, false
}

// ResolveModelWithFallback resolves a model reference to the canonical
// "provider/model" form used by the desktop runtime. If ref is stale or empty,
// it tries the user's configured default_model before falling back to the first
// configured provider — so preference isn't overwritten by iteration order.
func (c *Config) ResolveModelWithFallback(ref string) (resolvedRef string, fallback bool, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if e, found := c.ResolveModel(ref); found {
			return e.Name + "/" + e.Model, false, true
		}
	}
	// Before falling back to the first configured provider (which may not be the
	// user's preferred choice), try the configured default_model.  Skip when ref
	// already WAS the DefaultModel (it already failed above, so retrying won't
	// help) or when the default provider has no API key configured.
	if ref != c.DefaultModel && c.DefaultModel != "" {
		if e, found := c.ResolveModel(c.DefaultModel); found && e.Configured() {
			return e.Name + "/" + e.Model, true, true
		}
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		// Skip providers with no models or no API key: falling back onto a keyless
		// provider just boots the tab onto something that fails on first use. Mirrors
		// the Configured() gate the provider-removal/selection paths already apply.
		if len(p.ModelList()) == 0 || !p.Configured() {
			continue
		}
		return p.Name + "/" + p.DefaultModel(), true, true
	}
	return "", false, false
}

// NormalizedAuthType reports the configured auth mode. Empty preserves the
// historical API-key behavior.
func (e *ProviderEntry) NormalizedAuthType() string {
	return provider.AuthConfig{Type: e.AuthType}.NormalizedType()
}

// AuthEnvName returns the credential env var used by the current auth mode.
func (e *ProviderEntry) AuthEnvName() string {
	switch e.NormalizedAuthType() {
	case provider.AuthTypeBearer:
		if strings.TrimSpace(e.AuthTokenEnv) != "" {
			return strings.TrimSpace(e.AuthTokenEnv)
		}
		return strings.TrimSpace(e.APIKeyEnv)
	case provider.AuthTypeWorkloadIdentity:
		if strings.TrimSpace(e.AuthTokenEnv) != "" {
			return strings.TrimSpace(e.AuthTokenEnv)
		}
		if strings.TrimSpace(e.IdentityEnv) != "" {
			return strings.TrimSpace(e.IdentityEnv)
		}
		return strings.TrimSpace(e.APIKeyEnv)
	default:
		return strings.TrimSpace(e.APIKeyEnv)
	}
}

// AuthToken resolves the entry's request token from env. API key auth reads
// api_key_env; bearer auth reads auth_token_env with api_key_env as a legacy
// fallback; workload identity reads a pre-minted access token when present.
func (e *ProviderEntry) AuthToken() string {
	env := e.AuthEnvName()
	if e.NormalizedAuthType() == provider.AuthTypeWorkloadIdentity {
		env = strings.TrimSpace(e.AuthTokenEnv)
	}
	if env == "" {
		return ""
	}
	return os.Getenv(env)
}

// IdentityToken resolves an OIDC/JWT assertion for Anthropic workload identity.
func (e *ProviderEntry) IdentityToken() string {
	if env := strings.TrimSpace(e.IdentityEnv); env != "" {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	if path := strings.TrimSpace(e.IdentityFile); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// AuthConfig returns the provider-layer authentication contract for this entry.
func (e *ProviderEntry) AuthConfig() provider.AuthConfig {
	return provider.AuthConfig{
		Type:          e.AuthType,
		Token:         e.AuthToken(),
		TokenEnv:      e.AuthEnvName(),
		HeaderName:    e.AuthHeader,
		HeaderScheme:  e.AuthScheme,
		IdentityToken: e.IdentityToken(),
		IdentityEnv:   strings.TrimSpace(e.IdentityEnv),
		Extra: map[string]string{
			"federation_rule_id":   strings.TrimSpace(e.FederationID),
			"organization_id":      strings.TrimSpace(e.Organization),
			"service_account_id":   strings.TrimSpace(e.ServiceAcctID),
			"workspace_id":         strings.TrimSpace(e.WorkspaceID),
			"identity_provider_id": strings.TrimSpace(e.IdentityProviderID),
			"subject_token_type":   strings.TrimSpace(e.SubjectTokenType),
			"token_url":            strings.TrimSpace(e.TokenURL),
		},
	}
}

// APIKey resolves the entry's API key from its api_key_env.
func (e *ProviderEntry) APIKey() string {
	if e == nil {
		return ""
	}
	if e.resolvedAPIKey != "" {
		return e.resolvedAPIKey
	}
	if e.APIKeyEnv == "" {
		return ""
	}
	value, _, ok := storedCredentialValue(e.APIKeyEnv)
	if !ok {
		return ""
	}
	return value
}

// Configured reports whether the provider's configured auth material is
// present — the same check Validate enforces, so pickers can filter on it.
func (e *ProviderEntry) Configured() bool {
	if e.AuthToken() != "" {
		return true
	}
	return e.NormalizedAuthType() == provider.AuthTypeWorkloadIdentity && e.IdentityToken() != ""
}

// ResolveSystemPrompt returns the system prompt, reading system_prompt_file if set.
func (c *Config) ResolveSystemPrompt() (string, error) {
	return c.ResolveSystemPromptForRoot(".")
}

// ResolveSystemPromptForRoot is like ResolveSystemPrompt but resolves a relative
// system_prompt_file against root. Desktop tabs pass their workspace root here so
// prompt files are project-scoped even when the process cwd is elsewhere.
func (c *Config) ResolveSystemPromptForRoot(root string) (string, error) {
	if c.Agent.SystemPromptFile != "" {
		path := c.Agent.SystemPromptFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(resolveRoot(root), path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("system_prompt_file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if strings.TrimSpace(c.Agent.SystemPrompt) == "" {
		return DefaultSystemPrompt, nil
	}
	return c.Agent.SystemPrompt, nil
}

// Validate checks that the selected model's provider is usable.
func (c *Config) Validate(model string) error {
	e, ok := c.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown model %q (configured: %s)", model, c.providerNames())
	}
	if e.Kind == "" {
		return fmt.Errorf("provider %q: kind is required", model)
	}
	if e.BaseURL == "" {
		return fmt.Errorf("provider %q: base_url is required", model)
	}
	if !e.Configured() {
		if env := e.AuthEnvName(); env != "" {
			return fmt.Errorf("provider %q: missing env %s", model, env)
		}
		return fmt.Errorf("provider %q: missing auth material", model)
	}
	return nil
}

func (c *Config) providerNames() string {
	names := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}
