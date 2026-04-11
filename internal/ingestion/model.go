package ingestion

import "time"

type Source string

const (
	SourceWebUpload    Source = "web_upload"
	SourceDesktopBatch Source = "desktop_batch"
	SourceExportImport Source = "export_restore"
)

type SessionStatus string

const (
	SessionDraft          SessionStatus = "draft"
	SessionAwaitingUpload SessionStatus = "awaiting_upload"
	SessionUploaded       SessionStatus = "uploaded"
	SessionConfirmed      SessionStatus = "confirmed"
	SessionFailed         SessionStatus = "failed"
)

type ImportSession struct {
	ID               string
	LibraryID        string
	Source           Source
	Status           SessionStatus
	ExpectedItemCount int
	Note             string
	CreatedBy        string
	ExpiresAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ImportItem struct {
	ID             string
	SessionID      string
	AssetID        string
	ObjectKey      string
	OriginalName   string
	ContentType    string
	ContentLength  int64
	ContentSHA256  string
	ETag           string
	Multipart      bool
	ConfirmedAt    *time.Time
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
