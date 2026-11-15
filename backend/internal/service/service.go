package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Service struct {
	DB             *sql.DB
	Sandbox        bool
	TrustedProxies []netip.Prefix
	mu             sync.Mutex
	limits         map[string]bucket
	nextLimitSweep time.Time
	authMu         sync.Mutex
	authLimits     map[string]bucket
	nextAuthSweep  time.Time
	authHashes     chan struct{}
	ruleMu         sync.Mutex
	ruleAddr       string
	ruleConn       *grpc.ClientConn
}

func (s *Service) Close() error {
	s.ruleMu.Lock()
	defer s.ruleMu.Unlock()
	if s.ruleConn == nil {
		return nil
	}
	err := s.ruleConn.Close()
	s.ruleConn = nil
	s.ruleAddr = ""
	return err
}

type bucket struct {
	start time.Time
	n     int
}
type Problem struct {
	Status int
	Code   string
}

func (p Problem) Error() string { return p.Code }
func bad(s int, c string) error { return Problem{s, c} }
func New(db *sql.DB, sandbox bool) *Service {
	return &Service{DB: db, Sandbox: sandbox, limits: map[string]bucket{}, authLimits: map[string]bucket{}, authHashes: make(chan struct{}, 4)}
}
func random() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad(400, "invalid_json")
	}
	if d.Decode(new(any)) != io.EOF {
		return bad(400, "trailing_json")
	}
	return nil
}

type endpoint func(*http.Request) (any, error)

func (s *Service) handle(m *http.ServeMux, path string, f endpoint) {
	m.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		host := clientIP(r, s.TrustedProxies)
		w.Header().Set("Content-Type", "application/json")
		if !s.allowRequest(host, time.Now()) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
			return
		}
		v, e := f(r)
		if e != nil {
			status, code := 500, "internal_error"
			var p Problem
			if errors.As(e, &p) {
				status, code = p.Status, p.Code
			} else {
				// Do not log raw driver errors: they can contain SQL values or credentials.
				attrs := []any{"path", r.URL.Path, "error_type", fmt.Sprintf("%T", e)}
				var mysqlErr *mysql.MySQLError
				if errors.As(e, &mysqlErr) {
					attrs = append(attrs, "mysql_code", mysqlErr.Number)
				}
				slog.Error("request_failed", attrs...)
			}
			s.recordFailure(r, code)
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		json.NewEncoder(w).Encode(v)
	})
}
func (s *Service) actor(r *http.Request) (string, error) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return "", bad(401, "unauthorized")
	}
	t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var id string
	e := s.DB.QueryRowContext(r.Context(), "SELECT player_id FROM sessions WHERE token_hash=? AND expires_at>?", s.sessionHash(t), time.Now().Unix()).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return "", bad(401, "unauthorized")
	}
	if e != nil {
		return "", e
	}
	return id, nil
}
func (s *Service) scope(r *http.Request) (string, string, error) {
	g := r.PathValue("game")
	var id string
	var e error
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer g_") {
		id, e = s.gameActor(r, g)
	} else {
		id, e = s.actor(r)
	}
	if e != nil {
		return "", "", e
	}
	var active bool
	e = s.DB.QueryRowContext(r.Context(), "SELECT active FROM games WHERE id=?", g).Scan(&active)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && !active) {
		return "", "", bad(404, "game_not_found")
	}
	if e != nil {
		return "", "", e
	}
	return g, id, nil
}
func (s *Service) Routes(m *http.ServeMux) {
	s.identityRoutes(m)
	s.savesRoutes(m)
	s.activitiesRoutes(m)
	s.rewardRoutes(m)
	s.mailRoutes(m)
	s.inventoryRoutes(m)
	s.boardRoutes(m)
	s.gmRoutes(m)
	s.overviewRoutes(m)
	s.operationsRoutes(m)
	s.platformRoutes(m)
	s.plannerRoutes(m)
	s.replayRoutes(m)
	s.diagnosticRoutes(m)
	s.companionRoutes(m)
	s.playerPlatformRoutes(m)
}
func (s *Service) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	for attempt := 0; attempt < 10; attempt++ {
		err := s.attempt(ctx, fn)
		var me *mysql.MySQLError
		if !errors.As(err, &me) || (me.Number != 1213 && me.Number != 1205) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
	return bad(503, "transaction_busy")
}
func (s *Service) attempt(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
