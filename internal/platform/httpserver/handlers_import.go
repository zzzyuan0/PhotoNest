package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/auth"
	"github.com/photonest/photonest/internal/provider/storage"
)

type createImportSessionRequest struct {
	LibraryID         string `json:"libraryId"`
	Source            string `json:"source"`
	ExpectedItemCount int    `json:"expectedItemCount"`
	Note              string `json:"note"`
}

type importSessionResponse struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	ExpiresAt string `json:"expiresAt"`
}

type createUploadTicketRequest struct {
	LibraryID     string `json:"libraryId"`
	FileName      string `json:"fileName"`
	ContentType   string `json:"contentType"`
	ContentLength int64  `json:"contentLength"`
	ContentSHA256 string `json:"contentSha256"`
	Multipart     bool   `json:"multipart"`
}

type uploadTicketResponse struct {
	SessionID         string                   `json:"sessionId"`
	ObjectKey         string                   `json:"objectKey"`
	Method            string                   `json:"method,omitempty"`
	URL               string                   `json:"url,omitempty"`
	Headers           map[string]string        `json:"headers,omitempty"`
	FormFields        map[string]string        `json:"formFields,omitempty"`
	ExpiresAt         string                   `json:"expiresAt"`
	ChecksumAlgorithm string                   `json:"checksumAlgorithm,omitempty"`
	Multipart         *multipartUploadResponse `json:"multipart,omitempty"`
}

type multipartUploadResponse struct {
	UploadID  string                  `json:"uploadId"`
	ExpiresAt string                  `json:"expiresAt"`
	Parts     []multipartPartResponse `json:"parts"`
}

type multipartPartResponse struct {
	PartNumber int               `json:"partNumber"`
	UploadURL  string            `json:"uploadUrl"`
	Headers    map[string]string `json:"headers,omitempty"`
}

type confirmUploadRequest struct {
	LibraryID     string                 `json:"libraryId"`
	ObjectKey     string                 `json:"objectKey"`
	ContentLength int64                  `json:"contentLength"`
	ETag          string                 `json:"etag"`
	ContentSHA256 string                 `json:"contentSha256"`
	UploadID      string                 `json:"uploadId"`
	Parts         []confirmUploadPartDTO `json:"parts"`
}

type confirmUploadPartDTO struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

type assetAcceptedResponse struct {
	AssetID         string `json:"assetId"`
	ImportSessionID string `json:"importSessionId"`
	ProcessingStage string `json:"processingStage"`
}

func (s *Server) handleCreateImportSession(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := s.requireImportContext(w, r)
	if !ok {
		return
	}

	var request createImportSessionRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	if strings.TrimSpace(request.LibraryID) != "" && !strings.EqualFold(strings.TrimSpace(request.LibraryID), strings.TrimSpace(principal.LibraryID)) {
		s.writeError(w, http.StatusBadRequest, "library_mismatch", "request libraryId does not match the authorized library scope", nil)
		return
	}

	source, err := parseImportSource(request.Source)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	session, err := service.CreateSession(r.Context(), ingestion.CreateSessionInput{
		LibraryID:         principal.LibraryID,
		Source:            source,
		ExpectedItemCount: request.ExpectedItemCount,
		Note:              request.Note,
		CreatedBy:         principal.Session.SubjectID,
	})
	if err != nil {
		s.writeImportError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapImportSession(session))
}

func (s *Server) handleCreateUploadTicket(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := s.requireImportContext(w, r)
	if !ok {
		return
	}

	var request createUploadTicketRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	if strings.TrimSpace(request.LibraryID) != "" && !strings.EqualFold(strings.TrimSpace(request.LibraryID), strings.TrimSpace(principal.LibraryID)) {
		s.writeError(w, http.StatusBadRequest, "library_mismatch", "request libraryId does not match the authorized library scope", nil)
		return
	}

	ticket, err := service.CreateUploadTicket(r.Context(), ingestion.CreateUploadTicketInput{
		SessionID:     r.PathValue("sessionId"),
		LibraryID:     principal.LibraryID,
		FileName:      request.FileName,
		ContentType:   request.ContentType,
		ContentLength: request.ContentLength,
		ContentSHA256: request.ContentSHA256,
		Multipart:     request.Multipart,
	})
	if err != nil {
		s.writeImportError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapUploadTicket(ticket.Plan))
}

