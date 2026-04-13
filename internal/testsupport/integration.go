package testsupport

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/persistence"
)

const (
	defaultPostgresHost = "127.0.0.1"
	defaultPostgresPort = 5432
	defaultPostgresUser = "postgres"
	defaultPostgresPass = "postgres"
	defaultRedisAddr    = "127.0.0.1:6379"
)

func NewPostgresDatabase(t *testing.T) (config.DatabaseConfig, func()) {
	t.Helper()

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adminDB, err := openPostgres(ctx, adminCfg)
	if err != nil {
		t.Skipf("跳过 PostgreSQL 集成测试，无法连接测试数据库: %v", err)
	}

	dbName := fmt.Sprintf("photonest_test_%d", time.Now().UTC().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test database: %v", err)
	}

	appCfg := adminCfg
	appCfg.Name = dbName

	appDB, err := openPostgres(ctx, appCfg)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
		_ = adminDB.Close()
		t.Fatalf("open test database: %v", err)
	}
	if err := applyMigrations(ctx, appDB); err != nil {
		_ = appDB.Close()
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
		_ = adminDB.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	if err := appDB.Close(); err != nil {
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
		_ = adminDB.Close()
		t.Fatalf("close migrated test database: %v", err)
	}

	cleanup := func() {
		t.Helper()

		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()

		_, _ = adminDB.ExecContext(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, _ = adminDB.ExecContext(dropCtx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
		_ = adminDB.Close()
	}

	return appCfg, cleanup
}

func NewRedisQueueConfig(t *testing.T) (config.QueueConfig, func()) {
	t.Helper()

	cfg := config.QueueConfig{
		Address:   defaultEnv("PHOTONEST_TEST_REDIS_ADDR", defaultRedisAddr),
		Password:  config.SecretValue{Value: os.Getenv("PHOTONEST_TEST_REDIS_PASSWORD"), AllowEmpty: true},
		DB:        defaultEnvInt("PHOTONEST_TEST_REDIS_DB", 0),
		Namespace: fmt.Sprintf("photonest-test-%d", time.Now().UTC().UnixNano()),
	}

	password, err := cfg.Password.Resolve(context.Background(), config.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve redis password: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("跳过 Redis 集成测试，无法连接测试实例: %v", err)
	}

	cleanup := func() {
		t.Helper()
		key := cfg.Namespace + ":enrichment:ready"
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanCancel()
		_ = client.Del(cleanCtx, key).Err()
		_ = client.Close()
	}

	return cfg, cleanup
}

func OpenPostgres(t *testing.T, cfg config.DatabaseConfig) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := openPostgres(ctx, cfg)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	return db
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	return persistence.ApplyMigrations(ctx, db)
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
	dsn, err := cfg.EffectiveDSN(ctx)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(max(cfg.MaxOpenConns, 1))
	db.SetMaxIdleConns(max(cfg.MaxIdleConns, 1))
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
