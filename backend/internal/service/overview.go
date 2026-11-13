package service

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// overviewWindow is the number of UTC days in the dashboard series.
const overviewWindow = 7

type overviewDay struct {
	Day   int64  `json:"day"`
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// countedQuestion keeps the dashboard readable: one place to see which number
// comes from which table, instead of a wall of nested queries.
type countedQuestion struct {
	key   string
	query string
	args  []any
}

// overviewRoutes serves the GM dashboard. Every value is a server-side count:
// the console must not rebuild totals from list endpoints that cap at 100/200
// rows, or the cards would silently drift from the database.
func (s *Service) overviewRoutes(m *http.ServeMux) {
	s.handle(m, "GET /gm/games/{game}/overview", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		ctx, db := r.Context(), s.DB
		now := time.Now().UTC()
		start := now.Truncate(24*time.Hour).AddDate(0, 0, -(overviewWindow - 1))

		var name string
		var configVersion int
		if e = db.QueryRowContext(ctx, "SELECT name,config_version FROM games WHERE id=?", a.Game).Scan(&name, &configVersion); e != nil {
			return nil, bad(404, "game_not_found")
		}

		totals := map[string]int64{}
		questions := []countedQuestion{
			{"players", "SELECT COUNT(*) FROM profiles WHERE game_id=?", []any{a.Game}},
			{"online", "SELECT COUNT(DISTINCT s.player_id) FROM sessions s JOIN profiles p ON p.player_id=s.player_id AND p.game_id=? WHERE s.expires_at>?", []any{a.Game, now.Unix()}},
			{"saves", "SELECT COUNT(*) FROM saves WHERE game_id=? AND deleted=FALSE", []any{a.Game}},
			{"mail_pending", "SELECT COUNT(*) FROM mail m LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.claimed=FALSE AND m.expires_at>? AND COALESCE(st.deleted_at,0)=0", []any{a.Game, now.Unix()}},
			{"activities_active", "SELECT COUNT(*) FROM activities WHERE game_id=? AND active=TRUE", []any{a.Game}},
			{"items", "SELECT COUNT(*) FROM inventory WHERE game_id=? AND amount>0", []any{a.Game}},
			{"requests_pending", "SELECT COUNT(*) FROM gm_requests WHERE game_id=? AND state='pending'", []any{a.Game}},
			{"audits_24h", "SELECT COUNT(*) FROM audits WHERE game_id=? AND created_at>=?", []any{a.Game, now.Unix() - int64(24*time.Hour/time.Second)}},
			{"failures_24h", "SELECT COUNT(*) FROM audits WHERE game_id=? AND created_at>=? AND action IN ('operation_failed','player_anomaly')", []any{a.Game, now.Unix() - int64(24*time.Hour/time.Second)}},
			{"grants_24h", "SELECT COUNT(*) FROM grants WHERE game_id=? AND created_at>=?", []any{a.Game, now.Unix() - int64(24*time.Hour/time.Second)}},
		}
		for _, q := range questions {
			var n int64
			if e = db.QueryRowContext(ctx, q.query, q.args...).Scan(&n); e != nil {
				return nil, e
			}
			totals[q.key] = n
		}

		audits, e := overviewSeries(ctx, db, "SELECT CAST(created_at DIV 86400 AS SIGNED)*86400 AS day,COUNT(*) FROM audits WHERE game_id=? AND created_at>=? GROUP BY day", a.Game, start)
		if e != nil {
			return nil, e
		}
		grants, e := overviewSeries(ctx, db, "SELECT CAST(created_at DIV 86400 AS SIGNED)*86400 AS day,COUNT(*) FROM grants WHERE game_id=? AND created_at>=? GROUP BY day", a.Game, start)
		if e != nil {
			return nil, e
		}

		return map[string]any{
			"game":         map[string]any{"id": a.Game, "name": name, "config_version": configVersion},
			"role":         a.Role,
			"generated_at": now.Unix(),
			"totals":       totals,
			"series":       map[string]any{"days": overviewWindow, "start": start.Unix(), "audits": audits, "grants": grants},
		}, nil
	})
}

// overviewSeries fills the empty days so the chart does not need to invent
// buckets, and so every client renders the same axis for the same window.
func overviewSeries(ctx context.Context, db *sql.DB, query, game string, start time.Time) ([]overviewDay, error) {
	rows, e := db.QueryContext(ctx, query, game, start.Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	buckets := map[int64]int64{}
	for rows.Next() {
		var day, n int64
		if e = rows.Scan(&day, &n); e != nil {
			return nil, e
		}
		buckets[day] = n
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	out := make([]overviewDay, 0, overviewWindow)
	for i := 0; i < overviewWindow; i++ {
		d := start.AddDate(0, 0, i)
		out = append(out, overviewDay{Day: d.Unix(), Date: d.Format("2006-01-02"), Count: buckets[d.Unix()]})
	}
	return out, nil
}
