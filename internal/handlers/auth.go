package handlers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"showcaster-be/internal/dto"
	"showcaster-be/internal/services"
)

// AuthHandler handles all authentication-related HTTP routes.
type AuthHandler struct {
	AuthService services.AuthService
	Validator   *validator.Validate
}

// Register handles POST /api/auth/register.
//
//	@Summary		Register a new user
//	@Description	Creates a new user account with the provided email, full name, and password.
//	@Description	On success a 6-digit numeric OTP is sent to the registered email address.
//	@Description	The account must be verified via POST /api/auth/verify-otp before login is possible.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.RegisterRequest				true	"Registration payload"
//	@Success		201		{object}	dto.RegisterResponse			"Account created — OTP sent to email"
//	@Failure		400		{object}	dto.ValidationErrorResponse		"Validation error (missing or invalid field)"
//	@Failure		409		{object}	dto.ErrorResponse				"Email already registered"
//	@Failure		503		{object}	dto.ErrorResponse				"Database unavailable"
//	@Router			/auth/register [post]
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req dto.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			first := ve[0]
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("validation failed: "+first.Field()+" "+first.Tag(), first.Field()),
			)
		}
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.AuthService.Register(c.Context(), req); err != nil {
		switch {
		case errors.Is(err, services.ErrEmailTaken):
			return c.Status(fiber.StatusConflict).JSON(dto.Fail("email already registered"))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("internal server error"))
		}
	}

	return c.Status(fiber.StatusCreated).JSON(
		dto.OK(fiber.Map{"message": "registration successful; please verify your email with the OTP sent"}),
	)
}

// Login handles POST /api/auth/login.
//
//	@Summary		Login
//	@Description	Authenticates a registered and verified user with email and password.
//	@Description	Returns a signed HS256 JWT valid for exactly 24 hours (86400 seconds).
//	@Description	Both wrong-password and unknown-email return the same 401 body to prevent user enumeration.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.LoginRequest			true	"Login credentials"
//	@Success		200		{object}	dto.LoginResponse			"JWT issued — include as 'Authorization: Bearer <token>' on protected routes"
//	@Failure		400		{object}	dto.ValidationErrorResponse	"Missing or invalid field"
//	@Failure		401		{object}	dto.ErrorResponse			"Invalid credentials (wrong password or unknown email)"
//	@Failure		503		{object}	dto.ErrorResponse			"Database unavailable"
//	@Router			/auth/login [post]
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req dto.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			first := ve[0]
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("validation failed: "+first.Field()+" "+first.Tag(), first.Field()),
			)
		}
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	tokenResp, err := h.AuthService.Login(c.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentials):
			return c.Status(fiber.StatusUnauthorized).JSON(dto.Fail("invalid credentials"))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("internal server error"))
		}
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(tokenResp))
}

// VerifyOTP handles POST /api/auth/verify-otp.
//
//	@Summary		Verify email OTP
//	@Description	Verifies the 6-digit numeric OTP that was sent to the user's email during registration.
//	@Description	On success the account is marked as verified and a JWT is returned (same shape as login).
//	@Description	The OTP expires after 10 minutes. After 5 consecutive wrong attempts the OTP is permanently invalidated.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.VerifyOTPRequest		true	"Email and 6-digit OTP"
//	@Success		200		{object}	dto.VerifyOTPResponse		"Account verified — JWT issued"
//	@Failure		400		{object}	dto.ErrorResponse			"OTP expired, locked (≥5 failures), or invalid"
//	@Failure		404		{object}	dto.ErrorResponse			"Email not found"
//	@Failure		409		{object}	dto.ErrorResponse			"Account already verified"
//	@Failure		503		{object}	dto.ErrorResponse			"Database unavailable"
//	@Router			/auth/verify-otp [post]
func (h *AuthHandler) VerifyOTP(c *fiber.Ctx) error {
	var req dto.VerifyOTPRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			first := ve[0]
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("validation failed: "+first.Field()+" "+first.Tag(), first.Field()),
			)
		}
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	tokenResp, err := h.AuthService.VerifyOTP(c.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrOTPExpired):
			return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("OTP has expired"))
		case errors.Is(err, services.ErrOTPLocked):
			return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("OTP is no longer valid due to too many failed attempts"))
		case errors.Is(err, services.ErrOTPInvalid):
			return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid OTP"))
		case errors.Is(err, services.ErrAlreadyVerified):
			return c.Status(fiber.StatusConflict).JSON(dto.Fail("account is already verified"))
		case errors.Is(err, services.ErrNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.Fail("user not found"))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("internal server error"))
		}
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(tokenResp))
}

// ResendOTP handles POST /api/auth/resend-otp.
//
//	@Summary		Resend OTP
//	@Description	Generates a fresh 6-digit OTP, resets the failure counter, and sends it to the user's email.
//	@Description	Use this when the original OTP has expired or was never received.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.ResendOTPRequest		true	"Registered email address"
//	@Success		200		{object}	dto.ResendOTPResponse		"New OTP sent"
//	@Failure		400		{object}	dto.ValidationErrorResponse	"Missing or invalid email"
//	@Failure		404		{object}	dto.ErrorResponse			"Email not found"
//	@Failure		503		{object}	dto.ErrorResponse			"Database unavailable"
//	@Router			/auth/resend-otp [post]
func (h *AuthHandler) ResendOTP(c *fiber.Ctx) error {
	var req dto.ResendOTPRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.Validator.Struct(req); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			first := ve[0]
			return c.Status(fiber.StatusBadRequest).JSON(
				dto.FailField("validation failed: "+first.Field()+" "+first.Tag(), first.Field()),
			)
		}
		return c.Status(fiber.StatusBadRequest).JSON(dto.Fail("invalid request body"))
	}

	if err := h.AuthService.ResendOTP(c.Context(), req.Email); err != nil {
		switch {
		case errors.Is(err, services.ErrNotFound):
			return c.Status(fiber.StatusNotFound).JSON(dto.Fail("user not found"))
		case errors.Is(err, services.ErrDBUnavailable):
			return c.Status(fiber.StatusServiceUnavailable).JSON(dto.Fail("service temporarily unavailable"))
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(dto.Fail("internal server error"))
		}
	}

	return c.Status(fiber.StatusOK).JSON(dto.OK(fiber.Map{"message": "OTP resent successfully"}))
}
