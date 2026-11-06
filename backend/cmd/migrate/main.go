package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log"
	"miniverse/backend/internal/database"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	db, e := database.Open(os.Getenv("MYSQL_DSN"))
	if e != nil {
		log.Fatal("database configuration failed")
	}
	defer db.Close()
	ctx := context.Background()
	conn, e := db.Conn(ctx)
	if e != nil {
		log.Fatal("database connection failed")
	}
	defer conn.Close()
	var locked int
	if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK('miniverse_migrations',30)").Scan(&locked); e != nil || locked != 1 {
		log.Fatal("migration lock failed")
	}
	defer conn.ExecContext(ctx, "SELECT RELEASE_LOCK('miniverse_migrations')")
	if _, e = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS migration_files(name VARCHAR(128) PRIMARY KEY, checksum CHAR(64) NOT NULL, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)"); e != nil {
		log.Fatal("migration tracking failed")
	}
	files, _ := filepath.Glob("migrations/*.sql")
	sort.Strings(files)
	if len(files) == 0 {
		log.Fatal("no migrations found")
	}
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			log.Fatal(e)
		}
		b = []byte(strings.ReplaceAll(string(b), "\r\n", "\n"))
		sum := sha256.Sum256(b)
		checksum := hex.EncodeToString(sum[:])
		name := filepath.Base(f)
		var previous string
		e = conn.QueryRowContext(ctx, "SELECT checksum FROM migration_files WHERE name=?", name).Scan(&previous)
		if e == nil {
			if previous != checksum {
				log.Fatal("applied migration checksum changed: ", name)
			}
			log.Print("already applied ", name)
			continue
		}
		if e != sql.ErrNoRows {
			log.Fatal("migration status failed")
		}
		for _, q := range strings.Split(string(b), ";") {
			if strings.TrimSpace(q) != "" {
				if _, e = conn.ExecContext(ctx, q); e != nil {
					log.Fatal("migration failed: ", name, ": ", e)
				}
			}
		}
		version, e := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if e != nil {
			log.Fatal("invalid migration filename")
		}
		if _, e = conn.ExecContext(ctx, "INSERT IGNORE INTO schema_migrations(version) VALUES(?)", version); e != nil {
			log.Fatal("version tracking failed")
		}
		if _, e = conn.ExecContext(ctx, "INSERT INTO migration_files(name,checksum) VALUES(?,?)", name, checksum); e != nil {
			log.Fatal("checksum tracking failed")
		}
		log.Print("applied ", name)
	}
}
