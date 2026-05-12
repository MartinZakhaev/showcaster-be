package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"showcaster-be/internal/clients"
	"showcaster-be/internal/dto"
)

const maxImageSize = 10 * 1024 * 1024 // 10 MB

// UploadHandler handles image upload requests.
type UploadHandler struct {
	CloudinaryClient clients.CloudinaryClient
}

// UploadImage handles POST /api/upload/image.
//
//	@Summary		Upload a model or product image
//	@Description	Uploads a JPEG or PNG image to Cloudinary and returns the public HTTPS URL.
//	@Description	Use the returned URL as `modelImageUrl` or `productImageUrl` when submitting a job.
//	@Description
//	@Description	**Constraints:**
//	@Description	- File must be sent in the `file` multipart form field.
//	@Description	- Accepted MIME types: `image/jpeg`, `image/png` (detected from file content, not the header).
//	@Description	- Maximum file size: 10 MB.
//	@Tags			Upload
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			file	formData	file						true	"Image file (JPEG or PNG, max 10 MB)"
//	@Success		200		{object}	dto.UploadImageResponse		"Upload successful — use the URL in job submission"
//	@Failure		400		{object}	dto.ErrorResponse			"Missing `file` field in form data"
//	@Failure		401		{object}	dto.ErrorResponse			"Missing or invalid JWT"
//	@Failure		413		{object}	dto.ErrorResponse			"File exceeds the 10 MB size limit"
//	@Failure		415		{object}	dto.ErrorResponse			"Unsupported file type (only JPEG and PNG accepted)"
//	@Failure		502		{object}	dto.ErrorResponse			"Cloudinary upload failed"
//	@Router			/upload/image [post]
func (h *UploadHandler) UploadImage(c *fiber.Ctx) error {
	header, err := c.FormFile("file")
	if err != nil || header == nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("file field is required"))
	}

	if header.Size > maxImageSize {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(dto.Fail("file size exceeds the 10 MB limit"))
	}

	f, err := header.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("failed to read uploaded file"))
	}
	defer f.Close()

	// Detect MIME type from the first 512 bytes — more reliable than trusting
	// the Content-Type header sent by the client.
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("failed to read uploaded file"))
	}
	mimeType := http.DetectContentType(buf[:n])

	if mimeType != "image/jpeg" && mimeType != "image/png" {
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(
			dto.Fail("unsupported file type; only JPEG and PNG are accepted"),
		)
	}

	// Seek back to the beginning so the full file is uploaded.
	if seeker, ok := f.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("failed to read uploaded file"))
		}
	}

	filename := uuid.New().String()
	url, err := h.CloudinaryClient.UploadImage(c.Context(), f, filename)
	if err != nil {
		if errors.Is(err, clients.ErrUploadFailed) {
			return c.Status(fiber.StatusBadGateway).JSON(dto.Fail("upstream image upload service failed"))
		}
		return c.Status(fiber.StatusBadGateway).JSON(dto.Fail("upstream image upload service failed"))
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(fiber.Map{"url": url}))
}
