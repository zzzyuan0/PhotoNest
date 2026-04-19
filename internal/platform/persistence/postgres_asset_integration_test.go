package persistence_test

import (
	"context"
	"testing"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/platform/persistence"
	"github.com/photonest/photonest/internal/testsupport"
)

func TestPostgresRepositorySaveAssetAllowsEmptyDuplicateCandidate(t *testing.T) {
	dbCfg, cleanup := testsupport.NewPostgresDatabase(t)
	defer cleanup()

	db := testsupport.OpenPostgres(t, dbCfg)
	defer db.Close()

	repository := persistence.NewPostgresRepository(db)
	ctx := context.Background()

	record, err := repository.CreateAsset(ctx, asset.Asset{
		LibraryID:        "11111111-1111-1111-1111-111111111111",
		MediaType:        "image/png",
		OriginalFilename: "regression.png",
		ContentSHA256:    "sha256-regression",
		ProcessingStage:  asset.StageDerivativesReady,
		BackupStatus:     "pending",
	})
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}

	record.DuplicateCandidateOf = ""
	record.ProcessingStage = asset.StageMetadataReady
	record.BackupStatus = "verified"

	if err := repository.SaveAsset(ctx, record); err != nil {
		t.Fatalf("save asset with empty duplicate candidate: %v", err)
	}

	reloaded, err := repository.GetAsset(ctx, record.ID)
	if err != nil {
		t.Fatalf("reload asset: %v", err)
	}
	if reloaded.ProcessingStage != asset.StageMetadataReady {
		t.Fatalf("expected processing stage %s, got %s", asset.StageMetadataReady, reloaded.ProcessingStage)
	}
	if reloaded.BackupStatus != "verified" {
		t.Fatalf("expected backup status verified, got %s", reloaded.BackupStatus)
	}
	if reloaded.DuplicateCandidateOf != "" {
		t.Fatalf("expected empty duplicate candidate after reload, got %s", reloaded.DuplicateCandidateOf)
	}
}
