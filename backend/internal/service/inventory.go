package service

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func queryLimit(r *http.Request, fallback int) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 || n > 200 {
		return 0, bad(400, "invalid_limit")
	}
	return n, nil
}

func creditInventory(ctx context.Context, tx *sql.Tx, g, p, item string, amount int64, key, reason string, now int64) (int64, error) {
	if amount < 1 {
		return 0, bad(400, "invalid_reward_amount")
	}
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO inventory(game_id,player_id,item,amount) VALUES(?,?,?,0)", g, p, item); err != nil {
		return 0, err
	}
	var balance int64
	if err := tx.QueryRowContext(ctx, "SELECT amount FROM inventory WHERE game_id=? AND player_id=? AND item=? FOR UPDATE", g, p, item).Scan(&balance); err != nil {
		return 0, err
	}
	if balance < 0 || balance > math.MaxInt64-amount {
		return 0, bad(409, "inventory_overflow")
	}
	balance += amount
	if _, err := tx.ExecContext(ctx, "UPDATE inventory SET amount=? WHERE game_id=? AND player_id=? AND item=?", balance, g, p, item); err != nil {
		return 0, err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO inventory_ledger(game_id,player_id,item,delta,balance,business_key,reason,created_at) VALUES(?,?,?,?,?,?,?,?)", g, p, item, amount, balance, key, reason, now)
	return balance, err
}

type ConsumeRequest struct {
	RequestID string `json:"request_id"`
	Item      string `json:"item"`
	Amount    int64  `json:"amount"`
}

func (v ConsumeRequest) validate() error {
	if strings.TrimSpace(v.RequestID) == "" || len(v.RequestID) > 128 || strings.TrimSpace(v.Item) == "" || len(v.Item) > 64 || v.Amount < 1 || v.Amount > 1000000 {
		return bad(400, "invalid_consumption")
	}
	return nil
}

type ConsumeResult struct {
	RequestID string `json:"request_id"`
	Item      string `json:"item"`
	Amount    int64  `json:"amount"`
	Balance   int64  `json:"balance"`
}
type LedgerEntry struct {
	ID          int64  `json:"id"`
	Item        string `json:"item"`
	Delta       int64  `json:"delta"`
	Balance     int64  `json:"balance"`
	Reason      string `json:"reason"`
	BusinessKey string `json:"business_key"`
	CreatedAt   int64  `json:"created_at"`
}

func (s *Service) inventoryRoutes(m *http.ServeMux) {
	base := "/v1/games/{game}"
	s.handle(m, "GET "+base+"/inventory", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		rows, err := s.DB.QueryContext(r.Context(), "SELECT item,amount FROM inventory WHERE game_id=? AND player_id=? ORDER BY item", g, p)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]int64{}
		for rows.Next() {
			var item string
			var n int64
			if err = rows.Scan(&item, &n); err != nil {
				return nil, err
			}
			out[item] = n
		}
		return out, rows.Err()
	})
	s.handle(m, "GET "+base+"/items", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		limit, err := queryLimit(r, 100)
		if err != nil {
			return nil, err
		}
		rows, err := s.DB.QueryContext(r.Context(), "SELECT i.id,i.name,COALESCE(v.amount,0) FROM items i LEFT JOIN inventory v ON v.game_id=i.game_id AND v.item=i.id AND v.player_id=? WHERE i.game_id=? AND i.id>? ORDER BY i.id LIMIT ?", p, g, r.URL.Query().Get("after"), limit+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type item struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Amount int64  `json:"amount"`
		}
		out := []item{}
		for rows.Next() {
			var v item
			if err = rows.Scan(&v.ID, &v.Name, &v.Amount); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		next := ""
		if len(out) > limit {
			out = out[:limit]
			next = out[len(out)-1].ID
		}
		return map[string]any{"items": out, "next_cursor": next}, nil
	})
	s.handle(m, "POST "+base+"/inventory/consume", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		var b ConsumeRequest
		if err = decode(r, &b); err != nil {
			return nil, err
		}
		if err = b.validate(); err != nil {
			return nil, err
		}
		out := ConsumeResult{RequestID: b.RequestID, Item: b.Item, Amount: b.Amount}
		err = s.transaction(r.Context(), func(tx *sql.Tx) error {
			now := time.Now().Unix()
			if _, err := tx.ExecContext(r.Context(), "INSERT IGNORE INTO inventory_requests(game_id,player_id,request_id,item,amount,created_at) VALUES(?,?,?,?,?,?)", g, p, b.RequestID, b.Item, b.Amount, now); err != nil {
				return err
			}
			var savedItem string
			var savedAmount int64
			var previous sql.NullInt64
			if err := tx.QueryRowContext(r.Context(), "SELECT item,amount,result_balance FROM inventory_requests WHERE game_id=? AND player_id=? AND request_id=? FOR UPDATE", g, p, b.RequestID).Scan(&savedItem, &savedAmount, &previous); err != nil {
				return err
			}
			if savedItem != b.Item || savedAmount != b.Amount {
				return bad(409, "idempotency_conflict")
			}
			if previous.Valid {
				out.Balance = previous.Int64
				return nil
			}
			var known int
			if err := tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, b.Item).Scan(&known); err != nil {
				return err
			}
			if known != 1 {
				return bad(400, "unknown_item")
			}
			var balance int64
			err := tx.QueryRowContext(r.Context(), "SELECT amount FROM inventory WHERE game_id=? AND player_id=? AND item=? FOR UPDATE", g, p, b.Item).Scan(&balance)
			if err == sql.ErrNoRows {
				return bad(409, "insufficient_items")
			}
			if err != nil {
				return err
			}
			if balance < b.Amount {
				return bad(409, "insufficient_items")
			}
			out.Balance = balance - b.Amount
			if _, err = tx.ExecContext(r.Context(), "UPDATE inventory SET amount=? WHERE game_id=? AND player_id=? AND item=?", out.Balance, g, p, b.Item); err != nil {
				return err
			}
			if _, err = tx.ExecContext(r.Context(), "INSERT INTO inventory_ledger(game_id,player_id,item,delta,balance,business_key,reason,created_at) VALUES(?,?,?,?,?,?,'client_consume',?)", g, p, b.Item, -b.Amount, out.Balance, "consume:"+b.RequestID, now); err != nil {
				return err
			}
			_, err = tx.ExecContext(r.Context(), "UPDATE inventory_requests SET result_balance=? WHERE game_id=? AND player_id=? AND request_id=?", out.Balance, g, p, b.RequestID)
			return err
		})
		return out, err
	})
	s.handle(m, "GET "+base+"/inventory/ledger", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		limit, err := queryLimit(r, 50)
		if err != nil {
			return nil, err
		}
		before := int64(math.MaxInt64)
		if raw := r.URL.Query().Get("before"); raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before < 1 {
				return nil, bad(400, "invalid_cursor")
			}
		}
		rows, err := s.DB.QueryContext(r.Context(), "SELECT id,item,delta,balance,reason,business_key,created_at FROM inventory_ledger WHERE game_id=? AND player_id=? AND id<? ORDER BY id DESC LIMIT ?", g, p, before, limit+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []LedgerEntry{}
		for rows.Next() {
			var v LedgerEntry
			if err = rows.Scan(&v.ID, &v.Item, &v.Delta, &v.Balance, &v.Reason, &v.BusinessKey, &v.CreatedAt); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		next := ""
		if len(out) > limit {
			out = out[:limit]
			next = strconv.FormatInt(out[len(out)-1].ID, 10)
		}
		return map[string]any{"entries": out, "next_cursor": next}, nil
	})
}
