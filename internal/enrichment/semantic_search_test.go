package enrichment

import (
	"context"
	"testing"

	providerai "github.com/photonest/photonest/internal/provider/ai"
)

func TestSemanticTagsBecomeSearchableAndVisibleInDiscovery(t *testing.T) {
	ctx := context.Background()
	_, provider, ingestionService, enrichmentService, discoveryService := newTestServices(t, []providerai.Provider{
		providerai.NewDeterministicProvider("local-ai", providerai.BoundaryLocalSidecar, nil, "test-model"),
	})

	accepted := uploadAsset(t, ctx, ingestionService, provider, "solo-girl-inside-car-beach.png", nil)
	if err := enrichmentService.QueueAsset(ctx, accepted.Asset.ID); err != nil {
		t.Fatalf("queue asset: %v", err)
	}
	if _, err := enrichmentService.RunPending(ctx); err != nil {
		t.Fatalf("run pending: %v", err)
	}

	detail, err := discoveryService.GetAssetDetail(ctx, testLibraryID, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(detail.SemanticTags) == 0 {
		t.Fatalf("expected semantic tags in detail, got %+v", detail)
	}
	if !contains(detail.SemanticTags, "scene:beach") || !contains(detail.SemanticTags, "activity:inside-car") {
		t.Fatalf("expected normalized semantic tags, got %+v", detail.SemanticTags)
	}

	results, err := discoveryService.Search(ctx, testLibraryID, "girl beach in car", 10)
	if err != nil {
		t.Fatalf("search assets: %v", err)
	}
	if len(results) != 1 || results[0].Asset.ID != accepted.Asset.ID {
		t.Fatalf("expected semantic search to return asset, got %+v", results)
	}
	if !contains(results[0].SemanticTags, "scene:beach") {
		t.Fatalf("expected summary to expose semantic tags, got %+v", results[0].SemanticTags)
	}
}
