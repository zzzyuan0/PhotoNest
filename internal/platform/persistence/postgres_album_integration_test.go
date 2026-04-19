package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/platform/persistence"
	"github.com/photonest/photonest/internal/provider/storage"
	"github.com/photonest/photonest/internal/testsupport"
)

func TestPostgresRepositoryCreateAlbumGeneratesIDAndPersists(t *testing.T) {
	dbCfg, cleanup := testsupport.NewPostgresDatabase(t)
	defer cleanup()

	db := testsupport.OpenPostgres(t, dbCfg)
	repository := persistence.NewPostgresRepository(db)

	discoveryService, err := discovery.NewService(discovery.ServiceConfig{
		Repository:  repository,
		Storage:     storage.NewMemoryProvider("test-memory", "photonest-main", "libraries/main"),
		DownloadTTL: 5 * time.Minute,
		TokenKey:    "test-token",
		Now: func() time.Time {
			return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}

	ctx := context.Background()
	libraryID := "11111111-1111-1111-1111-111111111111"

	created, err := discoveryService.CreateAlbum(ctx, libraryID, "旅行回忆")
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected postgres to generate album id")
	}
	if created.LibraryID != libraryID {
		t.Fatalf("expected library id %s, got %s", libraryID, created.LibraryID)
	}
	if created.Slug == "" {
		t.Fatal("expected album slug to be generated")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close first repository connection: %v", err)
	}

	restartedDB := testsupport.OpenPostgres(t, dbCfg)
	defer restartedDB.Close()

	restartedRepository := persistence.NewPostgresRepository(restartedDB)
	reloaded, err := restartedRepository.GetAlbum(ctx, created.ID)
	if err != nil {
		t.Fatalf("get album after restart: %v", err)
	}
	if reloaded.ID != created.ID {
		t.Fatalf("expected album id %s after restart, got %s", created.ID, reloaded.ID)
	}
	if reloaded.DisplayName != "旅行回忆" {
		t.Fatalf("expected display name to persist, got %s", reloaded.DisplayName)
	}
	if reloaded.Kind != discovery.AlbumKindCurated {
		t.Fatalf("expected curated album, got %s", reloaded.Kind)
	}
}
