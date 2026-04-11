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
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type ImportItem struct {
	ID             string
	SessionID      string
	ObjectKey      string
	OriginalName   string
	ContentType    string
	ContentLength  int64
	ContentSHA256  string
	Multipart      bool
	ConfirmedAt    *time.Time
	FailureReason  string
}
