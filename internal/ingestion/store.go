package ingestion

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/library"
)

var (
	ErrSessionNotFound = errors.New("import session not found")
	ErrItemNotFound    = errors.New("import item not found")
	ErrAssetNotFound   = errors.New("asset not found")
)

type Repository interface {
	CreateSession(ctx context.Context, session ImportSession) (ImportSession, error)
	GetSession(ctx context.Context, sessionID string) (ImportSession, error)
	SaveSession(ctx context.Context, session ImportSession) error
	CreateItem(ctx context.Context, item ImportItem) (ImportItem, error)
	SaveItem(ctx context.Context, item ImportItem) error
	FindReusableItem(ctx context.Context, sessionID string, lookup ReusableItemLookup) (ImportItem, bool, error)
	GetItemByObjectKey(ctx context.Context, sessionID string, objectKey string) (ImportItem, error)
	ListItemsBySession(ctx context.Context, sessionID string) ([]ImportItem, error)
	CreateAsset(ctx context.Context, record asset.Asset) (asset.Asset, error)
	SaveAsset(ctx context.Context, record asset.Asset) error
	GetAsset(ctx context.Context, assetID string) (asset.Asset, error)
	FindAssetByContentSHA(ctx context.Context, libraryID string, contentSHA string) (asset.Asset, bool, error)
	ListAssetsByLibrary(ctx context.Context, libraryID string) ([]asset.Asset, error)
	CreateObjectReference(ctx context.Context, ref asset.ObjectReference) (asset.ObjectReference, error)
	ListObjectReferencesByAsset(ctx context.Context, assetID string) ([]asset.ObjectReference, error)
	SaveRecognitionRun(ctx context.Context, run asset.RecognitionRun) (asset.RecognitionRun, error)
	GetRecognitionRun(ctx context.Context, assetID string, stage asset.RecognitionStage) (asset.RecognitionRun, bool, error)
	GetRecognitionRunByID(ctx context.Context, runID string) (asset.RecognitionRun, bool, error)
	ListRecognitionRunsByAsset(ctx context.Context, assetID string) ([]asset.RecognitionRun, error)
	SaveLibraryPolicy(ctx context.Context, libraryID string, policy library.Policy) error
	GetLibraryPolicy(ctx context.Context, libraryID string) (library.Policy, error)
}

type ReusableItemLookup struct {
	OriginalName  string
	ContentType   string
	ContentLength int64
	ContentSHA256 string
	Multipart     bool
}

type MemoryStore struct {
	mu               sync.RWMutex
	sessions         map[string]ImportSession
	items            map[string]ImportItem
	sessionItems     map[string][]string
	assets           map[string]asset.Asset
	libraryAssets    map[string][]string
	contentSHAIndex  map[string]string
	objectReferences map[string]asset.ObjectReference
	assetObjectRefs  map[string][]string
	recognitionRuns  map[string]asset.RecognitionRun
	assetRuns        map[string][]string
	libraryPolicies  map[string]library.Policy
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:         map[string]ImportSession{},
		items:            map[string]ImportItem{},
		sessionItems:     map[string][]string{},
		assets:           map[string]asset.Asset{},
		libraryAssets:    map[string][]string{},
		contentSHAIndex:  map[string]string{},
		objectReferences: map[string]asset.ObjectReference{},
		assetObjectRefs:  map[string][]string{},
		recognitionRuns:  map[string]asset.RecognitionRun{},
		assetRuns:        map[string][]string{},
		libraryPolicies:  map[string]library.Policy{},
	}
}

func (s *MemoryStore) CreateSession(_ context.Context, session ImportSession) (ImportSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := cloneSession(session)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = newOpaqueID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	s.sessions[record.ID] = record

	return cloneSession(record), nil
}

func (s *MemoryStore) GetSession(_ context.Context, sessionID string) (ImportSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return ImportSession{}, ErrSessionNotFound
	}

	return cloneSession(record), nil
}

func (s *MemoryStore) SaveSession(_ context.Context, session ImportSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[session.ID]; !ok {
		return ErrSessionNotFound
	}
	record := cloneSession(session)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	s.sessions[record.ID] = record
	return nil
}

func (s *MemoryStore) CreateItem(_ context.Context, item ImportItem) (ImportItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := cloneItem(item)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = newOpaqueID()
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	s.items[record.ID] = record
	s.sessionItems[record.SessionID] = append(s.sessionItems[record.SessionID], record.ID)

	return cloneItem(record), nil
}

