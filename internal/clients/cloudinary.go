package clients

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

// ErrUploadFailed is returned when a Cloudinary upload operation fails.
// Callers should map this to HTTP 502.
var ErrUploadFailed = errors.New("cloudinary: upload failed")

// CloudinaryClient defines the interface for uploading assets to Cloudinary.
type CloudinaryClient interface {
	// UploadImage uploads an image from r to the showcaster/images folder and
	// returns the public HTTPS URL of the uploaded asset.
	UploadImage(ctx context.Context, r io.Reader, filename string) (string, error)

	// UploadVideoFromURL uploads a video from sourceURL to the showcaster/videos
	// folder and returns the public HTTPS URL of the uploaded asset.
	UploadVideoFromURL(ctx context.Context, sourceURL string) (string, error)
}

// CloudinaryClientImpl is the production implementation of CloudinaryClient
// backed by the official cloudinary-go/v2 SDK.
type CloudinaryClientImpl struct {
	cld *cloudinary.Cloudinary
}

// NewCloudinaryClient initialises the Cloudinary SDK with the provided
// credentials and returns a ready-to-use *CloudinaryClientImpl.
func NewCloudinaryClient(cloudName, apiKey, apiSecret string) (*CloudinaryClientImpl, error) {
	cld, err := cloudinary.NewFromParams(cloudName, apiKey, apiSecret)
	if err != nil {
		return nil, fmt.Errorf("cloudinary: init: %w", err)
	}
	return &CloudinaryClientImpl{cld: cld}, nil
}

// UploadImage uploads the content of r to the showcaster/images folder in
// Cloudinary. filename is used as the public_id of the asset. It returns the
// secure (HTTPS) URL of the uploaded image or ErrUploadFailed on error.
func (c *CloudinaryClientImpl) UploadImage(ctx context.Context, r io.Reader, filename string) (string, error) {
	result, err := c.cld.Upload.Upload(ctx, r, uploader.UploadParams{
		PublicID:     filename,
		Folder:       "showcaster/images",
		ResourceType: "image",
	})
	if err != nil {
		return "", fmt.Errorf("%w: image: %v", ErrUploadFailed, err)
	}
	if result.SecureURL == "" {
		return "", fmt.Errorf("%w: image: empty secure URL in response", ErrUploadFailed)
	}
	return result.SecureURL, nil
}

// UploadVideoFromURL uploads a video from sourceURL to the showcaster/videos
// folder in Cloudinary. It returns the secure (HTTPS) URL of the uploaded
// video or ErrUploadFailed on error.
func (c *CloudinaryClientImpl) UploadVideoFromURL(ctx context.Context, sourceURL string) (string, error) {
	result, err := c.cld.Upload.Upload(ctx, sourceURL, uploader.UploadParams{
		Folder:       "showcaster/videos",
		ResourceType: "video",
	})
	if err != nil {
		return "", fmt.Errorf("%w: video: %v", ErrUploadFailed, err)
	}
	if result.SecureURL == "" {
		return "", fmt.Errorf("%w: video: empty secure URL in response", ErrUploadFailed)
	}
	return result.SecureURL, nil
}
