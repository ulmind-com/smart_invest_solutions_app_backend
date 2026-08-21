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
}

// NewDocumentService initializes a new instance of DocumentService.
func NewDocumentService(repo domain.DocumentRepository, storageSvc StorageService) domain.DocumentService {
	return &documentService{
		repo:       repo,
		storageSvc: storageSvc,
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

// UpdateDocument modifies an existing document record.
// If newFile is provided, the OLD file is purged from Cloudinary first, and the new compressed file is uploaded.
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

	// If a new file is uploaded, purge the old file from Cloudinary first!
	if newFile != nil {
		if existingDoc.PublicID != "" {
			_ = s.storageSvc.DeleteImage(ctx, existingDoc.PublicID)
		}

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
	}

	return s.repo.Update(ctx, id, userID, dto)
}

// DeleteDocument removes a document from MongoDB AND purges the file from Cloudinary!
func (s *documentService) DeleteDocument(ctx context.Context, idStr, userIDStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid document ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	// Fetch existing document to get PublicID for Cloudinary purging
	existingDoc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if existingDoc.UserID != userID {
		return fmt.Errorf("access denied: document does not belong to you")
	}

	// Purge asset from Cloudinary
	if existingDoc.PublicID != "" {
		_ = s.storageSvc.DeleteImage(ctx, existingDoc.PublicID)
	}

	// Remove record from MongoDB
	return s.repo.Delete(ctx, id, userID)
}

// GetDocumentsByUserIDAdmin allows admins to view documents of any client.
func (s *documentService) GetDocumentsByUserIDAdmin(ctx context.Context, targetUserIDStr, searchQuery string) (*domain.DocumentListResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target user ID format: %w", err)
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
