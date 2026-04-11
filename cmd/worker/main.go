package main

import (
	"log"

	"github.com/photonest/photonest/internal/platform/config"
)

func main() {
	cfg, err := config.LoadFile(config.DefaultPath())
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("photo worker booted for queue=%s storage=%s aiProviders=%d",
		cfg.Queue.Address,
		cfg.StorageProviders.Primary.Name,
		len(cfg.AIProviders),
	)
}
