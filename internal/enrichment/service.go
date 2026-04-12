package enrichment

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/job"
	"github.com/photonest/photonest/internal/library"
	"github.com/photonest/photonest/internal/platform/telemetry"
	"github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

const defaultDownloadTTL = 5 * time.Minute

type Repository interface {
	GetAsset(ctx context.Context, assetID string) (asset.Asset, error)
	SaveAsset(ctx context.Context, record asset.Asset) error
	ListObjectReferencesByAsset(ctx context.Context, assetID string) ([]asset.ObjectReference, error)
	SaveRecognitionRun(ctx context.Context, run asset.RecognitionRun) (asset.RecognitionRun, error)
	GetRecognitionRun(ctx context.Context, assetID string, stage asset.RecognitionStage) (asset.RecognitionRun, bool, error)
	GetRecognitionRunByID(ctx context.Context, runID string) (asset.RecognitionRun, bool, error)
	ListRecognitionRunsByAsset(ctx context.Context, assetID string) ([]asset.RecognitionRun, error)
	GetLibraryPolicy(ctx context.Context, libraryID string) (library.Policy, error)
}

type ServiceConfig struct {
	Repository          Repository
	Storage             storage.Provider
	AIProviders         []ai.Provider
	Queue               Queue
	Geocoder            ReverseGeocoder
	DownloadTTL         time.Duration
	DebugRetention      time.Duration
	RetainProviderDebug bool
	Telemetry           telemetry.Recorder
	Now                 func() time.Time
}

type Service struct {
	repository          Repository
	storage             storage.Provider
	providers           map[string]ai.Provider
	queue               Queue
	geocoder            ReverseGeocoder
	downloadTTL         time.Duration
	debugRetention      time.Duration
	retainProviderDebug bool
	telemetry           telemetry.Recorder
	now                 func() time.Time

	mu           sync.Mutex
	failureCount map[string]int
}

type stageResult struct {
	status       asset.RecognitionStatus
	providerName string
	policyReason string
	debugPayload map[string]any
	err          error
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("storage provider is required")
	}
	if cfg.Queue == nil {
		cfg.Queue = NewMemoryQueue()
	}
	if cfg.Geocoder == nil {
		cfg.Geocoder = FormattingGeocoder{}
	}
	if cfg.DownloadTTL <= 0 {
		cfg.DownloadTTL = defaultDownloadTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	providers := make(map[string]ai.Provider, len(cfg.AIProviders))
	for _, provider := range cfg.AIProviders {
		if provider == nil {
			continue
		}
		providers[provider.Name()] = provider
	}

	return &Service{
		repository:          cfg.Repository,
		storage:             cfg.Storage,
		providers:           providers,
		queue:               cfg.Queue,
		geocoder:            cfg.Geocoder,
		downloadTTL:         cfg.DownloadTTL,
		debugRetention:      cfg.DebugRetention,
		retainProviderDebug: cfg.RetainProviderDebug,
		telemetry:           cfg.Telemetry,
		now:                 cfg.Now,
		failureCount:        map[string]int{},
	}, nil
}

func (s *Service) QueueAsset(ctx context.Context, assetID string) error {
	return s.QueueStage(ctx, assetID, asset.RecognitionStageMetadata, 0, nil)
}

func (s *Service) QueueStage(ctx context.Context, assetID string, stage asset.RecognitionStage, retryCount int, debug map[string]any) error {
	return s.queue.Enqueue(ctx, job.Payload{
		TaskID:     fmt.Sprintf("%s:%s", strings.TrimSpace(assetID), stage),
		AssetID:    strings.TrimSpace(assetID),
		Operation:  "asset-enrichment",
		Stage:      string(stage),
		RetryCount: retryCount,
		Debug:      debug,
	})
}

