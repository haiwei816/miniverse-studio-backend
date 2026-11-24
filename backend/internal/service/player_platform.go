package service

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type platformGame struct {
	GameID         string `json:"game_id"`
	Name           string `json:"name"`
	Summary        string `json:"summary"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	Accent         string `json:"accent"`
	Icon           string `json:"icon"`
	Version        string `json:"version"`
	EntryURL       string `json:"entry_url"`
	CompanionLevel string `json:"companion_level"`
	Favorite       bool   `json:"favorite"`
	LastPlayedAt   int64  `json:"last_played_at"`
}

const listingColumns = "g.id,g.name,l.summary,l.description,l.category,l.accent,l.icon,l.version,l.entry_url,l.companion_level"
const listingJoin = " FROM game_listings l JOIN games g ON g.id=l.game_id"

type listingScanner interface{ Scan(...any) error }

func scanPlatformGame(row listingScanner, extra ...any) (platformGame, error) {
	var g platformGame
	args := []any{&g.GameID, &g.Name, &g.Summary, &g.Description, &g.Category, &g.Accent, &g.Icon, &g.Version, &g.EntryURL, &g.CompanionLevel}
	err := row.Scan(append(args, extra...)...)
	return g, err
}

var platformUsername = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
var platformEntry = regexp.MustCompile(`^/games/[a-z0-9][a-z0-9/-]*/$`)
var platformVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var platformChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var platformDraftHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validatePlatformGame(g platformGame) error {
	if strings.TrimSpace(g.Name) == "" || utf8.RuneCountInString(g.Name) > 64 || utf8.RuneCountInString(g.Summary) > 256 || utf8.RuneCountInString(g.Description) > 12000 || strings.TrimSpace(g.Category) == "" || utf8.RuneCountInString(g.Category) > 32 || len(g.Version) < 1 || len(g.Version) > 32 || len(g.EntryURL) > 256 || !platformEntry.MatchString(g.EntryURL) || strings.Contains(g.EntryURL, "//") {
		return bad(400, "invalid_listing")
	}
	switch g.Accent {
	case "violet", "cyan", "amber", "rose", "green", "blue", "sky", "mint":
	default:
		return bad(400, "invalid_listing_accent")
	}
	if utf8.RuneCountInString(g.Icon) < 1 || utf8.RuneCountInString(g.Icon) > 4 {
		return bad(400, "invalid_listing_icon")
	}
	switch g.CompanionLevel {
	case "basic", "aware", "context", "interactive":
	default:
		return bad(400, "invalid_companion_level")
	}
	return nil
}

func (s *Service) seedPlayerPlatform() error {
	if !s.Sandbox {
		return nil
	}
	for _, g := range []platformGame{
		{GameID: "sky-island", Name: "天空岛物语", Summary: "在浮空岛收集资源，和伙伴一起修复你的家园。", Description: "一个可玩的轻量探索样例。收集木材、修复建筑，伙伴会根据当前阶段给出提示。进度通过平台云存档保存。", Category: "探索", Accent: "cyan", Icon: "☁", Version: "1.0.0", EntryURL: "/games/sky-island/", CompanionLevel: "aware"},
		{GameID: "card-game", Name: "星际卡牌", Summary: "翻开星际卡片，和伙伴一起寻找相同的星象。", Description: "一个可玩的记忆配对样例。翻开卡片，记住星象的位置，找到六对相同的卡片。匹配进度通过平台云存档保存，伙伴会根据当前局面提供提示。", Category: "策略", Accent: "violet", Icon: "✦", Version: "1.0.0", EntryURL: "/games/card-game/", CompanionLevel: "context"},
		{GameID: "witch-cauldron", Name: "女巫的坩埚", Summary: "围绕神秘坩埚展开策略构筑，开启女巫的冒险。", Description: "女巫主题的策略构筑游戏。局内进度与金币保存在该游戏独立的云存档中，伙伴根据当前场景与目标提供提示。", Category: "策略", Accent: "violet", Icon: "⚗", Version: "1.0.0", EntryURL: "/games/witch-cauldron/", CompanionLevel: "context"},
	} {
		_, e := s.DB.Exec("INSERT IGNORE INTO game_listings(game_id,summary,description,category,accent,icon,version,entry_url,companion_level,published) VALUES(?,?,?,?,?,?,?,?,?,TRUE)", g.GameID, g.Summary, g.Description, g.Category, g.Accent, g.Icon, g.Version, g.EntryURL, g.CompanionLevel)
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) playerPlatformRoutes(m *http.ServeMux) {
	for _, register := range []bool{true, false} {
		path := "POST /v1/platform/auth/login"
		if register {
			path = "POST /v1/platform/auth/register"
		}
		s.handle(m, path, func(r *http.Request) (any, error) {
			var b struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			b.Username = strings.ToLower(strings.TrimSpace(b.Username))
			if !platformUsername.MatchString(b.Username) || len(b.Password) < 8 || len(b.Password) > 128 {
				return nil, bad(400, "invalid_credentials_format")
			}
			if !s.allowPlatformAuth(clientIP(r, s.TrustedProxies), b.Username, time.Now()) {
				return nil, bad(429, "auth_rate_limited")
			}
			var id string
			if register {
				id = random()
				salt := random()
				encoded, hashErr := s.platformPasswordHash(b.Password, salt)
				if hashErr != nil {
					return nil, hashErr
				}
				e := s.transaction(r.Context(), func(tx *sql.Tx) error {
					if _, e := tx.ExecContext(r.Context(), "INSERT INTO accounts(id,secret_hash) VALUES(?,?)", id, hash(random())); e != nil {
						return e
					}
					_, e := tx.ExecContext(r.Context(), "INSERT INTO platform_credentials(player_id,username,password_hash,salt,created_at) VALUES(?,?,?,?,?)", id, b.Username, encoded, salt, time.Now().Unix())
					return e
				})
				var me *mysql.MySQLError
				if errors.As(e, &me) && me.Number == 1062 {
					return nil, bad(409, "username_taken")
				}
				if e != nil {
					return nil, e
				}
			} else {
				var expected, salt string
				e := s.DB.QueryRowContext(r.Context(), "SELECT player_id,password_hash,salt FROM platform_credentials WHERE username=?", b.Username).Scan(&id, &expected, &salt)
				if errors.Is(e, sql.ErrNoRows) {
					if _, hashErr := s.platformPasswordHash(b.Password, "missing-platform-account"); hashErr != nil {
						return nil, hashErr
					}
					return nil, bad(401, "invalid_credentials")
				}
				if e != nil {
					return nil, e
				}
				encoded, hashErr := s.platformPasswordHash(b.Password, salt)
				if hashErr != nil {
					return nil, hashErr
				}
				if subtle.ConstantTimeCompare([]byte(encoded), []byte(expected)) != 1 {
					return nil, bad(401, "invalid_credentials")
				}
			}
			token := random()
			expiry := time.Now().Add(24 * time.Hour).Unix()
			_, e := s.DB.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,player_id,expires_at) VALUES(?,?,?)", s.sessionHash(token), id, expiry)
			return map[string]any{"player_id": id, "username": b.Username, "access_token": token, "expires_at": expiry}, e
		})
	}
	s.handle(m, "GET /v1/platform/me", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		var username string
		e = s.DB.QueryRowContext(r.Context(), "SELECT username FROM platform_credentials WHERE player_id=?", p).Scan(&username)
		if errors.Is(e, sql.ErrNoRows) && s.Sandbox {
			username = "sandbox-player"
			e = nil
		}
		return map[string]any{"player_id": p, "username": username}, e
	})
	s.handle(m, "POST /v1/platform/auth/logout", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		e = s.logoutPlatformSession(r.Context(), p, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /v1/platform/games", func(r *http.Request) (any, error) {
		q, category := r.URL.Query().Get("q"), r.URL.Query().Get("category")
		cursor, limit, e := platformCatalogPage(r)
		if e != nil {
			return nil, e
		}
		if utf8.RuneCountInString(q) > 128 || utf8.RuneCountInString(category) > 32 {
			return nil, bad(400, "invalid_search")
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT "+listingColumns+listingJoin+" WHERE l.published=TRUE AND g.active=TRUE AND (?='' OR g.name LIKE ? OR l.summary LIKE ?) AND (?='' OR l.category=?) AND (?='' OR g.id>?) ORDER BY g.id LIMIT ?", q, "%"+q+"%", "%"+q+"%", category, category, cursor, cursor, limit+1)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []platformGame{}
		for rows.Next() {
			g, e := scanPlatformGame(rows)
			if e != nil {
				return nil, e
			}
			out = append(out, g)
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		if e = rows.Close(); e != nil {
			return nil, e
		}
		next := ""
		if len(out) > limit {
			out = out[:limit]
			next = out[len(out)-1].GameID
		}
		categories, e := s.DB.QueryContext(r.Context(), "SELECT DISTINCT l.category"+listingJoin+" WHERE l.published=TRUE AND g.active=TRUE ORDER BY l.category")
		if e != nil {
			return nil, e
		}
		defer categories.Close()
		allCategories := []string{}
		for categories.Next() {
			var category string
			if e = categories.Scan(&category); e != nil {
				return nil, e
			}
			allCategories = append(allCategories, category)
		}
		return map[string]any{"items": out, "next_cursor": next, "categories": allCategories}, categories.Err()
	})
	s.handle(m, "GET /v1/platform/games/{game}", func(r *http.Request) (any, error) { return s.publishedGame(r, r.PathValue("game")) })
	s.handle(m, "GET /v1/platform/library", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT "+listingColumns+",b.favorite,b.last_played_at"+listingJoin+" JOIN player_library b ON b.game_id=g.id WHERE b.player_id=? AND (b.favorite=TRUE OR b.last_played_at>0) AND l.published=TRUE AND g.active=TRUE ORDER BY b.last_played_at DESC,g.id", p)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []platformGame{}
		for rows.Next() {
			var favorite bool
			var last int64
			g, e := scanPlatformGame(rows, &favorite, &last)
			if e != nil {
				return nil, e
			}
			g.Favorite = favorite
			g.LastPlayedAt = last
			out = append(out, g)
		}
		return map[string]any{"items": out}, rows.Err()
	})
	s.handle(m, "PUT /v1/platform/library/{game}", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		g, e := s.publishedGame(r, r.PathValue("game"))
		if e != nil {
			return nil, e
		}
		var b struct {
			Favorite bool `json:"favorite"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO player_library(player_id,game_id,favorite) VALUES(?,?,?) ON DUPLICATE KEY UPDATE favorite=VALUES(favorite)", p, g.GameID, b.Favorite)
		return map[string]bool{"favorite": b.Favorite}, e
	})
	s.handle(m, "POST /v1/platform/games/{game}/launch", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		g, e := s.publishedGame(r, r.PathValue("game"))
		if e != nil {
			return nil, e
		}
		var b struct {
			Challenge string `json:"code_challenge"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if !platformChallenge.MatchString(b.Challenge) {
			return nil, bad(400, "invalid_code_challenge")
		}
		code := random()
		expiry := time.Now().Add(time.Minute).Unix()
		parentHash := s.sessionHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		var epoch int64
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			// Serialize with logout before taking the context lock. An earlier
			// actor check alone can become stale while this request is queued.
			var owner string
			if e := tx.QueryRowContext(r.Context(), "SELECT player_id FROM sessions WHERE token_hash=? AND expires_at>? FOR UPDATE", parentHash, time.Now().Unix()).Scan(&owner); errors.Is(e, sql.ErrNoRows) {
				return bad(401, "unauthorized")
			} else if e != nil {
				return e
			} else if owner != p {
				return bad(401, "unauthorized")
			}
			if _, e := tx.ExecContext(r.Context(), "INSERT INTO companion_contexts(player_id,game_id,epoch,visible_targets,updated_at) VALUES(?,?,1,JSON_ARRAY(),?) ON DUPLICATE KEY UPDATE game_id=VALUES(game_id),epoch=epoch+1,scene_id='',quest_stage='',visible_targets=JSON_ARRAY(),updated_at=VALUES(updated_at)", p, g.GameID, time.Now().Unix()); e != nil {
				return e
			}
			if e := tx.QueryRowContext(r.Context(), "SELECT epoch FROM companion_contexts WHERE player_id=? FOR UPDATE", p).Scan(&epoch); e != nil {
				return e
			}
			_, e := tx.ExecContext(r.Context(), "INSERT INTO launch_grants(code_hash,player_id,game_id,parent_hash,code_challenge,expires_at,consumed,context_epoch) VALUES(?,?,?,?,?,?,FALSE,?)", hash(code), p, g.GameID, parentHash, b.Challenge, expiry, epoch)
			return e
		})
		return map[string]any{"game_id": g.GameID, "launch_code": code, "expires_at": expiry, "entry_url": g.EntryURL, "epoch": epoch}, e
	})
	s.handle(m, "POST /v1/platform/launch/exchange", func(r *http.Request) (any, error) {
		var b struct {
			Code     string `json:"launch_code"`
			Game     string `json:"game_id"`
			Verifier string `json:"code_verifier"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Code) != 48 || len(b.Game) > 64 || !platformVerifier.MatchString(b.Verifier) {
			return nil, bad(400, "invalid_launch_grant")
		}
		var p, parent string
		token := "g_" + random()
		var expiry, epoch int64
		e := s.transaction(r.Context(), func(tx *sql.Tx) error {
			var game, challenge string
			var deadline, parentDeadline int64
			var consumed bool
			e := tx.QueryRowContext(r.Context(), "SELECT c.player_id,c.game_id,c.parent_hash,c.code_challenge,c.expires_at,c.consumed,s.expires_at,c.context_epoch FROM launch_grants c JOIN sessions s ON s.token_hash=c.parent_hash JOIN companion_contexts cc ON cc.player_id=c.player_id AND cc.game_id=c.game_id AND cc.epoch=c.context_epoch JOIN game_listings l ON l.game_id=c.game_id JOIN games g ON g.id=c.game_id WHERE c.code_hash=? AND l.published=TRUE AND g.active=TRUE FOR UPDATE", hash(b.Code)).Scan(&p, &game, &parent, &challenge, &deadline, &consumed, &parentDeadline, &epoch)
			if errors.Is(e, sql.ErrNoRows) {
				return bad(401, "invalid_launch_grant")
			}
			if e != nil {
				return e
			}
			digest := sha256.Sum256([]byte(b.Verifier))
			provided := base64.RawURLEncoding.EncodeToString(digest[:])
			if consumed || deadline <= time.Now().Unix() || parentDeadline <= time.Now().Unix() || game != b.Game || subtle.ConstantTimeCompare([]byte(provided), []byte(challenge)) != 1 {
				return bad(401, "invalid_launch_grant")
			}
			expiry = parentDeadline
			if _, e = tx.ExecContext(r.Context(), "UPDATE launch_grants SET consumed=TRUE WHERE code_hash=?", hash(b.Code)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(r.Context(), "INSERT INTO game_sessions(token_hash,player_id,game_id,parent_hash,expires_at,context_epoch) VALUES(?,?,?,?,?,?)", s.sessionHash(token), p, game, parent, expiry, epoch); e != nil {
				return e
			}
			_, e = tx.ExecContext(r.Context(), "INSERT INTO player_library(player_id,game_id,last_played_at) VALUES(?,?,?) ON DUPLICATE KEY UPDATE last_played_at=VALUES(last_played_at)", p, game, time.Now().Unix())
			return e
		})
		return map[string]any{"access_token": token, "player_id": p, "game_id": b.Game, "expires_at": expiry, "epoch": epoch}, e
	})
	s.developerListingRoutes(m)
	s.handle(m, "POST /v1/games/{game}/session/logout", func(r *http.Request) (any, error) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer g_") {
			return nil, bad(400, "game_session_required")
		}
		if _, _, e := s.scope(r); e != nil {
			return nil, e
		}
		_, e := s.DB.ExecContext(r.Context(), "DELETE FROM game_sessions WHERE token_hash=?", s.sessionHash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		return map[string]bool{"ok": true}, e
	})
}