func (s *Server) handleConfirmUpload(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := s.requireImportContext(w, r)
	if !ok {
		return
	}

	var request confirmUploadRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	if strings.TrimSpace(request.LibraryID) != "" && !strings.EqualFold(strings.TrimSpace(request.LibraryID), strings.TrimSpace(principal.LibraryID)) {
		s.writeError(w, http.StatusBadRequest, "library_mismatch", "request libraryId does not match the authorized library scope", nil)
		return
	}

	parts := make([]storage.CompletedPart, 0, len(request.Parts))
	for _, part := range request.Parts {
		parts = append(parts, storage.CompletedPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		})
	}

	result, err := service.ConfirmUpload(r.Context(), ingestion.ConfirmUploadInput{
		SessionID:     r.PathValue("sessionId"),
		LibraryID:     principal.LibraryID,
		ObjectKey:     request.ObjectKey,
		ContentLength: request.ContentLength,
		ETag:          request.ETag,
		ContentSHA256: request.ContentSHA256,
		UploadID:      request.UploadID,
		Parts:         parts,
	})
	if err != nil {
		s.writeImportError(w, err)
		return
	}
	if s.enrich != nil {
		_ = s.enrich.QueueAsset(r.Context(), result.Asset.ID)
	}
	if s.backup != nil {
		if updated, backupErr := s.backup.CopyAsset(r.Context(), principal.LibraryID, result.Asset.ID); backupErr == nil {
			result.Asset = updated
		}
	}

	writeJSON(w, http.StatusAccepted, assetAcceptedResponse{
		AssetID:         result.Asset.ID,
		ImportSessionID: result.ImportSession.ID,
		ProcessingStage: apiProcessingStage(result.Asset.ProcessingStage),
	})
}

func (s *Server) requireImportContext(w http.ResponseWriter, r *http.Request) (*ingestion.Service, auth.Principal, bool) {
	if s.ingestion == nil {
		s.writeError(w, http.StatusServiceUnavailable, "ingestion_unavailable", "ingestion service is not configured", nil)
		return nil, auth.Principal{}, false
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return nil, auth.Principal{}, false
	}
	return s.ingestion, principal, true
}

func (s *Server) writeImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ingestion.ErrSessionNotFound), errors.Is(err, ingestion.ErrItemNotFound):
		s.writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, ingestion.ErrSessionExpired), errors.Is(err, ingestion.ErrSessionClosed), errors.Is(err, ingestion.ErrUploadValidationFailed):
		s.writeError(w, http.StatusConflict, "import_conflict", err.Error(), nil)
	case errors.Is(err, ingestion.ErrSessionLibraryMismatch):
		s.writeError(w, http.StatusForbidden, "forbidden", err.Error(), nil)
	case errors.Is(err, ingestion.ErrMultipartConfirmationMissing):
		s.writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	default:
		s.writeError(w, http.StatusInternalServerError, "import_failed", err.Error(), nil)
	}
}

func mapImportSession(session ingestion.ImportSession) importSessionResponse {
	return importSessionResponse{
		ID:        session.ID,
		LibraryID: session.LibraryID,
		Status:    apiSessionStatus(session.Status),
		Source:    apiImportSource(session.Source),
		ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func mapUploadTicket(plan ingestion.UploadPlan) uploadTicketResponse {
	response := uploadTicketResponse{
		SessionID:         plan.SessionID,
		ObjectKey:         plan.ObjectKey,
		Method:            plan.Method,
		URL:               plan.URL,
		Headers:           plan.Headers,
		FormFields:        plan.FormFields,
		ExpiresAt:         plan.ExpiresAt,
		ChecksumAlgorithm: plan.ChecksumAlgorithm,
	}
	if plan.Multipart != nil {
		parts := make([]multipartPartResponse, 0, len(plan.Multipart.Parts))
		for _, part := range plan.Multipart.Parts {
			parts = append(parts, multipartPartResponse{
				PartNumber: part.PartNumber,
				UploadURL:  part.UploadURL,
				Headers:    part.Headers,
			})
		}
		response.Multipart = &multipartUploadResponse{
			UploadID:  plan.Multipart.UploadID,
			ExpiresAt: plan.Multipart.ExpiresAt.UTC().Format(time.RFC3339),
			Parts:     parts,
		}
	}
	return response
}

func parseImportSource(value string) (ingestion.Source, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "web-upload":
		return ingestion.SourceWebUpload, nil
	case "desktop-batch":
		return ingestion.SourceDesktopBatch, nil
	case "export-restore":
		return ingestion.SourceExportImport, nil
	default:
		return "", errors.New("source must be one of web-upload, desktop-batch or export-restore")
	}
}

func apiImportSource(value ingestion.Source) string {
	return strings.ReplaceAll(string(value), "_", "-")
}

func apiSessionStatus(value ingestion.SessionStatus) string {
	return strings.ReplaceAll(string(value), "_", "-")
}

func apiProcessingStage(value asset.ProcessingStage) string {
	return strings.ReplaceAll(string(value), "_", "-")
}

func decodeJSON(r *http.Request, dest any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}
