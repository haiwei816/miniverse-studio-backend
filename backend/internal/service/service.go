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
	"miniverse/backend/internal/cache"
	"miniverse/backend/internal/httpserver"
	"miniverse/backend/internal/lock"
	"miniverse/backend/internal/mq"
	"miniverse/backend/internal/redisx"
	"miniverse/backend/internal/resilience"
	"miniverse/backend/internal/telemetry"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Service struct {
	DB                    *sql.DB
	Sandbox               bool
	TrustedProxies        []netip.Prefix
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
	RedisAddr             string
	KafkaBrokers          []string
	opsAgentMu            sync.RWMutex
	PersistOpsEvents      bool
	Telemetry             *telemetry.Recorder
	mu                    sync.Mutex
	limits                map[string]bucket
	nextLimitSweep        time.Time
	authMu                sync.Mutex
	authLimits            map[string]bucket
	nextAuthSweep         time.Time
	authHashes            chan struct{}
	companionMu           sync.Mutex
	companionLimits       map[string]bucket
	nextCompanionSweep    time.Time
	companionActive       chan struct{}
	opsAgentActive        chan struct{}
	ConcurrentDiagnostics bool
	EnforceBans           bool
	gameCache             *cache.ShardedLRU[bool]
	gameCacheSF           cache.Group[bool]
	activitiesCache       *cache.ShardedLRU[[]Activity]
	activitiesSF          cache.Group[[]Activity]
	banCache              *cache.ShardedLRU[bool]
	boardSF               cache.Group[boardData]
	ruleBreaker           *resilience.CircuitBreaker
	redisRateLimiter      *resilience.TwoTierLimiter
	Redis                 *redisx.Client
	LockStore             lock.Store
	MQTransport           mq.Transport
	OutboxRelay           *mq.OutboxRelay
	kafkaConsumers        []*mq.KafkaConsumerGroup
	stopInvalidationSub   func()
	stopSeasonSettler     func()
	ruleMu                sync.Mutex
	ruleAddr              string
	ruleConn              *grpc.ClientConn
	opsCh                 chan opsEventTask
	opsStopOnce           sync.Once
	opsStopCh             chan struct{}
	opsDoneCh             chan struct{}
	opsRateMu             sync.Mutex
	opsRateLastLog        map[string]int64
}

func (s *Service) Close() error {
	if s.stopSeasonSettler != nil {
		s.stopSeasonSettler()
	}
	if s.stopInvalidationSub != nil {
		s.stopInvalidationSub()
	}
	for _, cg := range s.kafkaConsumers {
		cg.Stop()
	}
	if s.OutboxRelay != nil {
		s.OutboxRelay.Stop()
	}
	if s.MQTransport != nil {
		_ = s.MQTransport.Close()
	}
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	s.stopOpsWriter()
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
	svc := &Service{
		DB:                    db,
		Sandbox:               sandbox,
		RuleCoreAddr:          "rulecore:50051",
		SandboxPlatformSecret: "local-sandbox-not-a-platform-key",
		Telemetry:             telemetry.New(),
		limits:                map[string]bucket{},
		authLimits:            map[string]bucket{},
		authHashes:            make(chan struct{}, 4),
		companionLimits:       map[string]bucket{},
		companionActive:       make(chan struct{}, 16),
		opsAgentActive:        make(chan struct{}, 8),
		gameCache:             cache.NewShardedLRU[bool](1024),
		activitiesCache:       cache.NewShardedLRU[[]Activity](512),
		banCache:              cache.NewShardedLRU[bool](4096),
		ruleBreaker:           resilience.NewCircuitBreaker(5, 3*time.Second),
		redisRateLimiter:      resilience.NewTwoTierLimiter(nil, "mv:rl:api", 600, 10000),
		opsRateLastLog:        map[string]int64{},
	}
	return svc
}

// InvalidateGameCache evicts cached active state for a game (or all games when empty).
func (s *Service) InvalidateGameCache(gameID string) {
	if s.gameCache != nil {
		if gameID == "" {
			s.gameCache.Clear()
		} else {
			s.gameCache.Delete(gameID)
		}
	}
	if s.Redis != nil {
		s.Redis.PublishInvalidation(context.Background(), redisx.InvalidationEvent{
			Kind:   "game",
			GameID: gameID,
		})
	}
}

// InvalidateActivitiesCache evicts cached active activity configs for a game.
func (s *Service) InvalidateActivitiesCache(gameID string) {
	if s.activitiesCache != nil {
		if gameID == "" {
			s.activitiesCache.Clear()
		} else {
			s.activitiesCache.Delete(gameID)
		}
	}
	if s.Redis != nil {
		s.Redis.PublishInvalidation(context.Background(), redisx.InvalidationEvent{
			Kind:   "activity",
			GameID: gameID,
		})
	}
}

