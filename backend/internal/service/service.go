package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/go-sql-driver/mysql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Service struct {
	DB             *sql.DB
	Sandbox        bool
	mu             sync.Mutex
	limits         map[string]bucket
	nextLimitSweep time.Time
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
	return &Service{DB: db, Sandbox: sandbox, limits: map[string]bucket{}}
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
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
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
				slog.Error("request_failed", "path", r.URL.Path, "error_type", "database_or_service")
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
	if e != nil {
		return "", bad(401, "unauthorized")
	}
	return id, nil
}
func (s *Service) scope(r *http.Request) (string, string, error) {
	id, e := s.actor(r)
	if e != nil {
		return "", "", e
	}
	g := r.PathValue("game")
	var active bool
	if e = s.DB.QueryRowContext(r.Context(), "SELECT active FROM games WHERE id=?", g).Scan(&active); e != nil || !active {
		return "", "", bad(404, "game_not_found")
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
	s.operationsRoutes(m)
	s.platformRoutes(m)
	s.plannerRoutes(m)
	s.replayRoutes(m)
	s.diagnosticRoutes(m)
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
