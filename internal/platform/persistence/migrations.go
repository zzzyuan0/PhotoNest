package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ApplyMigrations applies the baseline schema for new databases and then runs
// any idempotent upgrade migrations for existing installations.
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	applied, err := hasBaselineSchema(ctx, db)
	if err != nil {
		return err
	}

	paths, err := migrationPaths()
	if err != nil {
		return err
	}

	if !applied {
		if err := execMigrationFile(ctx, db, paths[0]); err != nil {
			return err
		}
	}

	for _, path := range paths[1:] {
		if err := execMigrationFile(ctx, db, path); err != nil {
			return err
		}
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

func migrationPaths() ([]string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("resolve migration helper path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	dir := filepath.Join(root, "db", "migrations")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no migration files found in %s", dir)
	}
	return paths, nil
}

func execMigrationFile(ctx context.Context, db *sql.DB, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("exec %s: %w", path, err)
	}
	return nil
}
