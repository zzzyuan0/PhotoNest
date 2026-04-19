package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type DeterministicProvider struct {
	name         string
	boundary     Boundary
	capabilities []Capability
	model        string
}

func NewDeterministicProvider(name string, boundary Boundary, capabilities []Capability, model string) Provider {
	if len(capabilities) == 0 {
		capabilities = []Capability{CapabilityCaption, CapabilityOCR, CapabilityEmbedding}
	}
	if strings.TrimSpace(model) == "" {
		model = "deterministic-v1"
	}

	return &DeterministicProvider{
		name:         fallback(name, "deterministic-ai"),
		boundary:     boundary,
		capabilities: append([]Capability(nil), capabilities...),
		model:        model,
	}
}

func (p *DeterministicProvider) Name() string {
	return p.name
}

func (p *DeterministicProvider) Boundary() Boundary {
	return p.boundary
}

func (p *DeterministicProvider) Capabilities() []Capability {
	return append([]Capability(nil), p.capabilities...)
}

func (p *DeterministicProvider) Health(_ context.Context) (ProviderStatus, error) {
	return ProviderStatus{
		Boundary:  p.boundary,
		Healthy:   true,
		CheckedAt: time.Now().UTC(),
		Message:   "deterministic provider is always healthy",
	}, nil
}

func (p *DeterministicProvider) Caption(_ context.Context, request CaptionRequest) (CaptionResult, error) {
	tokens := KeywordTokens(request.FileName)
	text := "photo asset"
	if len(tokens) > 0 {
		text = "photo of " + strings.Join(tokens, " ")
	}
	metadata := p.ResolveModelProfile(CapabilityCaption, request.ModelProfile)

	return CaptionResult{
		Text:       text,
		Confidence: 0.66,
		RawID:      fmt.Sprintf("%s:caption:%s", p.name, request.AssetID),
		Metadata:   metadata,
		Signals:    InferSemanticSignals(text, request.FileName),
	}, nil
}

func (p *DeterministicProvider) OCR(_ context.Context, request OCRRequest) (OCRResult, error) {
	tokens := KeywordTokens(request.FileName)
	text := "image"
	if len(tokens) > 0 {
		text = strings.Join(tokens, " ")
	}

	return OCRResult{
		TextBlocks: []OCRBlock{{
			Text:  text,
			Score: 0.52,
		}},
		RawID:    fmt.Sprintf("%s:ocr:%s", p.name, request.AssetID),
		Metadata: p.ResolveModelProfile(CapabilityOCR, request.ModelProfile),
	}, nil
}

func (p *DeterministicProvider) Embedding(_ context.Context, request EmbeddingRequest) (EmbeddingResult, error) {
	model := request.Model
	if strings.TrimSpace(model) == "" {
		model = p.model
	}

	return EmbeddingResult{
		Vector: HashEmbeddingText(request.FileName+" "+model, 1536),
		RawID:  fmt.Sprintf("%s:embedding:%s", p.name, request.AssetID),
		Metadata: InvocationMetadata{
			ModelProfile: firstNonEmptyTrimmed(request.Model, "default"),
			Model:        model,
		},
	}, nil
}

func (p *DeterministicProvider) ResolveModelProfile(_ Capability, requestedProfile string) InvocationMetadata {
	profile := firstNonEmptyTrimmed(requestedProfile, "default")
	return InvocationMetadata{
		ModelProfile: profile,
		Model:        p.model,
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func fallback(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}
