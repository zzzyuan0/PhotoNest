package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/photonest/photonest/internal/platform/config"
)

func BuildProviders(ctx context.Context, configs []config.AIProviderConfig) ([]Provider, error) {
	providers := make([]Provider, 0, len(configs))
	for _, cfg := range configs {
		provider, err := BuildProvider(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("build ai provider %s: %w", cfg.Name, err)
		}
		if provider != nil {
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

func BuildProvider(ctx context.Context, cfg config.AIProviderConfig) (Provider, error) {
	capabilities := make([]Capability, 0, len(cfg.Capabilities))
	for _, capability := range cfg.Capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case string(CapabilityCaption):
			capabilities = append(capabilities, CapabilityCaption)
		case string(CapabilityOCR):
			capabilities = append(capabilities, CapabilityOCR)
		case string(CapabilityEmbedding):
			capabilities = append(capabilities, CapabilityEmbedding)
		}
	}

	boundary := BoundaryRemoteService
	if strings.EqualFold(strings.TrimSpace(cfg.ExecutionBoundary), string(BoundaryLocalSidecar)) {
		boundary = BoundaryLocalSidecar
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "openai-compatible":
		token, err := cfg.Token.Resolve(ctx, config.ResolveOptions{})
		if err != nil {
			return nil, err
		}
		return NewOpenAICompatibleProvider(OpenAICompatibleProviderConfig{
			Name:           cfg.Name,
			Endpoint:       cfg.Endpoint,
			Token:          token,
			Timeout:        cfg.Timeout,
			Boundary:       boundary,
			Capabilities:   capabilities,
			HealthCheckURL: cfg.HealthCheckURL,
			DefaultProfile: cfg.ModelProfile,
			Models:         cfg.Models,
		})
	case "deterministic", "":
		model := strings.TrimSpace(cfg.Model)
		if model == "" {
			model = strings.TrimSpace(cfg.Models[cfg.ModelProfile])
		}
		return NewDeterministicProvider(cfg.Name, boundary, capabilities, model), nil
	default:
		return nil, fmt.Errorf("unsupported ai provider kind %q", cfg.Kind)
	}
}
