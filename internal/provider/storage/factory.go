package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/photonest/photonest/internal/platform/config"
)

type HealthCheckable interface {
	HealthCheck(ctx context.Context) error
}

func NewProvider(ctx context.Context, cfg config.ObjectStorageProviderConfig) (Provider, error) {
	switch strings.TrimSpace(cfg.Kind) {
	case "tencent-cos", "s3-compatible":
		return newS3Provider(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported storage provider kind %q", cfg.Kind)
	}
}

func ValidateProvider(ctx context.Context, cfg config.ObjectStorageProviderConfig) error {
	provider, err := NewProvider(ctx, cfg)
	if err != nil {
		return err
	}

	checker, ok := provider.(HealthCheckable)
	if !ok {
		return nil
	}

	return checker.HealthCheck(ctx)
}

func ValidateConfiguredProviders(ctx context.Context, cfg config.AppConfig) error {
	providers := append([]config.ObjectStorageProviderConfig{cfg.StorageProviders.Primary}, cfg.StorageProviders.Backup...)
	for _, providerCfg := range providers {
		if err := ValidateProvider(ctx, providerCfg); err != nil {
			return fmt.Errorf("%s: %w", providerCfg.Name, err)
		}
	}

	return nil
}
