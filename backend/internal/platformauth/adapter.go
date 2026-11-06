// Package platformauth separates provider proof validation from account ownership.
package platformauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Identity struct {
	Channel string `json:"channel"`
	App     string `json:"app_id"`
	Subject string `json:"subject"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"expires_at"`
}
type PlatformAuth interface {
	Verify(context.Context, string, string) (Identity, error)
}
type Sandbox struct {
	Channel string
	Key     []byte
	Enabled bool
	Now     func() time.Time
}

func (s Sandbox) Sign(v Identity) (string, error) {
	if !s.Enabled {
		return "", errors.New("sandbox disabled")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s Sandbox) Verify(ctx context.Context, game, ticket string) (Identity, error) {
	var v Identity
	if !s.Enabled {
		return v, errors.New("real provider not configured")
	}
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 {
		return v, errors.New("invalid ticket")
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return v, e
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return v, errors.New("invalid signature")
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return v, e
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if v.Channel != s.Channel || v.App != game || v.Subject == "" || len(v.Subject) > 128 || v.Nonce == "" || v.Expires <= now.Unix() || v.Expires > now.Add(5*time.Minute).Unix() {
		return Identity{}, errors.New("invalid claims")
	}
	return v, ctx.Err()
}
