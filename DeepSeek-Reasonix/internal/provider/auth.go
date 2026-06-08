package provider

import (
	"net/http"
	"strings"
)

const (
	AuthTypeAPIKey           = "api_key"
	AuthTypeBearer           = "bearer"
	AuthTypeWorkloadIdentity = "workload_identity"
)

// AuthConfig describes request authentication after config/env resolution.
type AuthConfig struct {
	Type          string
	Token         string
	TokenEnv      string
	HeaderName    string
	HeaderScheme  string
	IdentityToken string
	IdentityEnv   string
	Extra         map[string]string
}

// AuthConfigFromExtra reads the auth fields accepted by provider.Config.Extra.
// It supports both the structured "auth" value passed by boot and individual
// string fields used by provider-package tests or custom assemblers.
func AuthConfigFromExtra(extra map[string]any, fallbackToken, fallbackEnv string) AuthConfig {
	var auth AuthConfig
	if v, ok := extra["auth"].(AuthConfig); ok {
		auth = v
	}
	read := func(key string) string {
		if v, ok := extra[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	if auth.Type == "" {
		auth.Type = read("auth_type")
	}
	if auth.Token == "" {
		auth.Token = read("auth_token")
	}
	if auth.TokenEnv == "" {
		auth.TokenEnv = read("auth_token_env")
	}
	if auth.HeaderName == "" {
		auth.HeaderName = read("auth_header")
	}
	if auth.HeaderScheme == "" {
		auth.HeaderScheme = read("auth_scheme")
	}
	if auth.IdentityToken == "" {
		auth.IdentityToken = read("identity_token")
	}
	if auth.IdentityEnv == "" {
		auth.IdentityEnv = read("identity_env")
	}
	if auth.Extra == nil {
		auth.Extra = map[string]string{}
	}
	for _, key := range []string{"federation_rule_id", "organization_id", "service_account_id", "workspace_id"} {
		if auth.Extra[key] == "" {
			auth.Extra[key] = read(key)
		}
	}
	if auth.Token == "" {
		auth.Token = fallbackToken
	}
	if auth.TokenEnv == "" {
		auth.TokenEnv = fallbackEnv
	}
	return auth
}

// NormalizedType returns the supported auth type, preserving the empty/default
// behavior as API key auth for back-compat.
func (a AuthConfig) NormalizedType() string {
	switch strings.ToLower(strings.TrimSpace(a.Type)) {
	case "", AuthTypeAPIKey, "apikey", "api-key":
		return AuthTypeAPIKey
	case AuthTypeBearer, "oauth", "oauth_bearer", "access_token":
		return AuthTypeBearer
	case AuthTypeWorkloadIdentity, "wif", "workload-identity":
		return AuthTypeWorkloadIdentity
	default:
		return strings.ToLower(strings.TrimSpace(a.Type))
	}
}

// EnvName returns the env var the current auth mode depends on.
func (a AuthConfig) EnvName() string {
	switch a.NormalizedType() {
	case AuthTypeWorkloadIdentity:
		if strings.TrimSpace(a.TokenEnv) != "" {
			return strings.TrimSpace(a.TokenEnv)
		}
		return strings.TrimSpace(a.IdentityEnv)
	default:
		return strings.TrimSpace(a.TokenEnv)
	}
}

// Header applies this auth config to req. API key auth defaults to the supplied
// provider-specific header name; bearer/workload identity auth uses
// Authorization: Bearer unless overridden.
func (a AuthConfig) Header(req *http.Request, defaultAPIKeyHeader string) {
	if req == nil {
		return
	}
	token := strings.TrimSpace(a.Token)
	if token == "" {
		return
	}
	switch a.NormalizedType() {
	case AuthTypeBearer, AuthTypeWorkloadIdentity:
		header := strings.TrimSpace(a.HeaderName)
		if header == "" {
			header = "Authorization"
		}
		scheme := strings.TrimSpace(a.HeaderScheme)
		if scheme == "" {
			scheme = "Bearer"
		}
		if scheme != "" {
			req.Header.Set(header, scheme+" "+token)
		} else {
			req.Header.Set(header, token)
		}
	default:
		header := strings.TrimSpace(a.HeaderName)
		if header == "" {
			header = strings.TrimSpace(defaultAPIKeyHeader)
		}
		if header == "" {
			header = "Authorization"
		}
		if strings.EqualFold(header, "Authorization") {
			scheme := strings.TrimSpace(a.HeaderScheme)
			if scheme == "" {
				scheme = "Bearer"
			}
			req.Header.Set(header, scheme+" "+token)
		} else {
			req.Header.Set(header, token)
		}
	}
}