// InvalidateBanCache evicts local/Redis ban state for player and notifies peer instances.
func (s *Service) InvalidateBanCache(playerID string) {
	if playerID == "" {
		return
	}
	if s.banCache != nil {
		s.banCache.Delete(playerID)
	}
	if s.Redis != nil && s.Redis.Raw() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_ = s.Redis.Raw().Del(ctx, "mv:ban:"+playerID).Err()
		cancel()
		s.Redis.PublishInvalidation(context.Background(), redisx.InvalidationEvent{
			Kind:     "ban",
			PlayerID: playerID,
		})
	}
}

func (s *Service) gameActive(ctx context.Context, gameID string) (bool, error) {
	if s.gameCache != nil {
		if active, neg, ok := s.gameCache.Get(gameID); ok {
			if s.Telemetry != nil {
				s.Telemetry.RecordCacheHit()
			}
			if neg {
				return false, nil
			}
			return active, nil
		}
	}
	if s.Telemetry != nil {
		s.Telemetry.RecordCacheMiss()
	}
	active, err, _ := s.gameCacheSF.Do(gameID, func() (bool, error) {
		var active bool
		err := s.DB.QueryRowContext(ctx, "SELECT active FROM games WHERE id=?", gameID).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) {
			if s.gameCache != nil {
				s.gameCache.SetNegative(gameID, 3*time.Second)
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if s.gameCache != nil {
			if active {
				s.gameCache.Set(gameID, true, 5*time.Second)
			} else {
				s.gameCache.Set(gameID, false, 3*time.Second)
			}
		}
		return active, nil
	})
	return active, err
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

type reqMetaKey struct{}

type reqMeta struct {
	gameID   string
	playerID string
	cookies  []*http.Cookie
}

func setResponseCookie(r *http.Request, c *http.Cookie) {
	if r == nil || c == nil {
		return
	}
	if meta, ok := r.Context().Value(reqMetaKey{}).(*reqMeta); ok && meta != nil {
		meta.cookies = append(meta.cookies, c)
	}
}

func requireCookieCSRF(r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	if r.Header.Get("X-MiniVerse-CSRF") != "1" {
		return bad(403, "csrf_rejected")
	}
	return nil
}

func writeResponseCookies(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		return
	}
	if meta, ok := r.Context().Value(reqMetaKey{}).(*reqMeta); ok && meta != nil {
		for _, c := range meta.cookies {
			http.SetCookie(w, c)
		}
	}
}

func (s *Service) handle(m *http.ServeMux, path string, f endpoint) {
	s.handleWithTimeout(m, path, 10*time.Second, f)
}

func (s *Service) handleWithTimeout(m *http.ServeMux, path string, timeout time.Duration, f endpoint) {
	m.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		meta := &reqMeta{gameID: r.PathValue("game")}
		ctx = context.WithValue(ctx, reqMetaKey{}, meta)
		r = r.WithContext(ctx)
		host := clientIP(r, s.TrustedProxies)
		w.Header().Set("Content-Type", "application/json")
		if !s.allowRequest(host, time.Now()) {
			elapsed := time.Since(start)
			if s.Telemetry != nil {
				s.Telemetry.RecordRateLimit()
				s.Telemetry.RecordRequest(path, r.Method, 429, elapsed)
			}
			s.recordOpsEvent(r, "rate_limit", 429, "rate_limited", elapsed, meta.playerID, nil)
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
			return
		}
		v, e := f(r)
		elapsed := time.Since(start)
		writeResponseCookies(w, r)
		if e != nil {
			status, code := 500, "internal_error"
			var p Problem
			if errors.As(e, &p) {
				status, code = p.Status, p.Code
			} else {
				// Do not log raw driver errors: they can contain SQL values or credentials.
				attrs := []any{"path", r.URL.Path, "error_type", fmt.Sprintf("%T", e)}
				if reqID := httpserver.RequestID(r.Context()); reqID != "" {
					attrs = append(attrs, "request_id", reqID)
				}
				var mysqlErr *mysql.MySQLError
				if errors.As(e, &mysqlErr) {
					attrs = append(attrs, "mysql_code", mysqlErr.Number)
				}
				slog.Error("request_failed", attrs...)
			}
			if s.Telemetry != nil {
				s.Telemetry.RecordRequest(path, r.Method, status, elapsed)
				if code == "auth_rate_limited" {
					s.Telemetry.RecordAuthRateLimit()
				}
			}
			if status == 409 || status == 422 || status == 429 || status >= 500 {
				s.recordOpsEvent(r, classifyOpsCategory(status, code), status, code, elapsed, meta.playerID, nil)
			}
			s.recordFailure(r, code)
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		if s.Telemetry != nil {
			s.Telemetry.RecordRequest(path, r.Method, 200, elapsed)
		}
		json.NewEncoder(w).Encode(v)
	})
}
func (s *Service) playerToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer "), nil
	}
	c, err := r.Cookie("mv_player_session")
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return "", bad(401, "unauthorized")
	}
	if err := requireCookieCSRF(r); err != nil {
		return "", err
	}
	return strings.TrimSpace(c.Value), nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) checkPlayerBan(ctx context.Context, game, player string) error {
	return s.checkPlayerBanWith(ctx, s.DB, game, player)
}

