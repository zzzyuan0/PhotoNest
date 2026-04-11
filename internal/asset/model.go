package asset

import "time"

type ProcessingStage string

const (
	StageAccepted         ProcessingStage = "accepted"
	StageStored           ProcessingStage = "stored"
	StageDerivativesReady ProcessingStage = "derivatives_ready"
	StageMetadataReady    ProcessingStage = "metadata_ready"
	StageAIReady          ProcessingStage = "ai_ready"
	StageIndexed          ProcessingStage = "indexed"
	StagePartialFailure   ProcessingStage = "partial_failure"
)

type ObjectPurpose string

const (
	ObjectPurposeOriginal  ObjectPurpose = "original"
	ObjectPurposeThumbnail ObjectPurpose = "thumbnail"
	ObjectPurposePreview   ObjectPurpose = "preview"
	ObjectPurposeBackup    ObjectPurpose = "backup"
)

type Asset struct {
	ID                    string
	LibraryID             string
	MediaType             string
	OriginalFilename      string
	ContentSHA256         string
	PerceptualHash        string
	Width                 int
	Height                int
	DurationMS            int
	ImportedAt            time.Time
	CapturedAt            *time.Time
	ProcessingStage       ProcessingStage
	BackupStatus          string
	IsDuplicateExact      bool
	DuplicateCandidateOf  string
	RecognitionStatusNote string
}

type ObjectReference struct {
	ID             string
	AssetID        string
	ProviderName   string
	Bucket         string
	ObjectKey      string
	ObjectVersion  string
	ETag           string
	Purpose        ObjectPurpose
	ContentLength  int64
	ContentSHA256  string
	Metadata       map[string]string
	CreatedAt      time.Time
	Immutable      bool
}
