package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type documentService struct {
	repo       domain.DocumentRepository
	storageSvc StorageService
	userRepo   domain.UserRepository
}

// NewDocumentService initializes a new instance of DocumentService.
func NewDocumentService(repo domain.DocumentRepository, storageSvc StorageService, userRepo domain.UserRepository) domain.DocumentService {
	return &documentService{
		repo:       repo,
		storageSvc: storageSvc,
		userRepo:   userRepo,
	}
}

// UploadDocument uploads a file to Cloudinary with compression and creates a Document record in MongoDB.
func (s *documentService) UploadDocument(ctx context.Context, userIDStr, name, category string, file io.Reader, filename string) (*domain.Document, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	folder := fmt.Sprintf("smart_invest_documents/%s", userIDStr)

	// Upload file to Cloudinary with auto quality compression
	uploadRes, err := s.storageSvc.UploadDocumentWithCompression(ctx, file, folder)
	if err != nil {
		return nil, fmt.Errorf("failed to upload document to Cloudinary: %w", err)
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		ext = uploadRes.Format
	}

	if category == "" {
		category = "General"
	}

	doc := &domain.Document{
		UserID:      userID,
		Name:        name,
		Category:    category,
		DocumentURL: uploadRes.SecureURL,
		PublicID:    uploadRes.PublicID,
		FileType:    ext,
		FileSize:    uploadRes.Bytes,
	}

	return s.repo.Create(ctx, doc)
}

// GetMyDocuments retrieves all documents belonging to the user with optional search filtering.
func (s *documentService) GetMyDocuments(ctx context.Context, userIDStr, searchQuery string) (*domain.DocumentListResponse, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	docs, total, err := s.repo.FindAllByUserID(ctx, userID, searchQuery)
	if err != nil {
		return nil, err
	}

	return &domain.DocumentListResponse{
		Total: total,
		Data:  docs,
	}, nil
}

// GetDocumentByID retrieves a single document record by ID with ownership verification.
func (s *documentService) GetDocumentByID(ctx context.Context, idStr, userIDStr string) (*domain.Document, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid document ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	doc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if doc.UserID != userID {
		return nil, fmt.Errorf("access denied: document does not belong to you")
	}

	return doc, nil
}

// UpdateDocument modifies an existing document record. If newFile is provided, the new file is
// uploaded and the DB record is switched over to it FIRST; only once that has fully succeeded is
// the OLD Cloudinary asset purged. This ordering is deliberate: if the upload or the DB update
// fails partway through, the user's existing document is left completely intact rather than
// destroyed — the old asset is only ever deleted once nothing can still fail.
func (s *documentService) UpdateDocument(ctx context.Context, idStr, userIDStr string, dto *domain.UpdateDocumentDTO, newFile io.Reader, filename string) (*domain.Document, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid document ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	// Fetch existing document to check ownership & get old PublicID
	existingDoc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if existingDoc.UserID != userID {
		return nil, fmt.Errorf("access denied: document does not belong to you")
	}

	oldPublicID := ""
	if newFile != nil {
		folder := fmt.Sprintf("smart_invest_documents/%s", userIDStr)
		uploadRes, err := s.storageSvc.UploadDocumentWithCompression(ctx, newFile, folder)
		if err != nil {
			return nil, fmt.Errorf("failed to upload new document file: %w", err)
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
		if ext == "" {
			ext = uploadRes.Format
		}

		dto.DocumentURL = &uploadRes.SecureURL
		dto.PublicID = &uploadRes.PublicID
		dto.FileType = &ext
		dto.FileSize = &uploadRes.Bytes
		oldPublicID = existingDoc.PublicID
	}

	updated, err := s.repo.Update(ctx, id, userID, dto)
	if err != nil {
		if newFile != nil {
			// The DB switch-over failed after a successful upload — clean up the now-orphaned new
			// asset (best-effort) rather than leaving it stranded; the old asset is untouched, so
			// the user's document is still exactly as it was before this request.
			_ = s.storageSvc.DeleteImage(ctx, *dto.PublicID)
		}
		return nil, err
	}

	// Only now, after the DB record is confirmed pointing at the new file, is the old one purged.
	if oldPublicID != "" {
		_ = s.storageSvc.DeleteImage(ctx, oldPublicID)
	}

	return updated, nil
}

// DeleteDocument removes a document from MongoDB AND purges the file from Cloudinary. The DB
// record is removed first: if that fails, nothing has changed yet and the request can simply be
// retried; if it succeeds but the Cloudinary purge then fails, the result is only a harmless
// orphaned asset (no data loss), which is a far safer failure mode than the reverse order.
func (s *documentService) DeleteDocument(ctx context.Context, idStr, userIDStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid document ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	// Fetch existing document to check ownership & get PublicID for Cloudinary purging
	existingDoc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if existingDoc.UserID != userID {
		return fmt.Errorf("access denied: document does not belong to you")
	}

	if err := s.repo.Delete(ctx, id, userID); err != nil {
		return err
	}

	if existingDoc.PublicID != "" {
		_ = s.storageSvc.DeleteImage(ctx, existingDoc.PublicID)
	}

	return nil
}

// GetDocumentsByUserIDAdmin allows admin/super_admin to view documents of any client. A plain
// admin may only look up a client belonging to their own agency (same resolveCallerAgencyID/
// canAccessAgencyScopedRecord pattern used everywhere else).
func (s *documentService) GetDocumentsByUserIDAdmin(ctx context.Context, requesterRole, requesterID, targetUserIDStr, searchQuery string) (*domain.DocumentListResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target user ID format: %w", err)
	}

	if requesterRole == domain.RoleAdmin {
		targetUser, err := s.userRepo.FindByID(ctx, targetUserID)
		if err != nil || targetUser == nil {
			return nil, fmt.Errorf("user not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("user not found")
		}
	}

	docs, total, err := s.repo.FindAllByUserID(ctx, targetUserID, searchQuery)
	if err != nil {
		return nil, err
	}

	return &domain.DocumentListResponse{
		Total: total,
		Data:  docs,
	}, nil
}

// DeleteAllByUserID purges all Cloudinary files belonging to a user and deletes all document records from MongoDB.
func (s *documentService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	// Fetch all documents for this user to purge their Cloudinary files
	docs, _, err := s.repo.FindAllByUserID(ctx, userID, "")
	if err == nil {
		for _, doc := range docs {
			if doc.PublicID != "" {
				_ = s.storageSvc.DeleteImage(ctx, doc.PublicID)
			}
		}
	}

	return s.repo.DeleteAllByUserID(ctx, userID)
}