func (s *Service) checkPlayerBanWith(ctx context.Context, q rowQuerier, game, player string) error {
	if !s.EnforceBans || player == "" {
		return nil
	}
	if q == s.DB && game == "" && s.banCache != nil {
		if banned, _, ok := s.banCache.Get(player); ok && !banned {
			return nil
		}
	}
	if q == s.DB && game == "" && s.Redis != nil && s.Redis.Raw() != nil {
		rCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		val, rErr := s.Redis.Raw().Get(rCtx, "mv:ban:"+player).Result()
		cancel()
		if rErr == nil && val == "0" {
			if s.banCache != nil {
				s.banCache.Set(player, false, 3*time.Second)
			}
			return nil
		}
	}
	now := time.Now().Unix()
	var n int
	var err error
	if game != "" {
		err = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM player_bans WHERE player_id=? AND (game_id='' OR game_id=?) AND (expires_at IS NULL OR expires_at>?)", player, game, now).Scan(&n)
	} else {
		err = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM player_bans WHERE player_id=? AND (expires_at IS NULL OR expires_at>?)", player, now).Scan(&n)
	}
	if err != nil {
		return err
	}
	// Cache only non-banned state so temporary bans (expires_at) lift instantaneously upon expiry.
	if q == s.DB && game == "" && n == 0 {
		if s.banCache != nil {
			s.banCache.Set(player, false, 3*time.Second)
		}
		if s.Redis != nil && s.Redis.Raw() != nil {
			rCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_ = s.Redis.Raw().Set(rCtx, "mv:ban:"+player, "0", 15*time.Second).Err()
			cancel()
		}
	}
	if n > 0 {
		return bad(403, "account_suspended")
	}
	return nil
}

func (s *Service) actor(r *http.Request) (string, error) {
	t, err := s.playerToken(r)
	if err != nil {
		return "", err
	}
	var id string
	e := s.DB.QueryRowContext(r.Context(), "SELECT player_id FROM sessions WHERE token_hash=? AND expires_at>?", s.sessionHash(t), time.Now().Unix()).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return "", bad(401, "unauthorized")
	}
	if e != nil {
		return "", e
	}
	if e = s.checkPlayerBan(r.Context(), "", id); e != nil {
		return "", e
	}
	if meta, ok := r.Context().Value(reqMetaKey{}).(*reqMeta); ok && meta != nil {
		meta.playerID = id
	}
	return id, nil
}
func (s *Service) scope(r *http.Request) (string, string, error) {
	g := r.PathValue("game")
	var id string
	var e error
	isGameToken := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer g_")
	if isGameToken {
		id, e = s.gameActor(r, g)
	} else {
		id, e = s.actor(r)
	}
	if e != nil {
		return "", "", e
	}
	if isGameToken {
		if e = s.checkPlayerBan(r.Context(), g, id); e != nil {
			return "", "", e
		}
	}
	active, e := s.gameActive(r.Context(), g)
	if e != nil {
		return "", "", e
	}
	if !active {
		return "", "", bad(404, "game_not_found")
	}
	if meta, ok := r.Context().Value(reqMetaKey{}).(*reqMeta); ok && meta != nil {
		meta.gameID = g
		meta.playerID = id
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
	s.opsObservabilityRoutes(m)
	s.mcpRoutes(m)
	s.opsAgentRoutes(m)
}
func (s *Service) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	for attempt := 0; attempt < 10; attempt++ {
		err := s.attempt(ctx, fn)
		var me *mysql.MySQLError
		if !errors.As(err, &me) || (me.Number != 1213 && me.Number != 1205) {
			return err
		}
		if s.Telemetry != nil {
			s.Telemetry.RecordDeadlockRetry()
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
