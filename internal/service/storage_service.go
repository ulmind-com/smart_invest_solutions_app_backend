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

// StorageService defines standard file storage operations.
type StorageService interface {
	UploadImage(ctx context.Context, file interface{}, folder string) (string, error)
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
// The 'file' parameter can be a file path, URL, io.Reader, etc.
func (s *CloudinaryService) UploadImage(ctx context.Context, file interface{}, folder string) (string, error) {
	// Generate a unique ID for the file
	uniqueID := uuid.New().String()
	
	// Create context with timeout for upload
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Upload to Cloudinary
	resp, err := s.client.Upload.Upload(uploadCtx, file, uploader.UploadParams{
		Folder:   folder,
		PublicID: uniqueID,
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %v", err)
	}

	return resp.SecureURL, nil
}

// DeleteImage removes a file from Cloudinary using its public ID.
func (s *CloudinaryService) DeleteImage(ctx context.Context, publicID string) error {
	// Create context with timeout for delete
	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := s.client.Upload.Destroy(deleteCtx, uploader.DestroyParams{
		PublicID: publicID,
	})
	
	if err != nil {
		return fmt.Errorf("failed to delete image: %v", err)
	}
	
	if resp.Result != "ok" {
		return fmt.Errorf("failed to delete image, cloudinary response: %s", resp.Result)
	}

	return nil
}
