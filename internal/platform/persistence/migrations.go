package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ApplyMigrations applies the current MVP schema to the target database.
// The SQL file uses IF NOT EXISTS and ON CONFLICT patterns, so rerunning it is safe.
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	applied, err := hasBaselineSchema(ctx, db)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	path, err := migrationPath()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("exec %s: %w", path, err)
	}
	return nil
}

func hasBaselineSchema(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'libraries'
		)
	`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check baseline schema: %w", err)
	}

	return exists, nil
}

func migrationPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve migration helper path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(root, "db", "migrations", "000001_init.sql"), nil
}