func (s *Service) RunPending(ctx context.Context) (int, error) {
	processed := 0
	var firstErr error
	for {
		payload, ok, err := s.queue.Dequeue(ctx)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, firstErr
		}
		processed++
		if err := s.handlePayload(ctx, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
}

func (s *Service) GetRecognitionRun(ctx context.Context, runID string) (asset.RecognitionRun, bool, error) {
	return s.repository.GetRecognitionRunByID(ctx, runID)
}

func (s *Service) Queue() Queue {
	return s.queue
}

func (s *Service) handlePayload(ctx context.Context, payload job.Payload) error {
	stage := asset.RecognitionStage(strings.TrimSpace(payload.Stage))
	if strings.TrimSpace(payload.AssetID) == "" {
		return fmt.Errorf("asset id is required")
	}
	if stage == "" {
		return fmt.Errorf("stage is required")
	}

	record, err := s.repository.GetAsset(ctx, payload.AssetID)
	if err != nil {
		return err
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, record.LibraryID)
	if err != nil {
		return err
	}

	existingRun, found, err := s.repository.GetRecognitionRun(ctx, record.ID, stage)
	if err != nil {
		return err
	}
	if found && (existingRun.Status == asset.RecognitionStatusSucceeded || existingRun.Status == asset.RecognitionStatusSkipped) {
		return nil
	}

	run, err := s.startRun(ctx, record.ID, stage, found, existingRun)
	if err != nil {
		return err
	}

	switch stage {
	case asset.RecognitionStageMetadata:
		result := s.runMetadataStage(ctx, &record, policy)
		return s.finishStage(ctx, record, run, stage, result)
	case asset.RecognitionStageCaption:
		result := s.runCaptionStage(ctx, &record, policy, payload.RetryCount)
		return s.finishStage(ctx, record, run, stage, result)
	case asset.RecognitionStageOCR:
		result := s.runOCRStage(ctx, &record, policy, payload.RetryCount)
		return s.finishStage(ctx, record, run, stage, result)
	case asset.RecognitionStageEmbedding:
		result := s.runEmbeddingStage(ctx, &record, policy, payload.RetryCount)
		return s.finishStage(ctx, record, run, stage, result)
	case asset.RecognitionStageIndexing:
		result := s.runIndexingStage(ctx, &record)
		return s.finishStage(ctx, record, run, stage, result)
	default:
		return fmt.Errorf("unsupported recognition stage %q", stage)
	}
}

func (s *Service) startRun(ctx context.Context, assetID string, stage asset.RecognitionStage, found bool, existing asset.RecognitionRun) (asset.RecognitionRun, error) {
	run := existing
	if !found {
		run = asset.RecognitionRun{
			AssetID: assetID,
			Stage:   stage,
		}
	}

	now := s.now().UTC()
	run.Status = asset.RecognitionStatusRunning
	run.Attempts++
	run.StartedAt = now
	run.FinishedAt = nil
	run.DebugExpiresAt = nil
	run.DebugPayload = nil
	run.LastError = ""
	run.PolicyReason = ""
	run.ProviderName = ""

	return s.repository.SaveRecognitionRun(ctx, run)
}

func (s *Service) finishStage(ctx context.Context, record asset.Asset, run asset.RecognitionRun, stage asset.RecognitionStage, result stageResult) error {
	now := s.now().UTC()
	if err := s.repository.SaveAsset(ctx, record); err != nil {
		return err
	}

	run.Status = result.status
	run.ProviderName = strings.TrimSpace(result.providerName)
	run.PolicyReason = strings.TrimSpace(result.policyReason)
	run.FinishedAt = &now
	if result.err != nil {
		run.LastError = result.err.Error()
	}
	if len(result.debugPayload) > 0 && s.debugRetention > 0 {
		expiresAt := now.Add(s.debugRetention)
		run.DebugExpiresAt = &expiresAt
		run.DebugPayload = result.debugPayload
	}
	if _, err := s.repository.SaveRecognitionRun(ctx, run); err != nil {
		return err
	}
	if strings.TrimSpace(run.ProviderName) != "" {
		s.trackProviderOutcome(run.ProviderName, result.err == nil)
	}
	s.recordStageTelemetry(record, run, result)
	if err := s.syncAssetState(ctx, record.ID); err != nil {
		return err
	}
	if err := s.enqueueFollowUps(ctx, record.ID, stage); err != nil {
		return err
	}
	return result.err
}

func (s *Service) runMetadataStage(ctx context.Context, record *asset.Asset, policy library.Policy) stageResult {
	input, err := s.loadOriginalInput(ctx, record.ID)
	if err != nil {
		return stageResult{status: asset.RecognitionStatusFailed, err: err}
	}
	extracted, err := ExtractMetadata(input.Payload, input.Info)
	if err != nil {
		return stageResult{status: asset.RecognitionStatusFailed, err: err}
	}

	if extracted.Width > 0 {
		record.Width = extracted.Width
	}
	if extracted.Height > 0 {
		record.Height = extracted.Height
	}
	if extracted.CapturedAt != nil {
		record.CapturedAt = extracted.CapturedAt
	}
	if !record.ImportedAt.IsZero() {
		record.TimelineAt = record.ImportedAt.UTC()
	}
	if record.CapturedAt != nil {
		record.TimelineAt = record.CapturedAt.UTC()
	}
	record.DeviceMake = firstNonEmpty(extracted.DeviceMake, record.DeviceMake)
	record.DeviceModel = firstNonEmpty(extracted.DeviceModel, record.DeviceModel)

	if policy.ShouldRunGPS() && extracted.GPSLatitude != nil && extracted.GPSLongitude != nil {
		record.GPSLatitude = extracted.GPSLatitude
		record.GPSLongitude = extracted.GPSLongitude
		record.LocationLabel = firstNonEmpty(extracted.LocationLabel, record.LocationLabel)
		if record.LocationLabel == "" {
			if label, err := s.geocoder.Lookup(ctx, *extracted.GPSLatitude, *extracted.GPSLongitude); err == nil {
				record.LocationLabel = label
			}
		}
	} else {
		record.GPSLatitude = nil
		record.GPSLongitude = nil
		record.LocationLabel = ""
	}

	if record.TimelineAt.IsZero() {
		record.TimelineAt = s.now().UTC()
	}

	return stageResult{
		status: asset.RecognitionStatusSucceeded,
		debugPayload: map[string]any{
			"stage":         string(asset.RecognitionStageMetadata),
			"capturedAt":    formatOptionalTime(record.CapturedAt),
			"deviceMake":    record.DeviceMake,
			"deviceModel":   record.DeviceModel,
			"locationLabel": record.LocationLabel,
		},
	}
}

func (s *Service) runCaptionStage(ctx context.Context, record *asset.Asset, policy library.Policy, retryCount int) stageResult {
	if !policy.ShouldRunCaption() {
		record.CaptionText = ""
		record.Tags = ai.MergeTags(record.OCRText, record.LocationLabel, record.OriginalFilename)
		return stageResult{
			status:       asset.RecognitionStatusSkipped,
			policyReason: "caption generation disabled by privacy policy",
		}
	}

	request, provider, routeErr := s.newAIRequest(ctx, *record, ai.CapabilityCaption, retryCount)
	if routeErr != nil {
		return s.routeErrorResult(record, ai.CapabilityCaption, routeErr)
	}
	result, err := provider.Caption(ctx, ai.CaptionRequest{
		AssetID:   record.ID,
		ObjectURL: request.URL,
		Locale:    "zh-CN",
		FileName:  record.OriginalFilename,
	})
	if err != nil {
		return stageResult{
			status:       asset.RecognitionStatusFailed,
			providerName: provider.Name(),
			err:          ai.ClassifyError(err, provider.Boundary()),
		}
	}

	record.CaptionText = strings.TrimSpace(result.Text)
	record.Tags = ai.MergeTags(record.CaptionText, record.OCRText, record.LocationLabel, record.OriginalFilename)

	return stageResult{
		status:       asset.RecognitionStatusSucceeded,
		providerName: provider.Name(),
		debugPayload: s.providerDebug(map[string]any{
			"stage":       string(asset.RecognitionStageCaption),
			"rawId":       result.RawID,
			"captionText": ai.TextPreview(result.Text, 96),
		}),
	}
}

func (s *Service) runOCRStage(ctx context.Context, record *asset.Asset, policy library.Policy, retryCount int) stageResult {
	if !policy.ShouldRunOCR() {
		record.OCRText = ""
		record.Tags = ai.MergeTags(record.CaptionText, record.LocationLabel, record.OriginalFilename)
		return stageResult{
			status:       asset.RecognitionStatusSkipped,
			policyReason: "ocr extraction disabled by privacy policy",
		}
	}

	request, provider, routeErr := s.newAIRequest(ctx, *record, ai.CapabilityOCR, retryCount)
	if routeErr != nil {
		return s.routeErrorResult(record, ai.CapabilityOCR, routeErr)
	}
	result, err := provider.OCR(ctx, ai.OCRRequest{
		AssetID:   record.ID,
		ObjectURL: request.URL,
		Locale:    "zh-CN",
		FileName:  record.OriginalFilename,
	})
	if err != nil {
		return stageResult{
			status:       asset.RecognitionStatusFailed,
			providerName: provider.Name(),
			err:          ai.ClassifyError(err, provider.Boundary()),
		}
	}

	blocks := make([]string, 0, len(result.TextBlocks))
	for _, block := range result.TextBlocks {
		if text := strings.TrimSpace(block.Text); text != "" {
			blocks = append(blocks, text)
		}
	}
	record.OCRText = strings.Join(blocks, "\n")
	record.Tags = ai.MergeTags(record.CaptionText, record.OCRText, record.LocationLabel, record.OriginalFilename)

	return stageResult{
		status:       asset.RecognitionStatusSucceeded,
		providerName: provider.Name(),
		debugPayload: s.providerDebug(map[string]any{
			"stage": string(asset.RecognitionStageOCR),
			"rawId": result.RawID,
			"text":  ai.TextPreview(record.OCRText, 96),
		}),
	}
}

func (s *Service) runEmbeddingStage(ctx context.Context, record *asset.Asset, policy library.Policy, retryCount int) stageResult {
	if !policy.ShouldRunEmbedding() {
		record.Embedding = nil
		return stageResult{
			status:       asset.RecognitionStatusSkipped,
			policyReason: "embedding generation disabled by privacy policy",
		}
	}

	request, provider, routeErr := s.newAIRequest(ctx, *record, ai.CapabilityEmbedding, retryCount)
	if routeErr != nil {
		return s.routeErrorResult(record, ai.CapabilityEmbedding, routeErr)
	}
	result, err := provider.Embedding(ctx, ai.EmbeddingRequest{
		AssetID:   record.ID,
		ObjectURL: request.URL,
		Model:     "discovery-hash-v1",
		FileName:  strings.TrimSpace(record.OriginalFilename + " " + record.CaptionText + " " + record.OCRText),
	})
	if err != nil {
		return stageResult{
			status:       asset.RecognitionStatusFailed,
			providerName: provider.Name(),
			err:          ai.ClassifyError(err, provider.Boundary()),
		}
	}

	record.Embedding = slices.Clone(result.Vector)
	return stageResult{
		status:       asset.RecognitionStatusSucceeded,
		providerName: provider.Name(),
		debugPayload: s.providerDebug(map[string]any{
			"stage":      string(asset.RecognitionStageEmbedding),
			"rawId":      result.RawID,
			"dimensions": len(result.Vector),
		}),
	}
}

func (s *Service) runIndexingStage(_ context.Context, record *asset.Asset) stageResult {
	record.Tags = ai.MergeTags(record.CaptionText, record.OCRText, record.LocationLabel, record.OriginalFilename)
	if record.CapturedAt != nil {
		record.TimelineAt = record.CapturedAt.UTC()
	} else if record.TimelineAt.IsZero() {
		record.TimelineAt = record.ImportedAt.UTC()
	}
	record.SearchDocument = buildSearchDocument(*record)
	if len(record.Embedding) > 0 {
		record.SearchEmbedding = slices.Clone(record.Embedding)
	} else {
		record.SearchEmbedding = ai.HashEmbeddingText(record.SearchDocument, 24)
	}
	indexedAt := s.now().UTC()
	record.IndexedAt = &indexedAt

	return stageResult{
		status: asset.RecognitionStatusSucceeded,
		debugPayload: map[string]any{
			"stage":          string(asset.RecognitionStageIndexing),
			"tags":           append([]string(nil), record.Tags...),
			"searchDocument": ai.TextPreview(record.SearchDocument, 120),
			"indexedAt":      indexedAt.Format(time.RFC3339),
		},
	}
}

func (s *Service) enqueueFollowUps(ctx context.Context, assetID string, stage asset.RecognitionStage) error {
	switch stage {
	case asset.RecognitionStageMetadata:
		if err := s.QueueStage(ctx, assetID, asset.RecognitionStageCaption, 0, nil); err != nil {
			return err
		}
		if err := s.QueueStage(ctx, assetID, asset.RecognitionStageOCR, 0, nil); err != nil {
			return err
		}
		return s.QueueStage(ctx, assetID, asset.RecognitionStageEmbedding, 0, nil)
	case asset.RecognitionStageCaption, asset.RecognitionStageOCR, asset.RecognitionStageEmbedding:
		ready, err := s.aiStagesTerminal(ctx, assetID)
		if err != nil || !ready {
			return err
		}
		return s.QueueStage(ctx, assetID, asset.RecognitionStageIndexing, 0, nil)
	default:
		return nil
	}
}

func (s *Service) aiStagesTerminal(ctx context.Context, assetID string) (bool, error) {
	for _, stage := range []asset.RecognitionStage{
		asset.RecognitionStageCaption,
		asset.RecognitionStageOCR,
		asset.RecognitionStageEmbedding,
	} {
		run, found, err := s.repository.GetRecognitionRun(ctx, assetID, stage)
		if err != nil {
			return false, err
		}
		if !found || !isTerminal(run.Status) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) syncAssetState(ctx context.Context, assetID string) error {
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	runs, err := s.repository.ListRecognitionRunsByAsset(ctx, assetID)
	if err != nil {
		return err
	}

	failed := make([]string, 0, len(runs))
	pending := make([]string, 0, len(runs))
	statusByStage := map[asset.RecognitionStage]asset.RecognitionStatus{}
	for _, run := range runs {
		statusByStage[run.Stage] = run.Status
		switch run.Status {
		case asset.RecognitionStatusFailed:
			failed = append(failed, string(run.Stage))
		case asset.RecognitionStatusPending, asset.RecognitionStatusRunning:
			pending = append(pending, string(run.Stage))
		}
	}

	switch {
	case len(failed) > 0:
		record.ProcessingStage = asset.StagePartialFailure
	case statusByStage[asset.RecognitionStageIndexing] == asset.RecognitionStatusSucceeded:
		record.ProcessingStage = asset.StageIndexed
	case allSucceededOrSkipped(statusByStage, asset.RecognitionStageCaption, asset.RecognitionStageOCR, asset.RecognitionStageEmbedding):
		record.ProcessingStage = asset.StageAIReady
	case statusByStage[asset.RecognitionStageMetadata] == asset.RecognitionStatusSucceeded:
		record.ProcessingStage = asset.StageMetadataReady
	}

	if len(failed) > 0 {
		record.RecognitionStatusNote = "failed: " + strings.Join(failed, ", ")
	} else if len(pending) > 0 {
		record.RecognitionStatusNote = "pending: " + strings.Join(pending, ", ")
	} else {
		record.RecognitionStatusNote = ""
	}

	return s.repository.SaveAsset(ctx, record)
}

func (s *Service) routeErrorResult(record *asset.Asset, capability ai.Capability, err error) stageResult {
	classified := ai.ClassifyError(err, ai.BoundaryRemoteService)

	switch capability {
	case ai.CapabilityCaption:
		record.CaptionText = ""
	case ai.CapabilityOCR:
		record.OCRText = ""
	case ai.CapabilityEmbedding:
		record.Embedding = nil
	}

	if classified.Kind == ai.ErrorKindPolicyBlocked {
		return stageResult{
			status:       asset.RecognitionStatusSkipped,
			policyReason: classified.Message,
		}
	}
	return stageResult{
		status: asset.RecognitionStatusFailed,
		err:    classified,
	}
}

func (s *Service) newAIRequest(ctx context.Context, record asset.Asset, capability ai.Capability, retryCount int) (storage.PresignedRequest, ai.Provider, error) {
	candidates := make([]ai.Candidate, 0, len(s.providers))
	for name, provider := range s.providers {
		status, err := provider.Health(ctx)
		healthy := err == nil && status.Healthy
		s.recordTelemetry(telemetry.Snapshot{
			Metric: "provider.health",
			Labels: map[string]string{
				"provider":   name,
				"capability": string(capability),
			},
			Data: map[string]any{
				"healthy": healthy,
				"message": status.Message,
			},
		})
		candidates = append(candidates, ai.Candidate{
			Name:         name,
			Boundary:     provider.Boundary(),
			Capabilities: provider.Capabilities(),
			Healthy:      healthy,
			FailureCount: s.providerFailureCount(name),
		})
	}

	policy, err := s.repository.GetLibraryPolicy(ctx, record.LibraryID)
	if err != nil {
		return storage.PresignedRequest{}, nil, err
	}
	decision, err := ai.SelectProvider(ai.RouteRequest{
		Capability: capability,
		Policy:     policy,
		RetryCount: retryCount,
		Candidates: candidates,
	})
	if err != nil {
		return storage.PresignedRequest{}, nil, err
	}

	provider := s.providers[decision.ProviderName]
	if provider == nil {
		return storage.PresignedRequest{}, nil, fmt.Errorf("selected provider %q is not configured", decision.ProviderName)
	}
	presigned, err := s.presignOriginal(ctx, record.ID)
	if err != nil {
		return storage.PresignedRequest{}, nil, err
	}
	return presigned, provider, nil
}

func (s *Service) presignOriginal(ctx context.Context, assetID string) (storage.PresignedRequest, error) {
	ref, err := s.findOriginalReference(ctx, assetID)
	if err != nil {
		return storage.PresignedRequest{}, err
	}
	return s.storage.PresignDownload(ctx, storage.PresignDownloadInput{
		Ref:       storage.ObjectRef{Bucket: ref.Bucket, Key: ref.ObjectKey},
		ExpiresIn: s.downloadTTL,
	})
}

func (s *Service) loadOriginalInput(ctx context.Context, assetID string) (originalInput, error) {
	ref, err := s.findOriginalReference(ctx, assetID)
	if err != nil {
		return originalInput{}, err
	}

	info, err := s.storage.HeadObject(ctx, storage.ObjectRef{
		Bucket: ref.Bucket,
		Key:    ref.ObjectKey,
	})
	if err != nil {
		return originalInput{}, err
	}
	body, err := s.storage.GetObject(ctx, storage.ObjectRef{
		Bucket: ref.Bucket,
		Key:    ref.ObjectKey,
	})
	if err != nil {
		return originalInput{}, err
	}
	defer body.Close()

	payload, err := io.ReadAll(body)
	if err != nil {
		return originalInput{}, err
	}
	return originalInput{Ref: ref, Info: info, Payload: payload}, nil
}

func (s *Service) findOriginalReference(ctx context.Context, assetID string) (asset.ObjectReference, error) {
	refs, err := s.repository.ListObjectReferencesByAsset(ctx, assetID)
	if err != nil {
		return asset.ObjectReference{}, err
	}
	for _, ref := range refs {
		if ref.Purpose == asset.ObjectPurposeOriginal {
			return ref, nil
		}
	}
	return asset.ObjectReference{}, fmt.Errorf("original object reference not found for asset %s", assetID)
}

func (s *Service) trackProviderOutcome(name string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if success {
		s.failureCount[name] = 0
		return
	}
	s.failureCount[name]++
}

func (s *Service) providerFailureCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.failureCount[name]
}

func (s *Service) providerDebug(payload map[string]any) map[string]any {
	if !s.retainProviderDebug {
		delete(payload, "captionText")
		delete(payload, "text")
	}
	return payload
}

func isTerminal(status asset.RecognitionStatus) bool {
	switch status {
	case asset.RecognitionStatusSucceeded, asset.RecognitionStatusFailed, asset.RecognitionStatusSkipped:
		return true
	default:
		return false
	}
}

func allSucceededOrSkipped(statusByStage map[asset.RecognitionStage]asset.RecognitionStatus, stages ...asset.RecognitionStage) bool {
	for _, stage := range stages {
		status := statusByStage[stage]
		if status != asset.RecognitionStatusSucceeded && status != asset.RecognitionStatusSkipped {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func buildSearchDocument(record asset.Asset) string {
	parts := []string{
		record.OriginalFilename,
		record.CaptionText,
		record.OCRText,
		record.LocationLabel,
		strings.Join(record.Tags, " "),
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return strings.Join(filtered, "\n")
}

func (s *Service) recordStageTelemetry(record asset.Asset, run asset.RecognitionRun, result stageResult) {
	labels := map[string]string{
		"asset_id": string(record.ID),
		"stage":    string(run.Stage),
	}
	if run.ProviderName != "" {
		labels["provider"] = run.ProviderName
	}
	s.recordTelemetry(telemetry.Snapshot{
		Metric: "enrichment.stage",
		Labels: labels,
		Data: map[string]any{
			"status":           string(result.status),
			"processing_stage": string(record.ProcessingStage),
			"has_error":        result.err != nil,
		},
	})
	if result.err != nil {
		s.recordTelemetry(telemetry.Snapshot{
			Metric: "job.failure",
			Labels: labels,
			Data: map[string]any{
				"error": result.err.Error(),
			},
		})
	}
	if run.Stage == asset.RecognitionStageIndexing {
		s.recordTelemetry(telemetry.Snapshot{
			Metric: "index.progress",
			Labels: map[string]string{
				"asset_id": record.ID,
			},
			Data: map[string]any{
				"indexed":      record.IndexedAt != nil,
				"search_terms": len(record.Tags),
			},
		})
	}
}

func (s *Service) recordTelemetry(snapshot telemetry.Snapshot) {
	if s.telemetry == nil {
		return
	}
	s.telemetry.Record(snapshot)
}

type originalInput struct {
	Ref     asset.ObjectReference
	Info    storage.ObjectInfo
	Payload []byte
}
