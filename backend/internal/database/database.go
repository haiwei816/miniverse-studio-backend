package database

import (
	"context"
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
)

const ExpectedSchemaVersion = 19

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	return db, nil
}
func Ready(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=?", ExpectedSchemaVersion).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return errors.New("schema not ready")
	}
	return nil
}
