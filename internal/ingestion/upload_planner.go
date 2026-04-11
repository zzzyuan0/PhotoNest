package ingestion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/provider/storage"
)

const defaultMultipartPartSize int64 = 8 << 20

type UploadPlanner struct {
	provider     storage.Provider
	providerName string
	bucket       string
	keyPrefix    string
	uploadTTL    time.Duration
}

type UploadIntent struct {
	FileName      string
	ContentType   string
	ContentLength int64
	ContentSHA256 string
	Multipart     bool
}

type UploadPlan struct {
	SessionID         string
	ObjectKey         string
	Method            string
	URL               string
	Headers           map[string]string
	FormFields        map[string]string
	ExpiresAt         string
	ChecksumAlgorithm string
	Multipart         *storage.MultipartUpload
}

type UploadValidationInput struct {
	SessionID      string
	LibraryID      string
	ObjectKey      string
	ContentLength  int64
	ETag           string
	ContentSHA256  string
}

func NewUploadPlanner(provider storage.Provider, cfg config.ObjectStorageProviderConfig) UploadPlanner {
	return UploadPlanner{
		provider:     provider,
		providerName: cfg.Name,
		bucket:       cfg.Bucket,
		keyPrefix:    cfg.KeyPrefix,
		uploadTTL:    cfg.UploadPresignTTL,
	}
}

func (p UploadPlanner) Plan(ctx context.Context, session ImportSession, intent UploadIntent) (UploadPlan, error) {
	return p.plan(ctx, session, intent, "")
}

func (p UploadPlanner) Reissue(ctx context.Context, session ImportSession, objectKey string, intent UploadIntent) (UploadPlan, error) {
	return p.plan(ctx, session, intent, objectKey)
}

func (p UploadPlanner) plan(ctx context.Context, session ImportSession, intent UploadIntent, objectKey string) (UploadPlan, error) {
	if strings.TrimSpace(session.ID) == "" {
		return UploadPlan{}, fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(session.LibraryID) == "" {
		return UploadPlan{}, fmt.Errorf("library id is required")
	}
	if strings.TrimSpace(intent.ContentType) == "" {
		return UploadPlan{}, fmt.Errorf("content type is required")
	}
	if intent.ContentLength <= 0 {
		return UploadPlan{}, fmt.Errorf("content length must be positive")
	}

	if strings.TrimSpace(objectKey) == "" {
		var err error
		objectKey, err = p.allocateObjectKey(session.LibraryID, session.ID)
		if err != nil {
			return UploadPlan{}, err
		}
	}

	headers := map[string]string{
		"x-amz-meta-photonest-session-id": session.ID,
		"x-amz-meta-photonest-library-id": session.LibraryID,
		"x-amz-meta-photonest-provider":   p.providerName,
	}
	if checksum := strings.TrimSpace(intent.ContentSHA256); checksum != "" {
		headers["x-amz-meta-photonest-content-sha256"] = checksum
	}

	ref := storage.ObjectRef{
		Bucket: p.bucket,
		Key:    objectKey,
	}

	if intent.Multipart {
		upload, err := p.provider.BeginMultipartUpload(ctx, storage.BeginMultipartUploadInput{
			Ref:         ref,
			ContentType: intent.ContentType,
			Metadata: map[string]string{
				"photonest-session-id":    session.ID,
				"photonest-library-id":    session.LibraryID,
				"photonest-provider":      p.providerName,
				"photonest-content-sha256": strings.TrimSpace(intent.ContentSHA256),
			},
			PartCount: p.estimatePartCount(intent.ContentLength),
			ExpiresIn: p.uploadTTL,
		})
		if err != nil {
			return UploadPlan{}, err
		}

		return UploadPlan{
			SessionID:         session.ID,
			ObjectKey:         objectKey,
			ExpiresAt:         upload.ExpiresAt.UTC().Format(time.RFC3339),
			ChecksumAlgorithm: checksumAlgorithm(intent.ContentSHA256),
			Multipart:         &upload,
		}, nil
	}

	presigned, err := p.provider.PresignUpload(ctx, storage.PresignUploadInput{
		Ref:           ref,
		ContentType:   intent.ContentType,
		ContentLength: intent.ContentLength,
		ExpiresIn:     p.uploadTTL,
		Headers:       headers,
	})
	if err != nil {
		return UploadPlan{}, err
	}

	return UploadPlan{
		SessionID:         session.ID,
		ObjectKey:         objectKey,
		Method:            presigned.Method,
		URL:               presigned.URL,
		Headers:           presigned.Headers,
		FormFields:        presigned.FormFields,
		ExpiresAt:         presigned.ExpiresAt.UTC().Format(time.RFC3339),
		ChecksumAlgorithm: checksumAlgorithm(intent.ContentSHA256),
	}, nil
}

func (p UploadPlanner) CompleteMultipartUpload(ctx context.Context, objectKey string, uploadID string, parts []storage.CompletedPart) (storage.ObjectInfo, error) {
	return p.provider.CompleteMultipartUpload(ctx, storage.CompleteMultipartUploadInput{
		Ref: storage.ObjectRef{
			Bucket: p.bucket,
			Key:    objectKey,
		},
		UploadID: uploadID,
		Parts:    parts,
	})
}

func (p UploadPlanner) ValidateUploadedObject(ctx context.Context, input UploadValidationInput) (storage.ObjectInfo, error) {
	ref := storage.ObjectRef{
		Bucket: p.bucket,
		Key:    input.ObjectKey,
	}
	info, err := p.provider.HeadObject(ctx, ref)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if info.ContentLength != input.ContentLength {
		return storage.ObjectInfo{}, fmt.Errorf("uploaded content length mismatch")
	}
	if expected := normalizeUploadETag(input.ETag); expected != "" && normalizeUploadETag(info.ETag) != expected {
		return storage.ObjectInfo{}, fmt.Errorf("uploaded etag mismatch")
	}
	if info.Metadata["photonest-session-id"] != strings.TrimSpace(input.SessionID) {
		return storage.ObjectInfo{}, fmt.Errorf("uploaded object session metadata mismatch")
	}
	if info.Metadata["photonest-library-id"] != strings.TrimSpace(input.LibraryID) {
		return storage.ObjectInfo{}, fmt.Errorf("uploaded object library metadata mismatch")
	}
	if checksum := strings.TrimSpace(input.ContentSHA256); checksum != "" && info.Metadata["photonest-content-sha256"] != checksum {
		return storage.ObjectInfo{}, fmt.Errorf("uploaded object checksum metadata mismatch")
	}

	return info, nil
}

func (p UploadPlanner) allocateObjectKey(libraryID string, sessionID string) (string, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}

	return path.Join(
		strings.Trim(p.keyPrefix, "/"),
		"imports",
		strings.TrimSpace(libraryID),
		strings.TrimSpace(sessionID),
		hex.EncodeToString(tokenBytes),
	), nil
}

func (p UploadPlanner) estimatePartCount(contentLength int64) int {
	parts := int((contentLength + defaultMultipartPartSize - 1) / defaultMultipartPartSize)
	if parts < 1 {
		return 1
	}

	return parts
}

func checksumAlgorithm(checksum string) string {
	if strings.TrimSpace(checksum) == "" {
		return ""
	}

	return "sha256"
}

func normalizeUploadETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"")
}
