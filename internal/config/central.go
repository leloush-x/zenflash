// Package config centralizes every environment variable, default, constant,
// timeout, limit, port, and header/route name in one typed struct.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Env names. Old names keep working; new names are additive only.
const (
	EnvDatabaseURL               = "DATABASE_URL"
	EnvWebUIUsername             = "WEBUI_USERNAME"
	EnvWebUIPassword             = "WEBUI_PASSWORD"
	EnvAntigravityOAuthClients   = "ANTIGRAVITY_OAUTH_CLIENTS"
	EnvAntigravityOAuthClientKey = "ANTIGRAVITY_OAUTH_CLIENT_KEY"
	EnvConfigPath                = "CONFIG_PATH"
	EnvConfigSeedPath            = "CONFIG_SEED_PATH"
	EnvListenAddress             = "LISTEN_ADDRESS"
	EnvWebUIListenAddress        = "WEBUI_LISTEN_ADDRESS"
	EnvStateDir                  = "STATE_DIR"
)

// Default listen addresses and ports.
const (
	DefaultAPIListen   = "127.0.0.1:8080"
	DefaultWebUIListen = "127.0.0.1:8081"
	DefaultClineHost   = "127.0.0.1"
	DefaultClinePort   = 3457
	DefaultOAuthPort   = 51121
)

// Network and server timeouts.
const (
	ReadHeaderTimeout  = 15 * time.Second
	IdleTimeout        = 120 * time.Second
	ShutdownTimeout    = 15 * time.Second
	ClineReadyTimeout  = 10 * time.Second
	ClineReadyInterval = 200 * time.Millisecond
	HTTPClientTimeout  = 2 * time.Second
	CodexLoginTimeout  = 16 * time.Minute
)

// Limits.
const (
	MaxRequestBodyBytes  = 32 << 20
	MaxAdminSessions     = 2048
	MaxLoginAttempts     = 5
	LoginWindow          = 5 * time.Minute
	MaxCatalogCacheBytes = 32 << 20
	ProxyFileMaxBytes    = 1024 * 1024
	AuthKeyTTL           = 60 * time.Second
	DBConnectRetryBudget = 30 * time.Second
)

// Default config values (JSON file defaults).
const (
	DefaultUpstreamZen                = "https://opencode.ai/zen"
	DefaultUpstreamGo                 = "https://opencode.ai/zen/go"
	DefaultUpstreamCodex              = "https://chatgpt.com/backend-api/codex"
	DefaultRetryMaxAttempts           = 3
	DefaultRetryTimeoutSeconds        = 300
	DefaultModelsRefreshSeconds       = 300
	DefaultPerfMaxIdleConns           = 2048
	DefaultPerfMaxIdleConnsPerHost    = 256
	DefaultPerfMaxConnsPerHost        = 0
	DefaultPerfIdleConnTimeoutSeconds = 120
	DefaultPerfConnectTimeoutSeconds  = 5
	DefaultPerfFailureCooldownSeconds = 15
	DefaultPerfAttemptTimeoutSeconds  = 0
	DefaultLogLevel                   = "info"
	DefaultLogRingSize                = 2000
	DefaultWebUIEnabledListen         = "0.0.0.0:8081"
	DefaultWebUISessionTTLMinutes     = 720
	DefaultPreferTier                 = TierGo
)

// HTTP header names.
const (
	HeaderAuthorization     = "Authorization"
	HeaderAPIKey            = "x-api-key"
	HeaderAnthropicVersion  = "anthropic-version"
	HeaderRequestID         = "x-request-id"
	HeaderSessionID         = "x-session-id"
	HeaderOpenCodeSession   = "x-opencode-session"
	HeaderSessionAffinity   = "x-session-affinity"
	HeaderConversationID    = "conversation-id"
	HeaderConversationIDAlt = "conversation_id"
	HeaderCSRFToken         = "X-CSRF-Token"
	BearerPrefix            = "Bearer "
)

// Routes (frozen; must not change).
const (
	RouteModels          = "/v1/models"
	RouteChatCompletions = "/v1/chat/completions"
	RouteResponses       = "/v1/responses"
	RouteMessages        = "/v1/messages"
	RouteSystemOne       = "/v1/systemone"
	RouteHealthz         = "/healthz"
)

// EnvConfig is the single typed view of process environment, loaded once.
type EnvConfig struct {
	DatabaseURL               string
	WebUIUsername             string
	WebUIPassword             string
	AntigravityOAuthClients   string
	AntigravityOAuthClientKey string
	ConfigPath                string
	ConfigSeedPath            string
	ListenAddress             string
	WebUIListenAddress        string
	StateDir                  string
}

// LoadEnv reads the environment once.
func LoadEnv() EnvConfig {
	get := func(k string) string { return strings.TrimSpace(os.Getenv(k)) }
	return EnvConfig{
		DatabaseURL:               strings.TrimSpace(os.Getenv(EnvDatabaseURL)),
		WebUIUsername:             get(EnvWebUIUsername),
		WebUIPassword:             get(EnvWebUIPassword),
		AntigravityOAuthClients:   strings.TrimSpace(os.Getenv(EnvAntigravityOAuthClients)),
		AntigravityOAuthClientKey: strings.TrimSpace(os.Getenv(EnvAntigravityOAuthClientKey)),
		ConfigPath:                get(EnvConfigPath),
		ConfigSeedPath:            get(EnvConfigSeedPath),
		ListenAddress:             get(EnvListenAddress),
		WebUIListenAddress:        get(EnvWebUIListenAddress),
		StateDir:                  get(EnvStateDir),
	}
}

var (
	envOnce   sync.Once
	cachedEnv EnvConfig
)

// LoadEnvOnce loads the environment once per process.
func LoadEnvOnce() EnvConfig {
	envOnce.Do(func() { cachedEnv = LoadEnv() })
	return cachedEnv
}

// Validate reports a clear error for an invalid environment.
func (e EnvConfig) Validate() error {
	if e.DatabaseURL != "" {
		u, err := url.Parse(e.DatabaseURL)
		if err != nil || u.Host == "" {
			return fmt.Errorf("DATABASE_URL must be a valid URL with a host")
		}
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return fmt.Errorf("DATABASE_URL must use postgres:// or postgresql:// scheme")
		}
		q := u.Query()
		if ssl := strings.ToLower(strings.TrimSpace(q.Get("sslmode"))); ssl == "disable" || ssl == "allow" {
			return fmt.Errorf("DATABASE_URL must not weaken TLS (sslmode=%q is forbidden)", ssl)
		}
	}
	return nil
}

// IsWindows reports Windows from the OS env (centralized accessor).
func IsWindows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}
