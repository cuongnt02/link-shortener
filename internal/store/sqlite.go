package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// SQLiteStore is a data structure that holds the connection to the sqlite database for saving and getting links
// The store is safe for concurrent use across goroutines
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore create a new sqlite store from the database file path supplied.
// Then check for the connection and create a schema
func NewSQLiteStore(ctx context.Context, path string) (_ *SQLiteStore, err error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%v?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path))
	if err != nil {
		return nil, fmt.Errorf("opening database %q: %w", path, err)
	}
	defer func() {
		if err != nil {
			if cerr := db.Close(); cerr != nil {
				err = errors.Join(
					err,
					fmt.Errorf("closing database: %w", cerr))
			}
		}
	}()
	err = db.PingContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting to database %q: %w", path, err)
	}

	ddl := `CREATE TABLE IF NOT EXISTS links(
		code TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.ExecContext(ctx, ddl)
	if err != nil {
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &SQLiteStore{
		db: db,
	}, nil
}

// Save insert a new code and url entry into the links table, returns ErrCodeExists if the inserted code is duplicated
func (s *SQLiteStore) Save(ctx context.Context, code, url string) error {
	query := `INSERT INTO links(code, url) VALUES (?, ?) ON CONFLICT(code) DO NOTHING;`
	result, err := s.db.ExecContext(ctx, query, code, url)
	if err != nil {
		return fmt.Errorf("insert new entry: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected after query: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("insert a duplicated code %q: %w", code, ErrCodeExists)
	}

	return nil
}

// Get query the entry in which the code is supplied, returns ErrNotFound when the query returns no result
func (s *SQLiteStore) Get(ctx context.Context, code string) (string, error) {
	query := `SELECT url FROM links WHERE code = ?`
	var url string
	err := s.db.QueryRowContext(ctx, query, code).Scan(&url)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("query for entry %q: %w", code, ErrNotFound)
	case err != nil:
		return "", fmt.Errorf("query for entry %q: %w", code, err)
	}
	return url, nil
}

// Close shut the database connection down, prevent any query performed
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
