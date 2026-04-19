package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleProviderConfig struct {
	Name           string
	Endpoint       string
	Token          string
	Timeout        time.Duration
	Boundary       Boundary
	Capabilities   []Capability
	HealthCheckURL string
	DefaultProfile string
	Models         map[string]string
	HTTPClient     *http.Client
}

type OpenAICompatibleProvider struct {
	name           string
	endpoint       string
	token          string
	timeout        time.Duration
	boundary       Boundary
	capabilities   []Capability
	healthCheckURL string
	defaultProfile string
	models         map[string]string
	httpClient     *http.Client
}

func NewOpenAICompatibleProvider(cfg OpenAICompatibleProviderConfig) (Provider, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if len(cfg.Capabilities) == 0 {
		cfg.Capabilities = []Capability{CapabilityCaption, CapabilityOCR}
	}
	if strings.TrimSpace(cfg.DefaultProfile) == "" {
		cfg.DefaultProfile = "default"
	}
	models := map[string]string{}
	for key, value := range cfg.Models {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		models[trimmedKey] = trimmedValue
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("at least one model profile is required")
	}
	if _, ok := models[cfg.DefaultProfile]; !ok {
		return nil, fmt.Errorf("default profile %q is not configured", cfg.DefaultProfile)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}

	return &OpenAICompatibleProvider{
		name:           strings.TrimSpace(cfg.Name),
		endpoint:       strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"),
		token:          strings.TrimSpace(cfg.Token),
		timeout:        cfg.Timeout,
		boundary:       cfg.Boundary,
		capabilities:   append([]Capability(nil), cfg.Capabilities...),
		healthCheckURL: strings.TrimSpace(cfg.HealthCheckURL),
		defaultProfile: strings.TrimSpace(cfg.DefaultProfile),
		models:         models,
		httpClient:     client,
	}, nil
}

func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

func (p *OpenAICompatibleProvider) Boundary() Boundary {
	return p.boundary
}

func (p *OpenAICompatibleProvider) Capabilities() []Capability {
	return append([]Capability(nil), p.capabilities...)
}

func (p *OpenAICompatibleProvider) Health(ctx context.Context) (ProviderStatus, error) {
	target := p.healthCheckURL
	if target == "" {
		target = p.endpoint + "/models"
	}
	req, err := http.NewRequestWithContext(p.withTimeout(ctx), http.MethodGet, target, nil)
	if err != nil {
		return ProviderStatus{Boundary: p.boundary, CheckedAt: time.Now().UTC()}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return ProviderStatus{
			Boundary:  p.boundary,
			Healthy:   false,
			CheckedAt: time.Now().UTC(),
			Message:   err.Error(),
		}, err
	}
	defer resp.Body.Close()

	status := ProviderStatus{
		Boundary:  p.boundary,
		Healthy:   resp.StatusCode >= 200 && resp.StatusCode < 300,
		CheckedAt: time.Now().UTC(),
		Message:   resp.Status,
	}
	if status.Healthy {
		status.Message = "openai-compatible provider reachable"
		return status, nil
	}
	return status, fmt.Errorf("health check returned %s", resp.Status)
}

func (p *OpenAICompatibleProvider) Caption(ctx context.Context, request CaptionRequest) (CaptionResult, error) {
	metadata := p.ResolveModelProfile(CapabilityCaption, request.ModelProfile)
	prompt := "Analyze the image and return compact JSON with keys: caption, peopleCount, presentations, scenes, activities. Use short English values. Do not include markdown."
	response, rawID, err := p.chatCompletion(ctx, metadata.Model, prompt, request.ObjectURL, true)
	if err != nil {
		return CaptionResult{}, err
	}

	payload := struct {
		Caption       string   `json:"caption"`
		PeopleCount   string   `json:"peopleCount"`
		Presentations []string `json:"presentations"`
		Scenes        []string `json:"scenes"`
		Activities    []string `json:"activities"`
	}{}
	if err := json.Unmarshal([]byte(response), &payload); err != nil {
		payload.Caption = strings.TrimSpace(response)
		payloadFromText := InferSemanticSignals(response)
		payload.PeopleCount = payloadFromText.PeopleCount
		payload.Presentations = payloadFromText.Presentations
		payload.Scenes = payloadFromText.Scenes
		payload.Activities = payloadFromText.Activities
	}

	return CaptionResult{
		Text:       strings.TrimSpace(payload.Caption),
		Confidence: 0.85,
		RawID:      rawID,
		Metadata:   metadata,
		Signals: SemanticSignals{
			PeopleCount:   payload.PeopleCount,
			Presentations: payload.Presentations,
			Scenes:        payload.Scenes,
			Activities:    payload.Activities,
		},
	}, nil
}

