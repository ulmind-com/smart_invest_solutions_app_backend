package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
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

// UploadDocumentWithCompression uploads a PDF/image file to Cloudinary with aggressive quality compression targeting sub-500KB storage size.
func (s *CloudinaryService) UploadDocumentWithCompression(ctx context.Context, file interface{}, folder string) (*UploadResult, error) {
	uniqueID := uuid.New().String()

	uploadCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Handle in-memory buffer compression if file is an io.Reader or []byte
	processedFile := file
	if reader, ok := file.(io.Reader); ok {
		buf, err := io.ReadAll(reader)
		if err == nil && len(buf) > 500*1024 {
			// If raw file size is > 500KB, attempt image compression
			compressedBuf, isCompressed := compressImageBufferSub500KB(buf)
			if isCompressed {
				processedFile = bytes.NewReader(compressedBuf)
			} else {
				processedFile = bytes.NewReader(buf)
			}
		} else if err == nil {
			processedFile = bytes.NewReader(buf)
		}
	}

	// Upload to Cloudinary with aggressive sub-500KB target transformations (q_auto:eco, w_1920, c_limit)
	resp, err := s.client.Upload.Upload(uploadCtx, processedFile, uploader.UploadParams{
		Folder:         folder,
		PublicID:       uniqueID,
		Transformation: "w_1920,c_limit,q_auto:eco,f_auto",
		ResourceType:   "auto",
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

// compressImageBufferSub500KB attempts to compress JPG/PNG image bytes to under 500KB using Go image encoding.
func compressImageBufferSub500KB(input []byte) ([]byte, bool) {
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return input, false // Not a standard image (e.g. PDF or raw binary), rely on Cloudinary q_auto:eco
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Scale down dimensions if image resolution is very large
	maxWidth := 1920
	maxHeight := 1920
	if width > maxWidth || height > maxHeight {
		// Calculate ratio
		ratioW := float64(maxWidth) / float64(width)
		ratioH := float64(maxHeight) / float64(height)
		ratio := ratioW
		if ratioH < ratioW {
			ratio = ratioH
		}
		newW := int(float64(width) * ratio)
		newH := int(float64(height) * ratio)
		
		// Simple nearest neighbor or bounds crop to fit max dimensions
		dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
		for y := 0; y < newH; y++ {
			for x := 0; x < newW; x++ {
				srcX := int(float64(x) / ratio)
				srcY := int(float64(y) / ratio)
				dst.Set(x, y, img.At(srcX, srcY))
			}
		}
		img = dst
	}

	// Try decreasing qualities (70, 55, 40, 30) until size is under 500KB (512,000 bytes)
	qualities := []int{70, 55, 40, 30, 20}
	for _, q := range qualities {
		var outBuf bytes.Buffer
		err := jpeg.Encode(&outBuf, img, &jpeg.Options{Quality: q})
		if err == nil {
			if outBuf.Len() <= 500*1024 || q == 20 {
				return outBuf.Bytes(), true
			}
		}
	}

	return input, false
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