func (s *MemoryStore) SaveItem(_ context.Context, item ImportItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[item.ID]; !ok {
		return ErrItemNotFound
	}
	record := cloneItem(item)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	s.items[record.ID] = record
	return nil
}

func (s *MemoryStore) FindReusableItem(_ context.Context, sessionID string, lookup ReusableItemLookup) (ImportItem, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	itemIDs := s.sessionItems[strings.TrimSpace(sessionID)]
	for index := len(itemIDs) - 1; index >= 0; index-- {
		record := s.items[itemIDs[index]]
		if record.ConfirmedAt != nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record.OriginalName), strings.TrimSpace(lookup.OriginalName)) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record.ContentType), strings.TrimSpace(lookup.ContentType)) {
			continue
		}
		if record.ContentLength != lookup.ContentLength || record.Multipart != lookup.Multipart {
			continue
		}
		if sha := strings.TrimSpace(lookup.ContentSHA256); sha != "" && record.ContentSHA256 != sha {
			continue
		}

		return cloneItem(record), true, nil
	}

	return ImportItem{}, false, nil
}

func (s *MemoryStore) GetItemByObjectKey(_ context.Context, sessionID string, objectKey string) (ImportItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, itemID := range s.sessionItems[strings.TrimSpace(sessionID)] {
		record := s.items[itemID]
		if strings.TrimSpace(record.ObjectKey) == strings.TrimSpace(objectKey) {
			return cloneItem(record), nil
		}
	}

	return ImportItem{}, ErrItemNotFound
}

func (s *MemoryStore) ListItemsBySession(_ context.Context, sessionID string) ([]ImportItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]ImportItem, 0, len(s.sessionItems[strings.TrimSpace(sessionID)]))
	for _, itemID := range s.sessionItems[strings.TrimSpace(sessionID)] {
		records = append(records, cloneItem(s.items[itemID]))
	}
	return records, nil
}

func (s *MemoryStore) CreateAsset(_ context.Context, record asset.Asset) (asset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := cloneAsset(record)
	if strings.TrimSpace(cloned.ID) == "" {
		cloned.ID = newOpaqueID()
	}
	if cloned.ImportedAt.IsZero() {
		cloned.ImportedAt = time.Now().UTC()
	}
	s.assets[cloned.ID] = cloned
	s.libraryAssets[cloned.LibraryID] = append(s.libraryAssets[cloned.LibraryID], cloned.ID)
	if sha := strings.TrimSpace(cloned.ContentSHA256); sha != "" {
		s.contentSHAIndex[contentSHAKey(cloned.LibraryID, sha)] = cloned.ID
	}

	return cloneAsset(cloned), nil
}

func (s *MemoryStore) SaveAsset(_ context.Context, record asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.assets[record.ID]; !ok {
		return ErrAssetNotFound
	}
	cloned := cloneAsset(record)
	s.assets[cloned.ID] = cloned
	if sha := strings.TrimSpace(cloned.ContentSHA256); sha != "" {
		s.contentSHAIndex[contentSHAKey(cloned.LibraryID, sha)] = cloned.ID
	}
	return nil
}

func (s *MemoryStore) GetAsset(_ context.Context, assetID string) (asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.assets[strings.TrimSpace(assetID)]
	if !ok {
		return asset.Asset{}, ErrAssetNotFound
	}
	return cloneAsset(record), nil
}

func (s *MemoryStore) FindAssetByContentSHA(_ context.Context, libraryID string, contentSHA string) (asset.Asset, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	assetID, ok := s.contentSHAIndex[contentSHAKey(libraryID, contentSHA)]
	if !ok {
		return asset.Asset{}, false, nil
	}
	record, ok := s.assets[assetID]
	if !ok {
		return asset.Asset{}, false, nil
	}
	return cloneAsset(record), true, nil
}

func (s *MemoryStore) ListAssetsByLibrary(_ context.Context, libraryID string) ([]asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]asset.Asset, 0, len(s.libraryAssets[strings.TrimSpace(libraryID)]))
	for _, assetID := range s.libraryAssets[strings.TrimSpace(libraryID)] {
		records = append(records, cloneAsset(s.assets[assetID]))
	}
	return records, nil
}

func (s *MemoryStore) CreateObjectReference(_ context.Context, ref asset.ObjectReference) (asset.ObjectReference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := cloneObjectReference(ref)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = newOpaqueID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.objectReferences[record.ID] = record
	s.assetObjectRefs[record.AssetID] = append(s.assetObjectRefs[record.AssetID], record.ID)
	return cloneObjectReference(record), nil
}