func (p *OpenAICompatibleProvider) OCR(ctx context.Context, request OCRRequest) (OCRResult, error) {
	metadata := p.ResolveModelProfile(CapabilityOCR, request.ModelProfile)
	prompt := "Extract all visible text from the image. Return compact JSON with a single key named text. Preserve line breaks where helpful."
	response, rawID, err := p.chatCompletion(ctx, metadata.Model, prompt, request.ObjectURL, true)
	if err != nil {
		return OCRResult{}, err
	}

	payload := struct {
		Text string `json:"text"`
	}{}
	if err := json.Unmarshal([]byte(response), &payload); err != nil {
		payload.Text = strings.TrimSpace(response)
	}

	blocks := []OCRBlock{}
	for _, line := range strings.Split(strings.TrimSpace(payload.Text), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			blocks = append(blocks, OCRBlock{Text: trimmed, Score: 0.8})
		}
	}
	return OCRResult{
		TextBlocks: blocks,
		RawID:      rawID,
		Metadata:   metadata,
	}, nil
}

func (p *OpenAICompatibleProvider) Embedding(_ context.Context, request EmbeddingRequest) (EmbeddingResult, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = p.models[p.defaultProfile]
	}
	return EmbeddingResult{
		Vector: HashEmbeddingText(request.FileName+" "+model, 1536),
		RawID:  fmt.Sprintf("%s:embedding:%s", p.name, request.AssetID),
		Metadata: InvocationMetadata{
			ModelProfile: p.defaultProfile,
			Model:        model,
		},
	}, nil
}

func (p *OpenAICompatibleProvider) ResolveModelProfile(_ Capability, requestedProfile string) InvocationMetadata {
	profile := strings.TrimSpace(requestedProfile)
	if profile == "" {
		profile = p.defaultProfile
	}
	model := strings.TrimSpace(p.models[profile])
	if model == "" {
		profile = p.defaultProfile
		model = p.models[profile]
	}
	return InvocationMetadata{
		ModelProfile: profile,
		Model:        model,
	}
}

func (p *OpenAICompatibleProvider) chatCompletion(
	ctx context.Context,
	model string,
	prompt string,
	imageURL string,
	requireJSON bool,
) (string, string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are a precise image understanding assistant.",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]any{
							"url": imageURL,
						},
					},
				},
			},
		},
	}
	if requireJSON {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(p.withTimeout(ctx), http.MethodPost, p.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", classifyHTTPStatus(resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var completion struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", "", fmt.Errorf("decode completion response: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", "", fmt.Errorf("completion response did not include choices")
	}
	content := extractMessageContent(completion.Choices[0].Message.Content)
	if strings.TrimSpace(content) == "" {
		return "", "", fmt.Errorf("completion response was empty")
	}
	return content, strings.TrimSpace(completion.ID), nil
}

func (p *OpenAICompatibleProvider) withTimeout(ctx context.Context) context.Context {
	timeoutCtx, _ := context.WithTimeout(ctx, p.timeout)
	return timeoutCtx
}

func classifyHTTPStatus(statusCode int, message string) error {
	err := fmt.Errorf("provider returned status %d: %s", statusCode, strings.TrimSpace(message))
	switch {
	case statusCode == http.StatusTooManyRequests:
		return ClassifiedError{Kind: ErrorKindRateLimited, Boundary: BoundaryRemoteService, Message: err.Error(), Retryable: true, Temporary: true, Cause: err}
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return ClassifiedError{Kind: ErrorKindProviderUnavailable, Boundary: BoundaryRemoteService, Message: err.Error(), Retryable: false, Temporary: false, Cause: err}
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout:
		return ClassifiedError{Kind: ErrorKindTimeout, Boundary: BoundaryRemoteService, Message: err.Error(), Retryable: true, Temporary: true, Cause: err}
	case statusCode >= 500:
		return ClassifiedError{Kind: ErrorKindTransient, Boundary: BoundaryRemoteService, Message: err.Error(), Retryable: true, Temporary: true, Cause: err}
	default:
		return err
	}
}

func extractMessageContent(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := entry["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
