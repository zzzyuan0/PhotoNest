package persistence_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/persistence"
)

const (
	defaultPostgresHost = "127.0.0.1"
	defaultPostgresPort = 5432
	defaultPostgresUser = "postgres"
	defaultPostgresPass = "postgres"
)

func TestApplyMigrationsCreatesLibrariesTable(t *testing.T) {
	adminCfg := config.DatabaseConfig{
		Host:         defaultEnv("PHOTONEST_TEST_POSTGRES_HOST", defaultPostgresHost),
		Port:         defaultEnvInt("PHOTONEST_TEST_POSTGRES_PORT", defaultPostgresPort),
		Name:         defaultEnv("PHOTONEST_TEST_POSTGRES_ADMIN_DB", "postgres"),
		User:         defaultEnv("PHOTONEST_TEST_POSTGRES_USER", defaultPostgresUser),
		Password:     config.SecretValue{Value: defaultEnv("PHOTONEST_TEST_POSTGRES_PASSWORD", defaultPostgresPass)},
		SSLMode:      defaultEnv("PHOTONEST_TEST_POSTGRES_SSLMODE", "disable"),
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminDB, err := openPostgres(ctx, adminCfg)
	if err != nil {
		t.Skipf("跳过 PostgreSQL 集成测试，无法连接测试数据库: %v", err)
	}
	defer adminDB.Close()

	dbName := fmt.Sprintf("photonest_test_migration_%d", time.Now().UTC().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_, _ = adminDB.ExecContext(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, _ = adminDB.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
	}()

	appCfg := adminCfg
	appCfg.Name = dbName

	appDB, err := openPostgres(ctx, appCfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer appDB.Close()

	if err := persistence.ApplyMigrations(ctx, appDB); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var exists bool
	if err := appDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'libraries'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("check libraries table: %v", err)
	}
	if !exists {
		t.Fatal("expected libraries table to exist after applying migrations")
	}
}

func TestApplyMigrationsIsSafeToRunTwice(t *testing.T) {
	adminCfg := config.DatabaseConfig{
		Host:         defaultEnv("PHOTONEST_TEST_POSTGRES_HOST", defaultPostgresHost),
		Port:         defaultEnvInt("PHOTONEST_TEST_POSTGRES_PORT", defaultPostgresPort),
		Name:         defaultEnv("PHOTONEST_TEST_POSTGRES_ADMIN_DB", "postgres"),
		User:         defaultEnv("PHOTONEST_TEST_POSTGRES_USER", defaultPostgresUser),
		Password:     config.SecretValue{Value: defaultEnv("PHOTONEST_TEST_POSTGRES_PASSWORD", defaultPostgresPass)},
		SSLMode:      defaultEnv("PHOTONEST_TEST_POSTGRES_SSLMODE", "disable"),
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminDB, err := openPostgres(ctx, adminCfg)
	if err != nil {
		t.Skipf("跳过 PostgreSQL 集成测试，无法连接测试数据库: %v", err)
	}
	defer adminDB.Close()

	dbName := fmt.Sprintf("photonest_test_migration_rerun_%d", time.Now().UTC().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_, _ = adminDB.ExecContext(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, _ = adminDB.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
	}()

	appCfg := adminCfg
	appCfg.Name = dbName

	appDB, err := openPostgres(ctx, appCfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer appDB.Close()

	if err := persistence.ApplyMigrations(ctx, appDB); err != nil {
		t.Fatalf("first apply migrations: %v", err)
	}
	if err := persistence.ApplyMigrations(ctx, appDB); err != nil {
		t.Fatalf("second apply migrations: %v", err)
	}
}

func TestApplyMigrationsUpgradesExistingAssetsTableWithTags(t *testing.T) {
	adminCfg := config.DatabaseConfig{
		Host:         defaultEnv("PHOTONEST_TEST_POSTGRES_HOST", defaultPostgresHost),
		Port:         defaultEnvInt("PHOTONEST_TEST_POSTGRES_PORT", defaultPostgresPort),
		Name:         defaultEnv("PHOTONEST_TEST_POSTGRES_ADMIN_DB", "postgres"),
		User:         defaultEnv("PHOTONEST_TEST_POSTGRES_USER", defaultPostgresUser),
		Password:     config.SecretValue{Value: defaultEnv("PHOTONEST_TEST_POSTGRES_PASSWORD", defaultPostgresPass)},
		SSLMode:      defaultEnv("PHOTONEST_TEST_POSTGRES_SSLMODE", "disable"),
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminDB, err := openPostgres(ctx, adminCfg)
	if err != nil {
		t.Skipf("跳过 PostgreSQL 集成测试，无法连接测试数据库: %v", err)
	}
	defer adminDB.Close()

	dbName := fmt.Sprintf("photonest_test_migration_upgrade_%d", time.Now().UTC().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_, _ = adminDB.ExecContext(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, _ = adminDB.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
	}()

	appCfg := adminCfg
	appCfg.Name = dbName

	appDB, err := openPostgres(ctx, appCfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer appDB.Close()

	if _, err := appDB.ExecContext(ctx, `
		CREATE EXTENSION IF NOT EXISTS "pgcrypto";
		CREATE TABLE libraries (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			slug TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE assets (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
			media_type TEXT NOT NULL,
			original_filename TEXT NOT NULL,
			content_sha256 TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}

	if err := persistence.ApplyMigrations(ctx, appDB); err != nil {
		t.Fatalf("apply migrations on legacy schema: %v", err)
	}

	var exists bool
	if err := appDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'assets' AND column_name = 'tags'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("check assets.tags column: %v", err)
	}
	if !exists {
		t.Fatal("expected assets.tags column to exist after applying migrations")
	}

	for _, column := range []string{
		"recognition_status_note",
		"search_document",
		"indexed_at",
		"embedding",
		"is_duplicate_exact",
		"duplicate_candidate_of",
	} {
		exists = false
		if err := appDB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'assets' AND column_name = $1
			)
		`, column).Scan(&exists); err != nil {
			t.Fatalf("check assets.%s column: %v", column, err)
		}
		if !exists {
			t.Fatalf("expected assets.%s column to exist after applying migrations", column)
		}
	}
}

func defaultEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func defaultEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func openPostgres(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	return persistence.OpenPostgres(ctx, cfg)
}
