package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/google/uuid"
	"github.com/smart-invest-solutions/backend/internal/config"
)

// UploadResult represents the metadata output after uploading a file to Cloudinary.
type UploadResult struct {
	SecureURL string `json:"secure_url"`
	PublicID  string `json:"public_id"`
	Bytes     int64  `json:"bytes"`
	Format    string `json:"format"`
}

// StorageService defines standard file storage operations.
type StorageService interface {
	UploadImage(ctx context.Context, file interface{}, folder string) (string, error)
	UploadDocumentWithCompression(ctx context.Context, file interface{}, folder string) (*UploadResult, error)
	DeleteImage(ctx context.Context, publicID string) error
}

// CloudinaryService implements StorageService using Cloudinary.
type CloudinaryService struct {
	client *cloudinary.Cloudinary
}

// NewCloudinaryService initializes a new Cloudinary service.
func NewCloudinaryService(cfg *config.Config) (*CloudinaryService, error) {
	if cfg.CloudinaryURL == "" {
		return nil, fmt.Errorf("CLOUDINARY_URL environment variable is not set")
	}

	cld, err := cloudinary.NewFromURL(cfg.CloudinaryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Cloudinary: %v", err)
	}

	return &CloudinaryService{
		client: cld,
	}, nil
}

// UploadImage uploads a file to Cloudinary and returns its secure URL.
func (s *CloudinaryService) UploadImage(ctx context.Context, file interface{}, folder string) (string, error) {
	uniqueID := uuid.New().String()

	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.client.Upload.Upload(uploadCtx, file, uploader.UploadParams{
		Folder:   folder,
		PublicID: uniqueID,
	})

	if err != nil {
		return "", fmt.Errorf("failed to upload image: %v", err)
	}

	return resp.SecureURL, nil
}

// UploadDocumentWithCompression uploads a PDF/image file to Cloudinary with auto quality compression.
func (s *CloudinaryService) UploadDocumentWithCompression(ctx context.Context, file interface{}, folder string) (*UploadResult, error) {
	uniqueID := uuid.New().String()

	uploadCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := s.client.Upload.Upload(uploadCtx, file, uploader.UploadParams{
		Folder:       folder,
		PublicID:     uniqueID,
		Transformation: "q_auto,f_auto",
		ResourceType: "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload document to Cloudinary: %v", err)
	}

	return &UploadResult{
		SecureURL: resp.SecureURL,
		PublicID:  resp.PublicID,
		Bytes:     int64(resp.Bytes),
		Format:    resp.Format,
	}, nil
}

// DeleteImage removes a file from Cloudinary using its public ID.
func (s *CloudinaryService) DeleteImage(ctx context.Context, publicID string) error {
	if publicID == "" {
		return nil
	}

	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := s.client.Upload.Destroy(deleteCtx, uploader.DestroyParams{
		PublicID: publicID,
	})

	if err != nil {
		return fmt.Errorf("failed to delete file from Cloudinary: %v", err)
	}

	if resp.Result != "ok" && resp.Result != "not found" {
		return fmt.Errorf("cloudinary delete error: %s", resp.Result)
	}

	return nil
}
