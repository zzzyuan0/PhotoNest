package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
	"github.com/photonest/photonest/internal/platform/httpserver"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfgPath := config.DefaultPath()
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           httpserver.New(cfg, health.NewDefault(cfg)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("photo api listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen api: %v", err)
	}
}
