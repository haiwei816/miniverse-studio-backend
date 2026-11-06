package platformauth

import (
	"context"
	"testing"
	"time"
)

func TestSandboxProof(t *testing.T) {
	now := time.Now()
	s := Sandbox{Channel: "steam", Key: []byte("sandbox-key"), Enabled: true, Now: func() time.Time { return now }}
	v := Identity{Channel: "steam", App: "card-game", Subject: "player", Nonce: "nonce", Expires: now.Add(time.Minute).Unix()}
	ticket, _ := s.Sign(v)
	if _, e := s.Verify(context.Background(), "card-game", ticket); e != nil {
		t.Fatal(e)
	}
	for _, x := range []struct{ game, ticket string }{{"sky-island", ticket}, {"card-game", ticket + "x"}, {"card-game", "forged"}} {
		if _, e := s.Verify(context.Background(), x.game, x.ticket); e == nil {
			t.Fatal("accepted invalid proof")
		}
	}
	v.Expires = now.Unix()
	expired, _ := s.Sign(v)
	if _, e := s.Verify(context.Background(), "card-game", expired); e == nil {
		t.Fatal("accepted expired proof")
	}
	s.Enabled = false
	if _, e := s.Verify(context.Background(), "card-game", ticket); e == nil {
		t.Fatal("production accepted sandbox proof")
	}
}
