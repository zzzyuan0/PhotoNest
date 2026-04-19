package ai

import (
	"context"
	"time"
)

type Provider interface {
	Name() string
	Boundary() Boundary
	Capabilities() []Capability
	Health(ctx context.Context) (ProviderStatus, error)
	Caption(ctx context.Context, request CaptionRequest) (CaptionResult, error)
	OCR(ctx context.Context, request OCRRequest) (OCRResult, error)
	Embedding(ctx context.Context, request EmbeddingRequest) (EmbeddingResult, error)
}

type Capability string

const (
	CapabilityCaption   Capability = "caption"
	CapabilityOCR       Capability = "ocr"
	CapabilityEmbedding Capability = "embedding"
)

type ProviderStatus struct {
	Boundary  Boundary
	Healthy   bool
	CheckedAt time.Time
	Message   string
}

type CaptionRequest struct {
	AssetID      string
	ObjectURL    string
	Locale       string
	FileName     string
	ModelProfile string
}

type CaptionResult struct {
	Text       string
	Confidence float64
	RawID      string
	Metadata   InvocationMetadata
	Signals    SemanticSignals
}

type OCRRequest struct {
	AssetID      string
	ObjectURL    string
	Locale       string
	FileName     string
	ModelProfile string
}

type OCRResult struct {
	TextBlocks []OCRBlock
	RawID      string
	Metadata   InvocationMetadata
}

type OCRBlock struct {
	Text  string
	Score float64
}

type EmbeddingRequest struct {
	AssetID   string
	ObjectURL string
	Model     string
	FileName  string
}

type EmbeddingResult struct {
	Vector   []float32
	RawID    string
	Metadata InvocationMetadata
}

type InvocationMetadata struct {
	ModelProfile string
	Model        string
}

type SemanticSignals struct {
	PeopleCount   string
	Presentations []string
	Scenes        []string
	Activities    []string
}

type ProfileResolver interface {
	ResolveModelProfile(capability Capability, requestedProfile string) InvocationMetadata
}
