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

type RecognitionStage string

const (
	RecognitionStageDerivatives RecognitionStage = "derivatives"
	RecognitionStageMetadata    RecognitionStage = "metadata"
	RecognitionStageCaption     RecognitionStage = "caption"
	RecognitionStageOCR         RecognitionStage = "ocr"
	RecognitionStageEmbedding   RecognitionStage = "embedding"
	RecognitionStageIndexing    RecognitionStage = "indexing"
	RecognitionStageBackup      RecognitionStage = "backup"
)

type RecognitionStatus string

const (
	RecognitionStatusPending   RecognitionStatus = "pending"
	RecognitionStatusRunning   RecognitionStatus = "running"
	RecognitionStatusSucceeded RecognitionStatus = "succeeded"
	RecognitionStatusFailed    RecognitionStatus = "failed"
	RecognitionStatusSkipped   RecognitionStatus = "skipped"
)

type ObjectPurpose string

const (
	ObjectPurposeOriginal  ObjectPurpose = "original"
	ObjectPurposeThumbnail ObjectPurpose = "thumbnail"
	ObjectPurposePreview   ObjectPurpose = "preview"
	ObjectPurposeBackup    ObjectPurpose = "backup"
)

type RecognitionRun struct {
	ID             string
	AssetID        string
	Stage          RecognitionStage
	ProviderName   string
	Status         RecognitionStatus
	PolicyReason   string
	Attempts       int
	StartedAt      time.Time
	FinishedAt     *time.Time
	DebugExpiresAt *time.Time
	DebugPayload   map[string]any
	LastError      string
}

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
	TimelineAt            time.Time
	DeviceMake            string
	DeviceModel           string
	GPSLatitude           *float64
	GPSLongitude          *float64
	LocationLabel         string
	CaptionText           string
	OCRText               string
	Tags                  []string
	Embedding             []float32
	SearchDocument        string
	SearchEmbedding       []float32
	IndexedAt             *time.Time
	ProcessingStage       ProcessingStage
	BackupStatus          string
	IsDuplicateExact      bool
	DuplicateCandidateOf  string
	RecognitionStatusNote string
}

type ObjectReference struct {
	ID            string
	AssetID       string
	ProviderName  string
	Bucket        string
	ObjectKey     string
	ObjectVersion string
	ETag          string
	Purpose       ObjectPurpose
	ContentLength int64
	ContentSHA256 string
	Metadata      map[string]string
	CreatedAt     time.Time
	Immutable     bool
}
