package main

import (
	"context"
	"log"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/provider/storage"
)

func main() {
	cfg, err := config.LoadFile(config.DefaultPath())
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := storage.ValidateConfiguredProviders(ctx, cfg); err != nil {
		log.Fatalf("validate storage providers: %v", err)
	}

	log.Printf("photo worker booted for queue=%s storage=%s aiProviders=%d",
		cfg.Queue.Address,
		cfg.StorageProviders.Primary.Name,
		len(cfg.AIProviders),
	)
}
