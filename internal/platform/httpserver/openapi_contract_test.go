package httpserver

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAPIContractIncludesDiscoveryAndExportEndpoints(t *testing.T) {
	content, err := os.ReadFile("../../../openapi/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(content)

	for _, required := range []string{
		"/api/v1/discovery/places",
		"/api/v1/discovery/duplicates",
		"/api/v1/albums",
		"/api/v1/assets/{assetId}/favorite",
		"AlbumDetailResponse",
		"RecoveryPlan",
		"redactedManifestUrl",
	} {
		if !strings.Contains(spec, required) {
			t.Fatalf("expected openapi spec to contain %q", required)
		}
	}
}
