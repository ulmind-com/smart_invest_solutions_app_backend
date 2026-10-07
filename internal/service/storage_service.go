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
	"path"
	"strings"
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
	// ResourceType is what the storage provider filed this asset as — "image" or "video". It has to
	// be kept, because deleting an asset requires naming its type: a delete sent as "image" for a
	// video is accepted and does nothing, leaving the file behind forever.
	ResourceType string `json:"resource_type,omitempty"`
	// ThumbnailURL is a still frame for a video, so a banner can show something before anyone presses
	// play. Empty for images, which are their own thumbnail.
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// Resource types an upload can be filed under.
const (
	ResourceTypeImage = "image"
	ResourceTypeVideo = "video"
)

// StorageService defines standard file storage operations.
type StorageService interface {
	UploadImage(ctx context.Context, file interface{}, folder string) (string, error)
	UploadDocumentWithCompression(ctx context.Context, file interface{}, folder string) (*UploadResult, error)
	// UploadMedia uploads an image or a video, detecting which from the bytes rather than trusting a
	// filename, and reports the resource type so the asset can later be deleted correctly.
	UploadMedia(ctx context.Context, file io.Reader, folder string) (*UploadResult, error)
	DeleteImage(ctx context.Context, publicID string) error
	// DeleteMedia removes an asset of a known resource type. DeleteImage can only remove images.
	DeleteMedia(ctx context.Context, publicID, resourceType string) error
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

func (u unavailableStorage) UploadMedia(context.Context, io.Reader, string) (*UploadResult, error) {
	return nil, fmt.Errorf("file storage is not configured on the server: %v", u.reason)
}

func (u unavailableStorage) DeleteImage(context.Context, string) error {
	return fmt.Errorf("file storage is not configured on the server: %v", u.reason)
}

func (u unavailableStorage) DeleteMedia(context.Context, string, string) error {
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

// maxBannerVideoBytes caps an uploaded video. A screen banner is a few seconds long; anything near
// this is a file somebody meant to trim first, and letting it through would mean a client on mobile
// data paying for it.
const maxBannerVideoBytes = 20 << 20 // 20 MiB

// UploadMedia uploads an image or a video for display in the app.
//
// Two things make this different from UploadDocumentWithCompression, and both are the reason it
// exists separately rather than as a flag:
//
//   - The resource type is set explicitly from the file's own bytes, and returned. Cloudinary's
//     "auto" guesses correctly on upload, but a *delete* has to name the type — so without carrying
//     it, removing a video would be accepted and quietly do nothing.
//   - The image size transformation is not applied to video. On a video Cloudinary would transcode
//     it, which is slow and lossy for no benefit here; videos get their own longer timeout instead,
//     since even a small one takes longer to transfer than a photo.
func (s *CloudinaryService) UploadMedia(ctx context.Context, file io.Reader, folder string) (*UploadResult, error) {
	buf, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read the uploaded file: %w", err)
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("the uploaded file is empty")
	}

	// Detected from the bytes, not from the filename: a .jpg that is really a video (or the reverse)
	// would otherwise be filed under the wrong type and become undeletable.
	contentType := http.DetectContentType(buf)
	isVideo := strings.HasPrefix(contentType, "video/")

	resourceType := ResourceTypeImage
	timeout := 45 * time.Second
	params := uploader.UploadParams{
		Folder:   folder,
		PublicID: uuid.New().String(),
	}

	if isVideo {
		if len(buf) > maxBannerVideoBytes {
			return nil, fmt.Errorf("that video is %.1f MB — please upload one under %d MB", float64(len(buf))/(1<<20), maxBannerVideoBytes>>20)
		}
		resourceType = ResourceTypeVideo
		timeout = 3 * time.Minute
	} else {
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("that file is neither an image nor a video (detected %s)", contentType)
		}
		// Same cap the rest of the app applies to images, and the compression that goes with it.
		params.Transformation = "c_limit,w_1920,h_1920,q_auto:good"
		if len(buf) > compressionThresholdBytes {
			if compressed, ok := compressImageBuffer(buf); ok {
				buf = compressed
			}
		}
	}
	params.ResourceType = resourceType

	uploadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.client.Upload.Upload(uploadCtx, bytes.NewReader(buf), params)
	if err != nil {
		return nil, fmt.Errorf("failed to upload media: %v", err)
	}

	result := &UploadResult{
		SecureURL:    resp.SecureURL,
		PublicID:     resp.PublicID,
		Bytes:        int64(resp.Bytes),
		Format:       resp.Format,
		ResourceType: resourceType,
	}
	if isVideo {
		result.ThumbnailURL = videoPosterURL(resp.SecureURL)
	}

	return result, nil
}

// videoPosterURL turns a Cloudinary video URL into a still of its first frame, by asking for the same
// asset as a JPEG. It costs no extra storage — the provider renders it on request — and gives a banner
// something to show before anyone presses play. Returns "" if the URL has no extension to swap.
func videoPosterURL(secureURL string) string {
	ext := path.Ext(secureURL)
	if ext == "" {
		return ""
	}
	return strings.TrimSuffix(secureURL, ext) + ".jpg"
}

// DeleteMedia removes an asset, naming its resource type.
//
// This is the half that makes video deletion work: Cloudinary's Destroy defaults to "image", and a
// video delete sent without the type comes back "not found" — an answer that looks like success and
// leaves the file in storage for good.
func (s *CloudinaryService) DeleteMedia(ctx context.Context, publicID, resourceType string) error {
	if publicID == "" {
		return nil
	}
	if resourceType == "" {
		resourceType = ResourceTypeImage
	}

	deleteCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resp, err := s.client.Upload.Destroy(deleteCtx, uploader.DestroyParams{
		PublicID:     publicID,
		ResourceType: resourceType,
	})
	if err != nil {
		return fmt.Errorf("failed to delete %s from storage: %v", resourceType, err)
	}
	if resp.Result != "ok" && resp.Result != "not found" {
		return fmt.Errorf("storage delete error: %s", resp.Result)
	}

	return nil
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
