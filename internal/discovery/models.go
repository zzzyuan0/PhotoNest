package discovery

import "time"

type AlbumKind string

const (
	AlbumKindFavorites       AlbumKind = "favorites"
	AlbumKindCurated         AlbumKind = "curated"
	AlbumKindDuplicatesReview AlbumKind = "duplicates_review"
)

type Album struct {
	ID          string
	LibraryID   string
	Slug        string
	DisplayName string
	Kind        AlbumKind
	AssetCount  int
	CreatedAt   time.Time
}

type PlaceSummary struct {
	Label        string
	Count        int
	LatestAsset  Summary
	LatestAt     time.Time
}

type DuplicateCandidate struct {
	Primary   Summary
	Candidate Summary
	Exact     bool
}

type TimelineQuery struct {
	Limit        int
	DateFrom     *time.Time
	DateTo       *time.Time
	Location     string
	Stage        string
	BackupStatus string
}
