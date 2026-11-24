// Package config reads and validates process configuration at startup.
package config

import (
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
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	if c.CompanionChatURL != "" {
		u, err := url.Parse(c.CompanionChatURL)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(c.IsSandbox() && u.Scheme == "http")) {
			return errors.New("COMPANION_CHAT_URL must be an https URL without userinfo (http allowed only in sandbox)")
		}
		if c.CompanionModel == "" {
			return errors.New("COMPANION_MODEL must be set when COMPANION_CHAT_URL is configured")
		}
	}
	return nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
}
