package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAICompatibleProviderSupportsCaptionAndOCR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/chat/completions":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			model := request["model"].(string)
			messages := request["messages"].([]any)
			user := messages[1].(map[string]any)
			content := user["content"].([]any)
			textPrompt := content[0].(map[string]any)["text"].(string)

			response := map[string]any{
				"id": "cmpl-test",
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"text":"Hello Beach"}`,
					},
				}},
			}
			if model != "vision-plus" {
				t.Fatalf("expected model vision-plus, got %s", model)
			}
			if textPrompt != "" && textPrompt[:7] == "Analyze" {
				response["choices"] = []map[string]any{{
					"message": map[string]any{
						"content": `{"caption":"woman walking on the beach","peopleCount":"single","presentations":["female"],"scenes":["beach"],"activities":["walking"]}`,
					},
				}}
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleProviderConfig{
		Name:           "vision",
		Endpoint:       server.URL,
		Token:          "test-token",
		Timeout:        2 * time.Second,
		Boundary:       BoundaryRemoteService,
		Capabilities:   []Capability{CapabilityCaption, CapabilityOCR},
		HealthCheckURL: server.URL + "/models",
		DefaultProfile: "default",
		Models: map[string]string{
			"default": "vision-plus",
			"budget":  "vision-flash",
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	caption, err := provider.Caption(context.Background(), CaptionRequest{
		AssetID:   "asset-1",
		ObjectURL: "https://example.com/photo.png",
	})
	if err != nil {
		t.Fatalf("caption: %v", err)
	}
	if caption.Metadata.ModelProfile != "default" || caption.Metadata.Model != "vision-plus" {
		t.Fatalf("unexpected caption metadata: %+v", caption.Metadata)
	}
	if caption.Text != "woman walking on the beach" {
		t.Fatalf("unexpected caption text %q", caption.Text)
	}
	if len(NormalizeSemanticTags(caption.Signals, caption.Text)) == 0 {
		t.Fatalf("expected semantic tags from caption, got %+v", caption.Signals)
	}

	ocr, err := provider.OCR(context.Background(), OCRRequest{
		AssetID:      "asset-1",
		ObjectURL:    "https://example.com/photo.png",
		ModelProfile: "default",
	})
	if err != nil {
		t.Fatalf("ocr: %v", err)
	}
	if len(ocr.TextBlocks) != 1 || ocr.TextBlocks[0].Text != "Hello Beach" {
		t.Fatalf("unexpected ocr result: %+v", ocr)
	}
}

func TestOpenAICompatibleProviderClassifiesRemoteFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleProviderConfig{
		Name:           "vision",
		Endpoint:       server.URL,
		Token:          "test-token",
		Timeout:        2 * time.Second,
		Boundary:       BoundaryRemoteService,
		Capabilities:   []Capability{CapabilityCaption},
		DefaultProfile: "default",
		Models: map[string]string{
			"default": "vision-plus",
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.Caption(context.Background(), CaptionRequest{
		AssetID:   "asset-1",
		ObjectURL: "https://example.com/photo.png",
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	classified := ClassifyError(err, BoundaryRemoteService)
	if classified.Kind != ErrorKindRateLimited {
		t.Fatalf("expected rate limited classification, got %+v", classified)
	}
}
