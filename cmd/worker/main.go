package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/photonest/photonest/internal/enrichment"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/persistence"
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadFile(config.DefaultPath())
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	validateCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := storage.ValidateConfiguredProviders(validateCtx, cfg); err != nil {
		log.Fatalf("validate storage providers: %v", err)
	}

	db, err := persistence.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := persistence.ApplyMigrations(ctx, db); err != nil {
		log.Fatalf("apply postgres migrations: %v", err)
	}

	repository := persistence.NewPostgresRepository(db)
	provider, err := storage.NewProvider(ctx, cfg.StorageProviders.Primary)
	if err != nil {
		log.Fatalf("create storage provider: %v", err)
	}
	queue := persistence.NewRedisQueue(cfg.Queue)
	defer queue.Close()

	aiProviders, err := providerai.BuildProviders(ctx, cfg.AIProviders)
	if err != nil {
		log.Fatalf("build ai providers: %v", err)
	}

	service, err := enrichment.NewService(enrichment.ServiceConfig{
		Repository:          repository,
		Storage:             provider,
		AIProviders:         aiProviders,
		Queue:               queue,
		DownloadTTL:         cfg.Security.DownloadCredentialTTL,
		DebugRetention:      cfg.Security.DebugRetention,
		RetainProviderDebug: true,
	})
	if err != nil {
		log.Fatalf("create enrichment service: %v", err)
	}

	log.Printf("photo worker consuming queue=%s storage=%s aiProviders=%d",
		cfg.Queue.Address, cfg.StorageProviders.Primary.Name, len(cfg.AIProviders))

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := service.RunPending(ctx); err != nil {
				log.Printf("worker run pending: %v", err)
			}
		}
	}
}
