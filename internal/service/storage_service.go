package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/config"
)

// UploadResult represents the metadata output after uploading a file to Cloudinary.
// Images above compressionThresholdBytes are re-encoded, aiming for compressionTargetBytes.
const (
	compressionThresholdBytes = 1 << 20 // 1 MiB
	compressionTargetBytes    = 1 << 20
)

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

// NewStorageService returns the Cloudinary-backed StorageService, or — when Cloudinary isn't
// configured — a stand-in whose every call fails with a clear error. Returning the constructor's nil
// *CloudinaryService instead (as the router used to) produced a non-nil interface wrapping a nil
// pointer: every upload or delete then panicked, and `storageSvc != nil` guards never caught it.
func NewStorageService(cfg *config.Config) StorageService {
	svc, err := NewCloudinaryService(cfg)
	if err != nil {
		log.Error().Err(err).Msg("file storage unavailable — document and brochure uploads will be refused")
		return unavailableStorage{reason: err}
	}
	return svc
}

type unavailableStorage struct{ reason error }

func (u unavailableStorage) UploadImage(context.Context, interface{}, string) (string, error) {
	return "", fmt.Errorf("file storage is not configured on the server: %v", u.reason)
}

func (u unavailableStorage) UploadDocumentWithCompression(context.Context, interface{}, string) (*UploadResult, error) {
	return nil, fmt.Errorf("file storage is not configured on the server: %v", u.reason)
}

func (u unavailableStorage) DeleteImage(context.Context, string) error {
	return fmt.Errorf("file storage is not configured on the server: %v", u.reason)
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
	isPDF := false
	if reader, ok := file.(io.Reader); ok {
		buf, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read uploaded file: %w", err)
		}
		isPDF = http.DetectContentType(buf) == "application/pdf"
		processedFile = bytes.NewReader(buf)
		if !isPDF && len(buf) > compressionThresholdBytes {
			if compressedBuf, ok := compressImageBuffer(buf); ok {
				processedFile = bytes.NewReader(compressedBuf)
			}
		}
	}

	params := uploader.UploadParams{
		Folder:       folder,
		PublicID:     uniqueID,
		ResourceType: "auto",
	}
	// The size cap is an image-only transformation: applied to a PDF it would have Cloudinary
	// rasterise or reject the document, so PDFs are stored exactly as uploaded.
	if !isPDF {
		params.Transformation = "c_limit,w_1920,h_1920,q_auto:good"
	}

	resp, err := s.client.Upload.Upload(uploadCtx, processedFile, params)
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

// compressImageBuffer re-encodes a large JPG/PNG as a JPEG no wider/taller than 1920px, aiming for
// compressionTargetBytes. It reports false (and the original bytes) when the input isn't a decodable
// image or re-encoding wouldn't make it smaller.
func compressImageBuffer(input []byte) ([]byte, bool) {
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return input, false // Not a decodable image; Cloudinary's own optimisation handles it
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	maxWidth := 1920
	maxHeight := 1920
	if width > maxWidth || height > maxHeight {
		ratioW := float64(maxWidth) / float64(width)
		ratioH := float64(maxHeight) / float64(height)
		ratio := ratioW
		if ratioH < ratioW {
			ratio = ratioH
		}
		newW := int(float64(width) * ratio)
		newH := int(float64(height) * ratio)

		dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
		for y := 0; y < newH; y++ {
			for x := 0; x < newW; x++ {
				srcX := bounds.Min.X + int(float64(x)/ratio)
				srcY := bounds.Min.Y + int(float64(y)/ratio)
				dst.Set(x, y, img.At(srcX, srcY))
			}
		}
		img = dst
	}

	// JPEG has no alpha channel: flatten onto white so transparent regions of a PNG don't turn black.
	flat := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	img = flat

	// Vault files are KYC scans and policy papers: text must stay legible, so quality never drops
	// below 60 — a slightly larger file beats an unreadable Aadhaar card.
	var best []byte
	for _, q := range []int{85, 75, 65, 60} {
		var outBuf bytes.Buffer
		if err := jpeg.Encode(&outBuf, img, &jpeg.Options{Quality: q}); err != nil {
			continue
		}
		best = outBuf.Bytes()
		if len(best) <= compressionTargetBytes {
			break
		}
	}

	if best == nil || len(best) >= len(input) {
		return input, false
	}
	return best, true
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
