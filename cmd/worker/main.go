package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
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

	service, err := enrichment.NewService(enrichment.ServiceConfig{
		Repository:          repository,
		Storage:             provider,
		AIProviders:         buildAIProviders(cfg.AIProviders),
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

func buildAIProviders(configs []config.AIProviderConfig) []providerai.Provider {
	providers := make([]providerai.Provider, 0, len(configs))
	for _, cfg := range configs {
		capabilities := make([]providerai.Capability, 0, len(cfg.Capabilities))
		for _, capability := range cfg.Capabilities {
			switch strings.ToLower(strings.TrimSpace(capability)) {
			case string(providerai.CapabilityCaption):
				capabilities = append(capabilities, providerai.CapabilityCaption)
			case string(providerai.CapabilityOCR):
				capabilities = append(capabilities, providerai.CapabilityOCR)
			case string(providerai.CapabilityEmbedding):
				capabilities = append(capabilities, providerai.CapabilityEmbedding)
			}
		}

		boundary := providerai.BoundaryRemoteService
		if strings.EqualFold(strings.TrimSpace(cfg.ExecutionBoundary), string(providerai.BoundaryLocalSidecar)) {
			boundary = providerai.BoundaryLocalSidecar
		}
		providers = append(providers, providerai.NewDeterministicProvider(cfg.Name, boundary, capabilities, cfg.Model))
	}
	return providers
}
