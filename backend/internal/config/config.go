// Package config reads and validates process configuration at startup.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPHost              string
	HTTPPort              int
	TrustedProxies        []netip.Prefix
	MySQLDSN              string
	AppEnv                string
	RuleCoreAddr          string
	SandboxPlatformSecret string
	CompanionChatURL      string
	CompanionAPIKey       string
	CompanionModel        string
	OpsAgentProtocol      string
	OpsAgentURL           string
	OpsAgentAPIKey        string
	OpsAgentModel         string
	OpsAgentSecretKey     []byte
	MetricsBearerToken    string
}

func (c Config) IsSandbox() bool {
	return c.AppEnv == "sandbox"
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		HTTPHost:              "127.0.0.1",
		HTTPPort:              8080,
		RuleCoreAddr:          "rulecore:50051",
		SandboxPlatformSecret: "local-sandbox-not-a-platform-key",
	}
	if host, ok := lookup("HTTP_HOST"); ok {
		cfg.HTTPHost = host
	}
	if port, ok := lookup("HTTP_PORT"); ok {
		n, err := strconv.Atoi(port)
		if err != nil {
			return Config{}, errors.New("HTTP_PORT must be an integer between 1 and 65535")
		}
		cfg.HTTPPort = n
	}
	if value, ok := lookup("TRUSTED_PROXIES"); ok && strings.TrimSpace(value) != "" {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			prefix, err := netip.ParsePrefix(item)
			if err != nil {
				addr, addrErr := netip.ParseAddr(item)
				if addrErr != nil || addr.Zone() != "" {
					return Config{}, errors.New("TRUSTED_PROXIES must be a comma-separated list of IP addresses or CIDRs")
				}
				addr = addr.Unmap()
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
		}
	}
	if dsn, ok := lookup("MYSQL_DSN"); ok {
		cfg.MySQLDSN = dsn
	}
	if env, ok := lookup("APP_ENV"); ok {
		cfg.AppEnv = strings.TrimSpace(env)
	}
	if addr, ok := lookup("RULECORE_ADDR"); ok && strings.TrimSpace(addr) != "" {
		cfg.RuleCoreAddr = strings.TrimSpace(addr)
	}
	if secret, ok := lookup("SANDBOX_PLATFORM_SECRET"); ok && secret != "" {
		cfg.SandboxPlatformSecret = secret
	}
	if chatURL, ok := lookup("COMPANION_CHAT_URL"); ok {
		cfg.CompanionChatURL = strings.TrimSpace(chatURL)
	}
	if apiKey, ok := lookup("COMPANION_API_KEY"); ok {
		cfg.CompanionAPIKey = apiKey
	}
	if model, ok := lookup("COMPANION_MODEL"); ok {
		cfg.CompanionModel = strings.TrimSpace(model)
	}
	if opsProto, ok := lookup("OPS_AGENT_PROTOCOL"); ok {
		cfg.OpsAgentProtocol = strings.ToLower(strings.TrimSpace(opsProto))
	}
	if opsURL, ok := lookup("OPS_AGENT_URL"); ok {
		cfg.OpsAgentURL = strings.TrimSpace(opsURL)
	}
	if opsKey, ok := lookup("OPS_AGENT_API_KEY"); ok {
		cfg.OpsAgentAPIKey = opsKey
	}
	if opsModel, ok := lookup("OPS_AGENT_MODEL"); ok {
		cfg.OpsAgentModel = strings.TrimSpace(opsModel)
	}
	if opsSecret, ok := lookup("OPS_AGENT_SECRET_KEY"); ok && strings.TrimSpace(opsSecret) != "" {
		parsed, err := parseSecretKey(opsSecret)
		if err != nil {
			return Config{}, err
		}
		cfg.OpsAgentSecretKey = parsed
	}
	if metricsTok, ok := lookup("METRICS_BEARER_TOKEN"); ok {
		cfg.MetricsBearerToken = strings.TrimSpace(metricsTok)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseSecretKey(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) == 64 {
		if b, err := hex.DecodeString(trimmed); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(trimmed); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("OPS_AGENT_SECRET_KEY must decode to 32 bytes (64 hex chars or base64)")
}

func validateModelEndpoint(urlField, rawURL, modelField, model string, sandbox bool) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(sandbox && u.Scheme == "http")) {
		return errors.New(urlField + " must be an https URL without userinfo (http allowed only in sandbox)")
	}
	if model == "" {
		return errors.New(modelField + " must be set when " + urlField + " is configured")
	}
	return nil
}

func (c Config) Validate() error {
	// Require a literal IP so an accidental empty host cannot expose all interfaces.
	if net.ParseIP(c.HTTPHost) == nil {
		return errors.New("HTTP_HOST must be an IPv4 or IPv6 address")
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return errors.New("HTTP_PORT must be an integer between 1 and 65535")
	}
	for _, prefix := range c.TrustedProxies {
		if !prefix.IsValid() {
			return errors.New("invalid trusted proxy prefix")
		}
	}
	if err := validateModelEndpoint("COMPANION_CHAT_URL", c.CompanionChatURL, "COMPANION_MODEL", c.CompanionModel, c.IsSandbox()); err != nil {
		return err
	}
	if err := validateModelEndpoint("OPS_AGENT_URL", c.OpsAgentURL, "OPS_AGENT_MODEL", c.OpsAgentModel, c.IsSandbox()); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(c.OpsAgentProtocol)) {
	case "", "auto", "openai", "anthropic", "claude":
	default:
		return errors.New("OPS_AGENT_PROTOCOL must be one of openai, anthropic, claude, or auto")
	}
	if len(c.OpsAgentSecretKey) != 0 && len(c.OpsAgentSecretKey) != 32 {
		return errors.New("OPS_AGENT_SECRET_KEY must be 32 bytes")
	}
	if c.MetricsBearerToken != "" && len(c.MetricsBearerToken) < 16 {
		return errors.New("METRICS_BEARER_TOKEN must be at least 16 characters when configured")
	}
	return nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
}
