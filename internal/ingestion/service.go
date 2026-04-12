package ingestion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/provider/storage"
)

var (
	ErrSessionExpired               = errors.New("import session expired")
	ErrSessionClosed                = errors.New("import session is closed")
	ErrSessionLibraryMismatch       = errors.New("import session does not belong to library")
	ErrUploadValidationFailed       = errors.New("uploaded object validation failed")
	ErrMultipartConfirmationMissing = errors.New("multipart upload confirmation payload is required")
	ErrMultipartConfirmationInvalid = errors.New("multipart upload confirmation payload is invalid")
	ErrUnexpectedMultipartPayload   = errors.New("multipart confirmation payload is only allowed for multipart uploads")
)

const (
	defaultSessionTTL       = 24 * time.Hour
	defaultSimilarityBudget = 8
)

type ServiceConfig struct {
	Repository          Repository
	Provider            storage.Provider
	ProviderConfig      config.ObjectStorageProviderConfig
	UploadCredentialTTL time.Duration
	SessionTTL          time.Duration
	SimilarityThreshold int
	Now                 func() time.Time
}

type Service struct {
	repository          Repository
	provider            storage.Provider
	planner             UploadPlanner
	providerName        string
	bucket              string
	sessionTTL          time.Duration
	similarityThreshold int
	now                 func() time.Time
}

type CreateSessionInput struct {
	LibraryID         string
	Source            Source
	ExpectedItemCount int
	Note              string
	CreatedBy         string
}

type CreateUploadTicketInput struct {
	SessionID     string
	LibraryID     string
	FileName      string
	ContentType   string
	ContentLength int64
	ContentSHA256 string
	Multipart     bool
}

type UploadTicket struct {
	Session ImportSession
	Item    ImportItem
	Plan    UploadPlan
}

type ConfirmUploadInput struct {
	SessionID     string
	LibraryID     string
	ObjectKey     string
	ContentLength int64
	ETag          string
	ContentSHA256 string
	UploadID      string
	Parts         []storage.CompletedPart
}

type ConfirmResult struct {
	Asset          asset.Asset
	ImportSession  ImportSession
	ExactDuplicate bool
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("storage provider is required")
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	uploadTTL := cfg.UploadCredentialTTL
	if uploadTTL <= 0 {
		uploadTTL = cfg.ProviderConfig.UploadPresignTTL
	}
	if uploadTTL <= 0 {
		uploadTTL = storage.MaxUploadTTL
	}
	if uploadTTL > storage.MaxUploadTTL {
		uploadTTL = storage.MaxUploadTTL
	}
	providerCfg := cfg.ProviderConfig
	providerCfg.UploadPresignTTL = uploadTTL

	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	similarityThreshold := cfg.SimilarityThreshold
	if similarityThreshold <= 0 {
		similarityThreshold = defaultSimilarityBudget
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		repository:          cfg.Repository,
		provider:            cfg.Provider,
		planner:             NewUploadPlanner(cfg.Provider, providerCfg),
		providerName:        providerCfg.Name,
		bucket:              providerCfg.Bucket,
		sessionTTL:          sessionTTL,
		similarityThreshold: similarityThreshold,
		now:                 now,
	}, nil
}

func (s *Service) Repository() Repository {
	return s.repository
}

func (s *Service) Provider() storage.Provider {
	return s.provider
}

func (s *Service) ProviderConfig() config.ObjectStorageProviderConfig {
	return s.planner.ProviderConfig()
}