func platformCatalogPage(r *http.Request) (string, int, error) {
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 64 {
		return "", 0, bad(400, "invalid_cursor")
	}
	limit := 24
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100 {
			return "", 0, bad(400, "invalid_limit")
		}
		limit = n
	}
	return cursor, limit, nil
}

func (s *Service) publishedGame(r *http.Request, id string) (platformGame, error) {
	g, e := scanPlatformGame(s.DB.QueryRowContext(r.Context(), "SELECT "+listingColumns+listingJoin+" WHERE g.id=? AND g.active=TRUE AND l.published=TRUE", id))
	if errors.Is(e, sql.ErrNoRows) {
		return g, bad(404, "game_not_found")
	}
	return g, e
}

// gameActor accepts only game-scoped tokens and checks the originating platform
// session on every request, so logout revokes its delegated game sessions too.
func (s *Service) gameActor(r *http.Request, game string) (string, error) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var p, scope string
	e := s.DB.QueryRowContext(r.Context(), "SELECT gs.player_id,gs.game_id FROM game_sessions gs JOIN sessions s ON s.token_hash=gs.parent_hash JOIN companion_contexts cc ON cc.player_id=gs.player_id AND cc.game_id=gs.game_id AND cc.epoch=gs.context_epoch WHERE gs.token_hash=? AND gs.expires_at>? AND s.expires_at>?", s.sessionHash(token), time.Now().Unix(), time.Now().Unix()).Scan(&p, &scope)
	if errors.Is(e, sql.ErrNoRows) {
		return "", bad(401, "unauthorized")
	}
	if e != nil {
		return "", e
	}
	if scope != game {
		return "", bad(403, "game_scope_forbidden")
	}
	return p, nil
}

