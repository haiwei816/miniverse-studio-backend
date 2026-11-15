package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "defaults", want: "127.0.0.1:8080"},
		{name: "custom", env: map[string]string{"HTTP_HOST": "0.0.0.0", "HTTP_PORT": "9090"}, want: "0.0.0.0:9090"},
		{name: "ipv6", env: map[string]string{"HTTP_HOST": "::1"}, want: "[::1]:8080"},
		{name: "invalid port", env: map[string]string{"HTTP_PORT": "abc"}, wantErr: true},
		{name: "empty port", env: map[string]string{"HTTP_PORT": ""}, wantErr: true},
		{name: "zero", env: map[string]string{"HTTP_PORT": "0"}, wantErr: true},
		{name: "negative", env: map[string]string{"HTTP_PORT": "-1"}, wantErr: true},
		{name: "overflow", env: map[string]string{"HTTP_PORT": "65536"}, wantErr: true},
		{name: "empty host", env: map[string]string{"HTTP_HOST": ""}, wantErr: true},
		{name: "invalid host", env: map[string]string{"HTTP_HOST": "not-an-ip"}, wantErr: true},
		{name: "trusted proxies", env: map[string]string{"TRUSTED_PROXIES": "127.0.0.1, 10.1.0.0/16"}, want: "127.0.0.1:8080"},
		{name: "invalid proxy", env: map[string]string{"TRUSTED_PROXIES": "10.0.0.1,not-an-ip"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := load(func(key string) (string, bool) { v, ok := tt.env[key]; return v, ok })
			if (err != nil) != tt.wantErr {
				t.Fatalf("load error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg.Address() != tt.want {
				t.Fatalf("address = %q, want %q", cfg.Address(), tt.want)
			}
		})
	}
}
