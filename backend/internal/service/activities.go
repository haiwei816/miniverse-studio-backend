package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Activity struct {
	ID            string `json:"id"`
	Version       int    `json:"version"`
	Kind          string `json:"kind"`
	Event         string `json:"event_type"`
	Target        int64  `json:"target"`
	Starts        int64  `json:"starts_at"`
	Ends          int64  `json:"ends_at"`
	ClientAllowed bool   `json:"client_allowed"`
	Item          string `json:"item"`
	Amount        int64  `json:"reward_amount"`
}

func readActivity(raw []byte) (Activity, error) {
	var a Activity
	e := json.Unmarshal(raw, &a)
	return a, e
}

func validateActivity(a Activity) error {
	if a.Kind == "signin" && a.Target != 1 {
		return bad(400, "signin_target_must_be_one")
	}
	if a.ID == "" || len(a.ID) > 64 || a.Version < 1 || a.Event == "" || len(a.Event) > 64 || a.Target < 1 || a.Target > 1000000000 || a.Starts >= a.Ends || a.Amount < 1 || a.Amount > 1000000 {
		return bad(400, "invalid_activity")
	}
	switch a.Kind {
	case "cumulative", "daily", "signin", "milestone":
	default:
		return bad(400, "invalid_activity_kind")
	}
	return nil
}
func bucketFor(a Activity, now time.Time) string {
	if a.Kind == "daily" || a.Kind == "signin" {
		return now.UTC().Format("2006-01-02")
	}
	return "all"
}
func (s *Service) activitiesRoutes(m *http.ServeMux) {
	s.handle(m, "PUT /v1/games/{game}/activities", func(r *http.Request) (any, error) {
		g, _, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		var a Activity
		if e = decode(r, &a); e != nil {
			return nil, e
		}
		if e = validateActivity(a); e != nil {
			return nil, e
		}
		raw, _ := json.Marshal(a)
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO activities(game_id,id,version,config,active) VALUES(?,?,?,?,TRUE) ON DUPLICATE KEY UPDATE config=IF(VALUES(version)>version,VALUES(config),config),version=GREATEST(version,VALUES(version))", g, a.ID, a.Version, raw)
		return a, e
	})
	s.handle(m, "GET /v1/games/{game}/activities", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT config FROM activities WHERE game_id=? AND active=TRUE ORDER BY id", g)
		if e != nil {
			return nil, e
		}
		configs := []Activity{}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return nil, e
			}
			var a Activity
			if e = json.Unmarshal(raw, &a); e != nil {
				rows.Close()
				return nil, e
			}
			configs = append(configs, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		type progressKey struct {
			id      string
			version int
			bucket  string
		}
		now := time.Now()
		progressMap := map[progressKey]int64{}
		if len(configs) > 0 {
			pRows, e := s.DB.QueryContext(r.Context(), "SELECT activity_id,version,bucket,progress FROM activity_progress WHERE game_id=? AND player_id=? AND bucket IN ('all',?)", g, p, now.UTC().Format("2006-01-02"))
			if e != nil {
				return nil, e
			}
			for pRows.Next() {
				var k progressKey
				var n int64
				if e = pRows.Scan(&k.id, &k.version, &k.bucket, &n); e != nil {
					pRows.Close()
					return nil, e
				}
				progressMap[k] = n
			}
			e = pRows.Err()
			pRows.Close()
			if e != nil {
				return nil, e
			}
		}
		out := []any{}
		for _, a := range configs {
			n := progressMap[progressKey{id: a.ID, version: a.Version, bucket: bucketFor(a, now)}]
			out = append(out, map[string]any{"config": a, "progress": n, "complete": n >= a.Target})
		}
		return out, nil
	})
	s.handle(m, "POST /v1/games/{game}/events", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			ID     string `json:"event_id"`
			Type   string `json:"event_type"`
			Amount int64  `json:"amount"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.ID == "" || len(b.ID) > 128 || b.Type == "" || len(b.Type) > 64 || b.Amount < 1 || b.Amount > 100000 {
			return nil, bad(400, "invalid_event")
		}
		duplicate := false
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			res, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO events(game_id,player_id,event_id,event_type,amount,source,created_at) VALUES(?,?,?,?,?,'client',?)", g, p, b.ID, b.Type, b.Amount, time.Now().Unix())
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			duplicate = n == 0
			if duplicate {
				return nil
			}
			rows, e := tx.QueryContext(r.Context(), "SELECT config FROM activities WHERE game_id=? AND active=TRUE", g)
			if e != nil {
				return e
			}
			configs := []Activity{}
			for rows.Next() {
				var raw []byte
				if e = rows.Scan(&raw); e != nil {
					rows.Close()
					return e
				}
				var a Activity
				if e = json.Unmarshal(raw, &a); e != nil {
					rows.Close()
					return e
				}
				configs = append(configs, a)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			now := time.Now()
			for _, a := range configs {
				if a.Event != b.Type || !a.ClientAllowed || now.Unix() < a.Starts || now.Unix() >= a.Ends {
					continue
				}
				amount := b.Amount
				if a.Kind == "signin" {
					amount = 1
				}
				_, e = tx.ExecContext(r.Context(), "INSERT INTO activity_progress(game_id,player_id,activity_id,version,bucket,progress) VALUES(?,?,?,?,?,LEAST(?,?)) ON DUPLICATE KEY UPDATE progress=LEAST(?,progress+?)", g, p, a.ID, a.Version, bucketFor(a, now), amount, a.Target, a.Target, amount)
				if e != nil {
					return e
				}
				if a.Amount > 0 {
					var progress int64
					bucket := bucketFor(a, now)
					if e = tx.QueryRowContext(r.Context(), "SELECT progress FROM activity_progress WHERE game_id=? AND player_id=? AND activity_id=? AND version=? AND bucket=?", g, p, a.ID, a.Version, bucket).Scan(&progress); e != nil {
						return e
					}
					if progress >= a.Target {
						if e = grant(r.Context(), tx, g, p, fmt.Sprintf("activity:%s:%d:%s", a.ID, a.Version, bucket), a.Item, a.Amount); e != nil {
							return e
						}
					}
				}
			}
			return nil
		})
		return map[string]any{"duplicate": duplicate, "source": "client"}, e
	})
}