func (s *Service) CreateSession(ctx context.Context, input CreateSessionInput) (ImportSession, error) {
	if strings.TrimSpace(input.LibraryID) == "" {
		return ImportSession{}, fmt.Errorf("library id is required")
	}
	if strings.TrimSpace(string(input.Source)) == "" {
		return ImportSession{}, fmt.Errorf("source is required")
	}

	now := s.now().UTC()
	record := ImportSession{
		ID:                newOpaqueID(),
		LibraryID:         strings.TrimSpace(input.LibraryID),
		Source:            input.Source,
		Status:            SessionAwaitingUpload,
		ExpectedItemCount: input.ExpectedItemCount,
		Note:              strings.TrimSpace(input.Note),
		CreatedBy:         strings.TrimSpace(input.CreatedBy),
		ExpiresAt:         now.Add(s.sessionTTL),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if record.ExpectedItemCount <= 0 {
		record.ExpectedItemCount = 1
	}

	return s.repository.CreateSession(ctx, record)
}

func (s *Service) CreateUploadTicket(ctx context.Context, input CreateUploadTicketInput) (UploadTicket, error) {
	session, err := s.getActiveSession(ctx, input.SessionID, input.LibraryID)
	if err != nil {
		return UploadTicket{}, err
	}
	intent := UploadIntent{
		FileName:      strings.TrimSpace(input.FileName),
		ContentType:   strings.TrimSpace(input.ContentType),
		ContentLength: input.ContentLength,
		ContentSHA256: normalizeContentSHA(input.ContentSHA256),
		Multipart:     input.Multipart,
	}

	item, found, err := s.repository.FindReusableItem(ctx, session.ID, ReusableItemLookup{
		OriginalName:  intent.FileName,
		ContentType:   intent.ContentType,
		ContentLength: intent.ContentLength,
		ContentSHA256: intent.ContentSHA256,
		Multipart:     intent.Multipart,
	})
	if err != nil {
		return UploadTicket{}, err
	}

	var plan UploadPlan
	if found {
		item.OriginalName = intent.FileName
		item.ContentType = intent.ContentType
		item.ContentLength = intent.ContentLength
		item.ContentSHA256 = intent.ContentSHA256
		item.Multipart = intent.Multipart
		item.FailureReason = ""
		item.UpdatedAt = s.now().UTC()
		plan, err = s.planner.Reissue(ctx, session, item.ObjectKey, intent)
		if err != nil {
			return UploadTicket{}, err
		}
		if err := s.repository.SaveItem(ctx, item); err != nil {
			return UploadTicket{}, err
		}
	} else {
		plan, err = s.planner.Plan(ctx, session, intent)
		if err != nil {
			return UploadTicket{}, err
		}
		now := s.now().UTC()
		item, err = s.repository.CreateItem(ctx, ImportItem{
			ID:            newOpaqueID(),
			SessionID:     session.ID,
			ObjectKey:     plan.ObjectKey,
			OriginalName:  intent.FileName,
			ContentType:   intent.ContentType,
			ContentLength: intent.ContentLength,
			ContentSHA256: intent.ContentSHA256,
			Multipart:     intent.Multipart,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
		if err != nil {
			return UploadTicket{}, err
		}
	}

	session.Status = SessionAwaitingUpload
	session.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveSession(ctx, session); err != nil {
		return UploadTicket{}, err
	}

	return UploadTicket{
		Session: session,
		Item:    item,
		Plan:    plan,
	}, nil
}

func (s *Service) ConfirmUpload(ctx context.Context, input ConfirmUploadInput) (ConfirmResult, error) {
	session, err := s.loadSession(ctx, input.SessionID, input.LibraryID)
	if err != nil {
		return ConfirmResult{}, err
	}
	item, err := s.repository.GetItemByObjectKey(ctx, session.ID, input.ObjectKey)
	if err != nil {
		return ConfirmResult{}, err
	}
	if item.ConfirmedAt != nil && strings.TrimSpace(item.AssetID) != "" {
		record, assetErr := s.repository.GetAsset(ctx, item.AssetID)
		if assetErr == nil {
			return ConfirmResult{
				Asset:         record,
				ImportSession: session,
			}, nil
		}
	}

	if err := validateConfirmationPayload(item, input); err != nil {
		return ConfirmResult{}, err
	}

	if item.Multipart {
		if _, err := s.planner.CompleteMultipartUpload(ctx, item.ObjectKey, input.UploadID, input.Parts); err != nil {
			return ConfirmResult{}, s.markItemFailure(ctx, session, item, fmt.Errorf("%w: %v", ErrUploadValidationFailed, err))
		}
	}

	info, err := s.planner.ValidateUploadedObject(ctx, UploadValidationInput{
		SessionID:     session.ID,
		LibraryID:     session.LibraryID,
		ObjectKey:     item.ObjectKey,
		ContentLength: input.ContentLength,
		ETag:          input.ETag,
		ContentSHA256: normalizeContentSHA(firstNonEmpty(input.ContentSHA256, item.ContentSHA256)),
	})
	if err != nil {
		return ConfirmResult{}, s.markItemFailure(ctx, session, item, fmt.Errorf("%w: %v", ErrUploadValidationFailed, err))
	}

	body, err := s.provider.GetObject(ctx, info.Ref)
	if err != nil {
		return ConfirmResult{}, s.markItemFailure(ctx, session, item, err)
	}
	defer body.Close()

	payload, err := readAll(body)
	if err != nil {
		return ConfirmResult{}, s.markItemFailure(ctx, session, item, err)
	}

	mediaType := DetectMediaType(firstNonEmpty(info.ContentType, item.ContentType), payload)
	contentSHA := normalizeContentSHA(firstNonEmpty(input.ContentSHA256, item.ContentSHA256))
	if contentSHA == "" {
		contentSHA = SHA256Hex(payload)
	}
	if existing, found, err := s.repository.FindAssetByContentSHA(ctx, session.LibraryID, contentSHA); err != nil {
		return ConfirmResult{}, err
	} else if found {
		now := s.now().UTC()
		item.AssetID = existing.ID
		item.ETag = info.ETag
		item.ContentSHA256 = contentSHA
		item.ConfirmedAt = &now
		item.FailureReason = ""
		item.UpdatedAt = now
		if err := s.repository.SaveItem(ctx, item); err != nil {
			return ConfirmResult{}, err
		}
		_ = s.provider.DeleteObject(ctx, info.Ref)

		session, err = s.refreshSessionStatus(ctx, session)
		if err != nil {
			return ConfirmResult{}, err
		}

		return ConfirmResult{
			Asset:          existing,
			ImportSession:  session,
			ExactDuplicate: true,
		}, nil
	}

	record, candidateID, err := s.createAssetRecord(ctx, session, item, info, mediaType, contentSHA, payload)
	if err != nil {
		return ConfirmResult{}, s.markItemFailure(ctx, session, item, err)
	}
	record.DuplicateCandidateOf = candidateID
	if err := s.repository.SaveAsset(ctx, record); err != nil {
		return ConfirmResult{}, err
	}

	now := s.now().UTC()
	item.AssetID = record.ID
	item.ETag = info.ETag
	item.ContentSHA256 = contentSHA
	item.ConfirmedAt = &now
	item.FailureReason = ""
	item.UpdatedAt = now
	if err := s.repository.SaveItem(ctx, item); err != nil {
		return ConfirmResult{}, err
	}

	session, err = s.refreshSessionStatus(ctx, session)
	if err != nil {
		return ConfirmResult{}, err
	}

	return ConfirmResult{
		Asset:         record,
		ImportSession: session,
	}, nil
}

func validateConfirmationPayload(item ImportItem, input ConfirmUploadInput) error {
	if item.Multipart {
		if strings.TrimSpace(input.UploadID) == "" || len(input.Parts) == 0 {
			return ErrMultipartConfirmationMissing
		}

		seenParts := make(map[int]struct{}, len(input.Parts))
		lastPart := 0
		for _, part := range input.Parts {
			if part.PartNumber <= 0 || strings.TrimSpace(part.ETag) == "" {
				return ErrMultipartConfirmationInvalid
			}
			if _, exists := seenParts[part.PartNumber]; exists {
				return ErrMultipartConfirmationInvalid
			}
			if part.PartNumber < lastPart {
				return ErrMultipartConfirmationInvalid
			}
			seenParts[part.PartNumber] = struct{}{}
			lastPart = part.PartNumber
		}
		return nil
	}

	if strings.TrimSpace(input.UploadID) != "" || len(input.Parts) > 0 {
		return ErrUnexpectedMultipartPayload
	}
	return nil
}

func (s *Service) getActiveSession(ctx context.Context, sessionID string, libraryID string) (ImportSession, error) {
	session, err := s.loadSession(ctx, sessionID, libraryID)
	if err != nil {
		return ImportSession{}, err
	}
	if session.Status == SessionConfirmed {
		return ImportSession{}, ErrSessionClosed
	}

	return session, nil
}

func (s *Service) loadSession(ctx context.Context, sessionID string, libraryID string) (ImportSession, error) {
	session, err := s.repository.GetSession(ctx, sessionID)
	if err != nil {
		return ImportSession{}, err
	}
	if strings.TrimSpace(libraryID) != "" && !strings.EqualFold(strings.TrimSpace(session.LibraryID), strings.TrimSpace(libraryID)) {
		return ImportSession{}, ErrSessionLibraryMismatch
	}
	if s.now().UTC().After(session.ExpiresAt) {
		return ImportSession{}, ErrSessionExpired
	}

	return session, nil
}

func (s *Service) createAssetRecord(
	ctx context.Context,
	session ImportSession,
	item ImportItem,
	info storage.ObjectInfo,
	mediaType string,
	contentSHA string,
	payload []byte,
) (asset.Asset, string, error) {
	now := s.now().UTC()
	candidateID := ""
	var err error
	imageMetadata := ImageMetadata{}
	var derivatives []DerivativeImage
	if strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		analyzed, generated, analyzeErr := AnalyzeImage(payload)
		if analyzeErr == nil {
			imageMetadata = analyzed
			derivatives = generated
			if analyzed.PerceptualHash != "" {
				candidateID, err = s.findSimilarAsset(ctx, session.LibraryID, analyzed.PerceptualHash, "")
				if err != nil {
					return asset.Asset{}, "", err
				}
			}
		}
	}

	record, err := s.repository.CreateAsset(ctx, asset.Asset{
		ID:               newOpaqueID(),
		LibraryID:        session.LibraryID,
		MediaType:        mediaType,
		OriginalFilename: item.OriginalName,
		ContentSHA256:    contentSHA,
		PerceptualHash:   imageMetadata.PerceptualHash,
		Width:            imageMetadata.Width,
		Height:           imageMetadata.Height,
		ImportedAt:       now,
		ProcessingStage:  asset.StageStored,
		BackupStatus:     "pending",
	})
	if err != nil {
		return asset.Asset{}, "", err
	}

	if _, err := s.repository.CreateObjectReference(ctx, asset.ObjectReference{
		ID:            newOpaqueID(),
		AssetID:       record.ID,
		ProviderName:  s.providerName,
		Bucket:        info.Ref.Bucket,
		ObjectKey:     info.Ref.Key,
		ObjectVersion: info.VersionID,
		ETag:          info.ETag,
		Purpose:       asset.ObjectPurposeOriginal,
		ContentLength: info.ContentLength,
		ContentSHA256: contentSHA,
		Metadata: map[string]string{
			"session_id": session.ID,
			"source":     string(session.Source),
		},
		Immutable: true,
		CreatedAt: now,
	}); err != nil {
		return asset.Asset{}, "", err
	}

	if len(derivatives) > 0 {
		if err := s.storeDerivatives(ctx, record.ID, derivatives); err != nil {
			record.ProcessingStage = asset.StagePartialFailure
		} else {
			record.ProcessingStage = asset.StageDerivativesReady
		}
	}

	return record, candidateID, nil
}

func (s *Service) storeDerivatives(ctx context.Context, assetID string, derivatives []DerivativeImage) error {
	for _, derivative := range derivatives {
		objectKey := path.Join(
			"derivatives",
			assetID,
			fmt.Sprintf("%s-%s.jpg", derivative.Purpose, strings.ReplaceAll(newOpaqueID(), "-", "")),
		)
		info, err := s.provider.PutObject(ctx, storage.PutObjectInput{
			Ref: storage.ObjectRef{
				Bucket: s.bucket,
				Key:    objectKey,
			},
			Body:          bytes.NewReader(derivative.Payload),
			ContentType:   derivative.ContentType,
			ContentLength: int64(len(derivative.Payload)),
			Metadata: map[string]string{
				"photonest-asset-id": string(assetID),
				"photonest-purpose":  string(derivative.Purpose),
			},
		})
		if err != nil {
			return err
		}
		if _, err := s.repository.CreateObjectReference(ctx, asset.ObjectReference{
			ID:            newOpaqueID(),
			AssetID:       assetID,
			ProviderName:  s.providerName,
			Bucket:        info.Ref.Bucket,
			ObjectKey:     info.Ref.Key,
			ObjectVersion: info.VersionID,
			ETag:          info.ETag,
			Purpose:       derivative.Purpose,
			ContentLength: info.ContentLength,
			Metadata: map[string]string{
				"width":  fmt.Sprintf("%d", derivative.Width),
				"height": fmt.Sprintf("%d", derivative.Height),
			},
			Immutable: true,
			CreatedAt: s.now().UTC(),
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) findSimilarAsset(ctx context.Context, libraryID string, perceptualHash string, excludeAssetID string) (string, error) {
	if strings.TrimSpace(perceptualHash) == "" {
		return "", nil
	}
	records, err := s.repository.ListAssetsByLibrary(ctx, libraryID)
	if err != nil {
		return "", err
	}
	bestID := ""
	bestDistance := s.similarityThreshold + 1
	for _, record := range records {
		if strings.TrimSpace(record.ID) == strings.TrimSpace(excludeAssetID) {
			continue
		}
		if strings.TrimSpace(record.PerceptualHash) == "" {
			continue
		}
		distance, err := HammingDistanceHex(perceptualHash, record.PerceptualHash)
		if err != nil {
			continue
		}
		if distance <= s.similarityThreshold && distance < bestDistance {
			bestID = record.ID
			bestDistance = distance
		}
	}

	return bestID, nil
}

func (s *Service) markItemFailure(ctx context.Context, session ImportSession, item ImportItem, err error) error {
	now := s.now().UTC()
	item.FailureReason = err.Error()
	item.UpdatedAt = now
	_ = s.repository.SaveItem(ctx, item)

	session.Status = SessionFailed
	session.UpdatedAt = now
	_ = s.repository.SaveSession(ctx, session)

	return err
}

func (s *Service) refreshSessionStatus(ctx context.Context, session ImportSession) (ImportSession, error) {
	items, err := s.repository.ListItemsBySession(ctx, session.ID)
	if err != nil {
		return ImportSession{}, err
	}

	confirmed := 0
	failed := 0
	for _, item := range items {
		if item.ConfirmedAt != nil {
			confirmed++
		}
		if strings.TrimSpace(item.FailureReason) != "" {
			failed++
		}
	}

	switch {
	case confirmed > 0 && confirmed >= session.ExpectedItemCount:
		session.Status = SessionConfirmed
	case confirmed > 0:
		session.Status = SessionUploaded
	case failed > 0:
		session.Status = SessionFailed
	default:
		session.Status = SessionAwaitingUpload
	}
	session.UpdatedAt = s.now().UTC()
	if err := s.repository.SaveSession(ctx, session); err != nil {
		return ImportSession{}, err
	}
	return session, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeContentSHA(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func readAll(body interface{ Read([]byte) (int, error) }) ([]byte, error) {
	return io.ReadAll(body)
}