func (s *MemoryStore) ListObjectReferencesByAsset(_ context.Context, assetID string) ([]asset.ObjectReference, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]asset.ObjectReference, 0, len(s.assetObjectRefs[strings.TrimSpace(assetID)]))
	for _, refID := range s.assetObjectRefs[strings.TrimSpace(assetID)] {
		records = append(records, cloneObjectReference(s.objectReferences[refID]))
	}
	return records, nil
}

func (s *MemoryStore) SaveRecognitionRun(_ context.Context, run asset.RecognitionRun) (asset.RecognitionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := cloneRecognitionRun(run)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = newOpaqueID()
	}
	key := recognitionRunKey(record.AssetID, record.Stage)
	if existing, ok := s.recognitionRuns[key]; ok {
		record.ID = existing.ID
	}
	s.recognitionRuns[key] = record
	if !slices.Contains(s.assetRuns[record.AssetID], key) {
		s.assetRuns[record.AssetID] = append(s.assetRuns[record.AssetID], key)
	}

	return cloneRecognitionRun(record), nil
}

func (s *MemoryStore) GetRecognitionRun(_ context.Context, assetID string, stage asset.RecognitionStage) (asset.RecognitionRun, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.recognitionRuns[recognitionRunKey(assetID, stage)]
	if !ok {
		return asset.RecognitionRun{}, false, nil
	}
	return cloneRecognitionRun(record), true, nil
}

func (s *MemoryStore) GetRecognitionRunByID(_ context.Context, runID string) (asset.RecognitionRun, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, record := range s.recognitionRuns {
		if strings.TrimSpace(record.ID) == strings.TrimSpace(runID) {
			return cloneRecognitionRun(record), true, nil
		}
	}
	return asset.RecognitionRun{}, false, nil
}

func (s *MemoryStore) ListRecognitionRunsByAsset(_ context.Context, assetID string) ([]asset.RecognitionRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := s.assetRuns[strings.TrimSpace(assetID)]
	records := make([]asset.RecognitionRun, 0, len(keys))
	for _, key := range keys {
		record, ok := s.recognitionRuns[key]
		if !ok {
			continue
		}
		records = append(records, cloneRecognitionRun(record))
	}
	return records, nil
}

func (s *MemoryStore) SaveLibraryPolicy(_ context.Context, libraryID string, policy library.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.libraryPolicies[strings.TrimSpace(libraryID)] = policy.WithDefaults()
	return nil
}

func (s *MemoryStore) GetLibraryPolicy(_ context.Context, libraryID string) (library.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policy, ok := s.libraryPolicies[strings.TrimSpace(libraryID)]
	if !ok {
		return library.DefaultPolicy(), nil
	}
	return policy.WithDefaults(), nil
}

func contentSHAKey(libraryID string, contentSHA string) string {
	return strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(contentSHA)
}

func recognitionRunKey(assetID string, stage asset.RecognitionStage) string {
	return strings.TrimSpace(assetID) + ":" + strings.TrimSpace(string(stage))
}

func newOpaqueID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		buf[0:4],
		buf[4:6],
		buf[6:8],
		buf[8:10],
		buf[10:16],
	)
}

func cloneSession(session ImportSession) ImportSession {
	return session
}

func cloneItem(item ImportItem) ImportItem {
	return item
}

func cloneAsset(record asset.Asset) asset.Asset {
	if record.CapturedAt != nil {
		capturedAt := *record.CapturedAt
		record.CapturedAt = &capturedAt
	}
	record.GPSLatitude = cloneFloat64Pointer(record.GPSLatitude)
	record.GPSLongitude = cloneFloat64Pointer(record.GPSLongitude)
	record.Tags = slices.Clone(record.Tags)
	record.Embedding = slices.Clone(record.Embedding)
	return record
}

func cloneObjectReference(ref asset.ObjectReference) asset.ObjectReference {
	ref.Metadata = cloneStringMap(ref.Metadata)
	return ref
}

func cloneRecognitionRun(run asset.RecognitionRun) asset.RecognitionRun {
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		run.FinishedAt = &finishedAt
	}
	if run.DebugExpiresAt != nil {
		debugExpiresAt := *run.DebugExpiresAt
		run.DebugExpiresAt = &debugExpiresAt
	}
	run.DebugPayload = maps.Clone(run.DebugPayload)
	return run
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneFloat64Pointer(input *float64) *float64 {
	if input == nil {
		return nil
	}

	value := *input
	return &value
}
