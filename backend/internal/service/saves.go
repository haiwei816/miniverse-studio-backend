package service

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

const maxPooledBufferCap = 64 * 1024

var (
	gzipWriterPool = sync.Pool{
		New: func() any {
			return gzip.NewWriter(io.Discard)
		},
	}
	saveBufPool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 4096))
		},
	}
)

type Save struct {
	Revision int64  `json:"revision"`
	Schema   int    `json:"schema_version"`
	Data     string `json:"data"`
	Checksum string `json:"checksum"`
	Deleted  bool   `json:"deleted"`
}

func unpack(body []byte) (string, error) {
	z, e := gzip.NewReader(bytes.NewReader(body))
	if e != nil {
		return "", e
	}
	defer z.Close()
	buf := saveBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// Prevent oversized buffers (up to 1 MiB) from staying pinned in sync.Pool forever.
		if buf.Cap() <= maxPooledBufferCap {
			buf.Reset()
			saveBufPool.Put(buf)
		}
	}()
	_, e = io.Copy(buf, io.LimitReader(z, (1<<20)+1))
	if buf.Len() > 1<<20 {
		return "", bad(413, "save_too_large")
	}
	if e != nil {
		return "", e
	}
	return buf.String(), nil
}

func pack(data string) []byte {
	buf := saveBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(buf)
	_, _ = zw.Write([]byte(data))
	_ = zw.Close()
	zw.Reset(io.Discard)
	gzipWriterPool.Put(zw)

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	if buf.Cap() <= maxPooledBufferCap {
		buf.Reset()
		saveBufPool.Put(buf)
	}
	return out
}
func (s *Service) savesRoutes(m *http.ServeMux) {
	base := "/v1/games/{game}/saves/{slot}"
	s.handle(m, "GET "+base, func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var v Save
		var body []byte
		e = s.DB.QueryRowContext(r.Context(), "SELECT revision,schema_version,body,checksum,deleted FROM saves WHERE game_id=? AND player_id=? AND slot=?", g, p, r.PathValue("slot")).Scan(&v.Revision, &v.Schema, &body, &v.Checksum, &v.Deleted)
		if e == sql.ErrNoRows {
			return nil, bad(404, "save_not_found")
		}
		if e != nil {
			return nil, e
		}
		if !v.Deleted {
			v.Data, e = unpack(body)
		}
		return v, e
	})
	s.handle(m, "GET "+base+"/history", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT revision,schema_version,checksum,deleted FROM save_history WHERE game_id=? AND player_id=? AND slot=? ORDER BY revision DESC LIMIT 50", g, p, r.PathValue("slot"))
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		a := []Save{}
		for rows.Next() {
			var v Save
			if e = rows.Scan(&v.Revision, &v.Schema, &v.Checksum, &v.Deleted); e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, rows.Err()
	})
	write := func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		slot := r.PathValue("slot")
		if len(slot) < 1 || len(slot) > 64 {
			return nil, bad(400, "invalid_slot")
		}
		var b struct {
			Revision int64  `json:"revision"`
			Schema   int    `json:"schema_version"`
			Data     string `json:"data"`
			Checksum string `json:"checksum"`
			Encoding string `json:"encoding"`
			Restore  int64  `json:"restore_revision"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		deleted := r.Method == "DELETE"
		var result Save
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO saves(game_id,player_id,slot) VALUES(?,?,?)", g, p, slot); e != nil {
				return e
			}
			var rev int64
			if e := tx.QueryRowContext(r.Context(), "SELECT revision FROM saves WHERE game_id=? AND player_id=? AND slot=? FOR UPDATE", g, p, slot).Scan(&rev); e != nil {
				return e
			}
			if rev != b.Revision {
				return bad(409, "revision_conflict")
			}
			data := b.Data
			schema := b.Schema
			if b.Restore > 0 {
				var body []byte
				var oldDeleted bool
				e := tx.QueryRowContext(r.Context(), "SELECT body,schema_version,deleted FROM save_history WHERE game_id=? AND player_id=? AND slot=? AND revision=?", g, p, slot, b.Restore).Scan(&body, &schema, &oldDeleted)
				if e != nil || oldDeleted {
					return bad(404, "history_not_found")
				}
				data, e = unpack(body)
				if e != nil {
					return e
				}
			} else if b.Encoding == "gzip" {
				raw, e := base64.StdEncoding.DecodeString(data)
				if e != nil {
					return bad(400, "invalid_compression")
				}
				data, e = unpack(raw)
				if e != nil {
					return bad(400, "invalid_compression")
				}
			} else if b.Encoding != "" && b.Encoding != "identity" {
				return bad(400, "invalid_encoding")
			}
			if !deleted {
				if len(data) > 1<<20 {
					return bad(413, "save_too_large")
				}
				if schema < 1 || !json.Valid([]byte(data)) {
					return bad(400, "invalid_save")
				}
				if b.Restore == 0 && hash(data) != b.Checksum {
					return bad(400, "checksum_mismatch")
				}
			} else {
				data = ""
				schema = 1
			}
			result = Save{Revision: rev + 1, Schema: schema, Data: data, Checksum: hash(data), Deleted: deleted}
			body := pack(data)
			if _, e := tx.ExecContext(r.Context(), "UPDATE saves SET revision=?,schema_version=?,body=?,checksum=?,deleted=? WHERE game_id=? AND player_id=? AND slot=?", result.Revision, schema, body, result.Checksum, deleted, g, p, slot); e != nil {
				return e
			}
			_, e := tx.ExecContext(r.Context(), "INSERT INTO save_history(game_id,player_id,slot,revision,schema_version,body,checksum,deleted) VALUES(?,?,?,?,?,?,?,?)", g, p, slot, result.Revision, schema, body, result.Checksum, deleted)
			return e
		})
		return result, e
	}
	s.handle(m, "PUT "+base, write)
	s.handle(m, "DELETE "+base, write)
}
