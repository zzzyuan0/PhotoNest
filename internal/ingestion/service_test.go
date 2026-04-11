package ingestion

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/provider/storage"
)

func TestServiceHandlesResignDuplicatesAndDerivatives(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	service, err := NewService(ServiceConfig{
		Repository: store,
		Provider:   provider,
		ProviderConfig: config.ObjectStorageProviderConfig{
			Name:             "primary-memory",
			Bucket:           "photonest-main",
			KeyPrefix:        "libraries/main",
			UploadPresignTTL: storage.MaxUploadTTL,
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	firstPayload := mustPNG(t, 0x28, 0x66, 0xa3)
	firstSHA := SHA256Hex(firstPayload)
	firstSession, err := service.CreateSession(ctx, CreateSessionInput{
		LibraryID:         "11111111-1111-1111-1111-111111111111",
		Source:            SourceWebUpload,
		ExpectedItemCount: 1,
		CreatedBy:         "tester",
	})
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}

	firstTicket, err := service.CreateUploadTicket(ctx, CreateUploadTicketInput{
		SessionID:     firstSession.ID,
		LibraryID:     firstSession.LibraryID,
		FileName:      "first.png",
		ContentType:   "image/png",
		ContentLength: int64(len(firstPayload)),
		ContentSHA256: firstSHA,
	})
	if err != nil {
		t.Fatalf("create first ticket: %v", err)
	}
	resigned, err := service.CreateUploadTicket(ctx, CreateUploadTicketInput{
		SessionID:     firstSession.ID,
		LibraryID:     firstSession.LibraryID,
		FileName:      "first.png",
		ContentType:   "image/png",
		ContentLength: int64(len(firstPayload)),
		ContentSHA256: firstSHA,
	})
	if err != nil {
		t.Fatalf("reissue first ticket: %v", err)
	}
	if firstTicket.Plan.ObjectKey != resigned.Plan.ObjectKey {
		t.Fatalf("expected reissued ticket to reuse object key, got %s vs %s", firstTicket.Plan.ObjectKey, resigned.Plan.ObjectKey)
	}
	putWithPlan(t, ctx, provider, firstTicket.Plan, firstPayload, "image/png")

	firstResult, err := service.ConfirmUpload(ctx, ConfirmUploadInput{
		SessionID:     firstSession.ID,
		LibraryID:     firstSession.LibraryID,
		ObjectKey:     firstTicket.Plan.ObjectKey,
		ContentLength: int64(len(firstPayload)),
		ContentSHA256: firstSHA,
	})
	if err != nil {
		t.Fatalf("confirm first upload: %v", err)
	}
	if firstResult.ExactDuplicate {
		t.Fatal("first upload must not be treated as exact duplicate")
	}
	if firstResult.Asset.ProcessingStage != asset.StageDerivativesReady {
		t.Fatalf("expected derivatives_ready stage, got %s", firstResult.Asset.ProcessingStage)
	}
	objectRefs, err := store.ListObjectReferencesByAsset(ctx, firstResult.Asset.ID)
	if err != nil {
		t.Fatalf("list object refs: %v", err)
	}
	if len(objectRefs) < 3 {
		t.Fatalf("expected original + thumbnail + preview refs, got %d", len(objectRefs))
	}

	duplicateSession, err := service.CreateSession(ctx, CreateSessionInput{
		LibraryID:         firstSession.LibraryID,
		Source:            SourceWebUpload,
		ExpectedItemCount: 1,
		CreatedBy:         "tester",
	})
	if err != nil {
		t.Fatalf("create duplicate session: %v", err)
	}
	duplicateTicket, err := service.CreateUploadTicket(ctx, CreateUploadTicketInput{
		SessionID:     duplicateSession.ID,
		LibraryID:     duplicateSession.LibraryID,
		FileName:      "duplicate.png",
		ContentType:   "image/png",
		ContentLength: int64(len(firstPayload)),
		ContentSHA256: firstSHA,
	})
	if err != nil {
		t.Fatalf("create duplicate ticket: %v", err)
	}
	putWithPlan(t, ctx, provider, duplicateTicket.Plan, firstPayload, "image/png")

	duplicateResult, err := service.ConfirmUpload(ctx, ConfirmUploadInput{
		SessionID:     duplicateSession.ID,
		LibraryID:     duplicateSession.LibraryID,
		ObjectKey:     duplicateTicket.Plan.ObjectKey,
		ContentLength: int64(len(firstPayload)),
		ContentSHA256: firstSHA,
	})
	if err != nil {
		t.Fatalf("confirm duplicate upload: %v", err)
	}
	if !duplicateResult.ExactDuplicate {
		t.Fatal("expected second upload to be treated as exact duplicate")
	}
	if duplicateResult.Asset.ID != firstResult.Asset.ID {
		t.Fatalf("expected duplicate to resolve to existing asset %s, got %s", firstResult.Asset.ID, duplicateResult.Asset.ID)
	}

	similarPayload := mustPNG(t, 0x2a, 0x66, 0xa3)
	similarSession, err := service.CreateSession(ctx, CreateSessionInput{
		LibraryID:         firstSession.LibraryID,
		Source:            SourceWebUpload,
		ExpectedItemCount: 1,
		CreatedBy:         "tester",
	})
	if err != nil {
		t.Fatalf("create similar session: %v", err)
	}
	similarTicket, err := service.CreateUploadTicket(ctx, CreateUploadTicketInput{
		SessionID:     similarSession.ID,
		LibraryID:     similarSession.LibraryID,
		FileName:      "similar.png",
		ContentType:   "image/png",
		ContentLength: int64(len(similarPayload)),
		ContentSHA256: SHA256Hex(similarPayload),
	})
	if err != nil {
		t.Fatalf("create similar ticket: %v", err)
	}
	putWithPlan(t, ctx, provider, similarTicket.Plan, similarPayload, "image/png")

	similarResult, err := service.ConfirmUpload(ctx, ConfirmUploadInput{
		SessionID:     similarSession.ID,
		LibraryID:     similarSession.LibraryID,
		ObjectKey:     similarTicket.Plan.ObjectKey,
		ContentLength: int64(len(similarPayload)),
		ContentSHA256: SHA256Hex(similarPayload),
	})
	if err != nil {
		t.Fatalf("confirm similar upload: %v", err)
	}
	if similarResult.Asset.ID == firstResult.Asset.ID {
		t.Fatal("similar upload must create a new asset instead of collapsing into exact duplicate")
	}
	if similarResult.Asset.DuplicateCandidateOf != firstResult.Asset.ID {
		t.Fatalf("expected duplicate candidate to point at %s, got %s", firstResult.Asset.ID, similarResult.Asset.DuplicateCandidateOf)
	}
}

func putWithPlan(t *testing.T, ctx context.Context, provider storage.Provider, plan UploadPlan, payload []byte, contentType string) {
	t.Helper()

	metadata := mapFromPresignHeaders(plan.Headers)
	if _, err := provider.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: plan.ObjectKey,
		},
		Body:          bytes.NewReader(payload),
		ContentType:   contentType,
		ContentLength: int64(len(payload)),
		Metadata:      metadata,
	}); err != nil {
		t.Fatalf("put object with plan: %v", err)
	}
}

func mapFromPresignHeaders(headers map[string]string) map[string]string {
	metadata := map[string]string{}
	for name, value := range headers {
		lowered := strings.ToLower(name)
		switch {
		case strings.HasPrefix(lowered, "x-amz-meta-"):
			metadata[strings.TrimPrefix(lowered, "x-amz-meta-")] = value
		case strings.HasPrefix(lowered, "x-cos-meta-"):
			metadata[strings.TrimPrefix(lowered, "x-cos-meta-")] = value
		}
	}
	return metadata
}

func mustPNG(t *testing.T, red uint8, green uint8, blue uint8) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{R: red, G: green + uint8((x+y)%3), B: blue, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}
