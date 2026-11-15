// Package config reads and validates process configuration at startup.
package config

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPHost       string
	HTTPPort       int
	TrustedProxies []netip.Prefix
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{HTTPHost: "127.0.0.1", HTTPPort: 8080}
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
	return nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
}