// Normalize the typed draft before hashing: MySQL JSON storage can change key
// order and whitespace without changing the content the reviewer has seen.
func listingDraftHash(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var g platformGame
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	return hash(string(canonical)), nil
}

func (s *Service) developerListingRoutes(m *http.ServeMux) {
	base := "/gm/games/{game}/listing"
	s.handle(m, "GET "+base, func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		var raw []byte
		var author string
		var submitted, published bool
		g, e := scanPlatformGame(s.DB.QueryRowContext(r.Context(), "SELECT "+listingColumns+",l.draft,l.draft_author,l.submitted,l.published"+listingJoin+" WHERE g.id=?", a.Game), &raw, &author, &submitted, &published)
		if errors.Is(e, sql.ErrNoRows) {
			return map[string]any{"published": nil, "draft": nil, "submitted": false, "draft_author": "", "draft_hash": ""}, nil
		}
		if e != nil {
			return nil, e
		}
		draftHash, e := listingDraftHash(raw)
		if e != nil {
			return nil, e
		}
		var draft any
		if len(raw) > 0 {
			if e = json.Unmarshal(raw, &draft); e != nil {
				return nil, e
			}
		}
		var visible any
		if published {
			visible = g
		}
		return map[string]any{"published": visible, "draft": draft, "submitted": submitted, "draft_author": author, "draft_hash": draftHash}, nil
	})
	s.handle(m, "PUT "+base, func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var g platformGame
		if e = decode(r, &g); e != nil {
			return nil, e
		}
		g.GameID = a.Game
		if e = validatePlatformGame(g); e != nil {
			return nil, e
		}
		raw, _ := json.Marshal(g)
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			_, e := tx.ExecContext(r.Context(), "INSERT INTO game_listings(game_id,summary,description,category,accent,icon,version,entry_url,companion_level,draft,draft_author) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE draft=VALUES(draft),draft_author=VALUES(draft_author),submitted=FALSE", g.GameID, g.Summary, g.Description, g.Category, g.Accent, g.Icon, g.Version, g.EntryURL, g.CompanionLevel, raw, a.ID)
			if e != nil {
				return e
			}
			return audit(r, tx, a, "listing_draft", "更新游戏目录草稿", map[string]string{"version": g.Version})
		})
		return map[string]bool{"ok": true}, e
	})
	for _, action := range []string{"submit", "publish", "unpublish"} {
		s.handle(m, "POST "+base+"/"+action, func(r *http.Request) (any, error) {
			permission := "write"
			if action != "submit" {
				permission = "approve"
			}
			a, e := s.gm(r, permission)
			if e != nil {
				return nil, e
			}
			var b struct {
				DraftHash string `json:"draft_hash"`
			}
			if action != "unpublish" {
				if e = decode(r, &b); e != nil {
					return nil, e
				}
				if !platformDraftHash.MatchString(b.DraftHash) {
					return nil, bad(400, "invalid_draft_hash")
				}
			}
			e = s.transaction(r.Context(), func(tx *sql.Tx) error {
				var raw []byte
				var author string
				var submitted bool
				e := tx.QueryRowContext(r.Context(), "SELECT draft,draft_author,submitted FROM game_listings WHERE game_id=? FOR UPDATE", a.Game).Scan(&raw, &author, &submitted)
				if errors.Is(e, sql.ErrNoRows) {
					return bad(404, "listing_not_found")
				}
				if e != nil {
					return e
				}
				if action != "unpublish" {
					currentHash, e := listingDraftHash(raw)
					if e != nil {
						return e
					}
					if currentHash != b.DraftHash {
						return bad(409, "listing_draft_changed")
					}
				}
				switch action {
				case "submit":
					if len(raw) == 0 {
						return bad(409, "listing_draft_required")
					}
					_, e = tx.ExecContext(r.Context(), "UPDATE game_listings SET submitted=TRUE WHERE game_id=?", a.Game)
				case "publish":
					if !submitted || len(raw) == 0 {
						return bad(409, "listing_not_submitted")
					}
					if author == a.ID {
						return bad(403, "independent_review_required")
					}
					var g platformGame
					if e = json.Unmarshal(raw, &g); e != nil {
						return e
					}
					if e = validatePlatformGame(g); e != nil {
						return e
					}
					_, e = tx.ExecContext(r.Context(), "UPDATE game_listings SET summary=?,description=?,category=?,accent=?,icon=?,version=?,entry_url=?,companion_level=?,published=TRUE,draft=NULL,draft_author='',submitted=FALSE WHERE game_id=?", g.Summary, g.Description, g.Category, g.Accent, g.Icon, g.Version, g.EntryURL, g.CompanionLevel, a.Game)
					if e == nil {
						_, e = tx.ExecContext(r.Context(), "UPDATE games SET name=? WHERE id=?", g.Name, a.Game)
					}
				case "unpublish":
					_, e = tx.ExecContext(r.Context(), "UPDATE game_listings SET published=FALSE WHERE game_id=?", a.Game)
				}
				if e != nil {
					return e
				}
				return audit(r, tx, a, "listing_"+action, "游戏目录发布管理", map[string]string{"action": action})
			})
			return map[string]bool{"ok": true}, e
		})
	}
}
