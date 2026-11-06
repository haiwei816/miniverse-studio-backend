// Package config reads and validates process configuration at startup.
package config

import (
	"errors"
	"net"
	"os"
	"strconv"
)

type Config struct {
	HTTPHost string
	HTTPPort int
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
	return nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort))
}
