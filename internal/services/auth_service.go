package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"showcaster-be/internal/dto"
	"showcaster-be/internal/models"
)

// Sentinel errors returned by AuthService methods. Handlers map these to HTTP
// status codes so the service layer stays HTTP-agnostic.
var (
	// ErrEmailTaken is returned by Register when the email already exists.
	ErrEmailTaken = errors.New("email already registered")

	// ErrInvalidCredentials is returned by Login for both wrong-password and
	// unknown-email cases (prevents user enumeration).
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrOTPExpired is returned by VerifyOTP when the OTP is past its 10-minute window.
	ErrOTPExpired = errors.New("OTP has expired")

	// ErrOTPLocked is returned by VerifyOTP when the fail count has reached 5.
	ErrOTPLocked = errors.New("OTP is no longer valid due to too many failed attempts")

	// ErrOTPInvalid is returned by VerifyOTP when the submitted code does not match.
	ErrOTPInvalid = errors.New("invalid OTP")

	// ErrAlreadyVerified is returned by VerifyOTP when the account is already verified.
	ErrAlreadyVerified = errors.New("account is already verified")

	// ErrNotFound is returned when a user record cannot be located.
	ErrNotFound = errors.New("user not found")

	// ErrDBUnavailable is returned when a database operation fails in a way that
	// suggests the DB is unreachable.
	ErrDBUnavailable = errors.New("service temporarily unavailable")
)

// AuthService defines the contract for user authentication operations.
type AuthService interface {
	// Register creates a new user, hashes the password, generates an OTP, and
	// (stub) sends a verification email. Returns ErrEmailTaken on duplicate email.
	Register(ctx context.Context, req dto.RegisterRequest) error

	// Login verifies credentials and issues a signed JWT on success.
	// Returns ErrInvalidCredentials for both wrong-password and unknown-email.
	Login(ctx context.Context, req dto.LoginRequest) (dto.TokenResponse, error)

	// VerifyOTP validates the submitted OTP, marks the account verified, and
	// returns a JWT. Returns ErrOTPExpired, ErrOTPLocked, ErrOTPInvalid,
	// ErrAlreadyVerified, or ErrNotFound as appropriate.
	VerifyOTP(ctx context.Context, req dto.VerifyOTPRequest) (dto.TokenResponse, error)

	// ResendOTP generates a fresh OTP for the given email, resets the fail count
	// and expiry, and (stub) sends a new verification email.
	ResendOTP(ctx context.Context, email string) error
}

// authService is the concrete implementation of AuthService.
type authService struct {
	db        *gorm.DB
	jwtSecret []byte
	logger    *slog.Logger
}

// NewAuthService constructs an AuthService backed by the given GORM DB.
// jwtSecret is the HS256 signing key; logger is used for OTP stub logging.
func NewAuthService(db *gorm.DB, jwtSecret string, logger *slog.Logger) AuthService {
	return &authService{
		db:        db,
		jwtSecret: []byte(jwtSecret),
		logger:    logger,
	}
}

// ─── Register ────────────────────────────────────────────────────────────────

// Register validates email uniqueness, hashes the password with bcrypt cost 12,
// persists the User record, generates a 6-digit OTP (10-minute expiry), and
// stubs the OTP email send. Per requirement 1.7, a failed email send does NOT
// prevent the HTTP 201 response — the caller should expose a resend mechanism.
func (s *authService) Register(ctx context.Context, req dto.RegisterRequest) error {
	// 1. Check email uniqueness (requirement 1.2).
	var existing models.User
	err := s.db.WithContext(ctx).
		Where("email = ?", req.Email).
		First(&existing).Error
	if err == nil {
		// Record found — email is taken.
		return ErrEmailTaken
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.logger.Error("register: db lookup failed", "error", err)
		return ErrDBUnavailable
	}

	// 2. Hash password with bcrypt cost 12 (requirement 1.1).
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		s.logger.Error("register: bcrypt failed", "error", err)
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 3. Generate 6-digit OTP with 10-minute expiry (requirement 1.6).
	otpCode := generateOTP()
	otpExpiry := time.Now().Add(10 * time.Minute)

	// 4. Persist the user record (requirement 1.1).
	user := models.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		FullName:     req.FullName,
		PasswordHash: string(hash),
		IsVerified:   false,
		OTPCode:      otpCode,
		OTPExpiresAt: otpExpiry,
		OTPFailCount: 0,
	}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		s.logger.Error("register: create user failed", "error", err)
		return ErrDBUnavailable
	}

	// 5. Stub OTP email send (requirement 1.6 / 1.7).
	// When a real email client is wired, replace this log with the actual send.
	s.logger.Info("register: OTP email stub",
		"email", req.Email,
		"otp", otpCode,
		"expires_at", otpExpiry,
	)

	return nil
}

// ─── Login ───────────────────────────────────────────────────────────────────

// Login looks up the user by email, compares the bcrypt hash, and issues an
// HS256 JWT with sub=userID, iat=now, exp=iat+86400. Both wrong-password and
// unknown-email return ErrInvalidCredentials to prevent user enumeration
// (requirements 2.2, 2.3).
func (s *authService) Login(ctx context.Context, req dto.LoginRequest) (dto.TokenResponse, error) {
	var user models.User
	err := s.db.WithContext(ctx).
		Where("email = ?", req.Email).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Unknown email — return same error as wrong password (requirement 2.3).
			return dto.TokenResponse{}, ErrInvalidCredentials
		}
		s.logger.Error("login: db lookup failed", "error", err)
		return dto.TokenResponse{}, ErrDBUnavailable
	}

	// Compare password hash (requirement 2.2).
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return dto.TokenResponse{}, ErrInvalidCredentials
	}

	// Issue JWT (requirements 2.1, 2.4).
	token, err := s.issueJWT(user.ID)
	if err != nil {
		return dto.TokenResponse{}, err
	}
	return dto.TokenResponse{Token: token}, nil
}

// ─── VerifyOTP ───────────────────────────────────────────────────────────────

// VerifyOTP validates the submitted OTP against the stored code, enforcing the
// 10-minute expiry window and the 5-attempt lockout. On success it marks the
// account as verified and returns a JWT (requirements 3.1–3.5).
func (s *authService) VerifyOTP(ctx context.Context, req dto.VerifyOTPRequest) (dto.TokenResponse, error) {
	var user models.User
	err := s.db.WithContext(ctx).
		Where("email = ?", req.Email).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.TokenResponse{}, ErrNotFound
		}
		s.logger.Error("verify-otp: db lookup failed", "error", err)
		return dto.TokenResponse{}, ErrDBUnavailable
	}

	// Already verified (requirement 3.5).
	if user.IsVerified {
		return dto.TokenResponse{}, ErrAlreadyVerified
	}

	// Locked due to too many failures (requirement 3.3).
	if user.OTPFailCount >= 5 {
		return dto.TokenResponse{}, ErrOTPLocked
	}

	// Expired (requirement 3.2).
	if time.Now().After(user.OTPExpiresAt) {
		return dto.TokenResponse{}, ErrOTPExpired
	}

	// Wrong code — increment fail count (requirement 3.3).
	if req.OTP != user.OTPCode {
		user.OTPFailCount++
		if dbErr := s.db.WithContext(ctx).
			Model(&user).
			Update("otp_fail_count", user.OTPFailCount).Error; dbErr != nil {
			s.logger.Error("verify-otp: update fail count failed", "error", dbErr)
		}
		// If this increment pushed us to 5, report locked immediately.
		if user.OTPFailCount >= 5 {
			return dto.TokenResponse{}, ErrOTPLocked
		}
		return dto.TokenResponse{}, ErrOTPInvalid
	}

	// Correct OTP — mark verified and clear OTP fields (requirement 3.1).
	updates := map[string]interface{}{
		"is_verified":    true,
		"otp_code":       "",
		"otp_expires_at": time.Time{},
		"otp_fail_count": 0,
	}
	if dbErr := s.db.WithContext(ctx).
		Model(&user).
		Updates(updates).Error; dbErr != nil {
		s.logger.Error("verify-otp: update user failed", "error", dbErr)
		return dto.TokenResponse{}, ErrDBUnavailable
	}

	// Issue JWT (requirement 3.1).
	token, err := s.issueJWT(user.ID)
	if err != nil {
		return dto.TokenResponse{}, err
	}
	return dto.TokenResponse{Token: token}, nil
}

// ─── ResendOTP ───────────────────────────────────────────────────────────────

// ResendOTP generates a fresh 6-digit OTP, resets the fail count and expiry,
// persists the changes, and stubs the email send (requirement 1.7).
func (s *authService) ResendOTP(ctx context.Context, email string) error {
	var user models.User
	err := s.db.WithContext(ctx).
		Where("email = ?", email).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		s.logger.Error("resend-otp: db lookup failed", "error", err)
		return ErrDBUnavailable
	}

	otpCode := generateOTP()
	otpExpiry := time.Now().Add(10 * time.Minute)

	updates := map[string]interface{}{
		"otp_code":       otpCode,
		"otp_expires_at": otpExpiry,
		"otp_fail_count": 0,
	}
	if dbErr := s.db.WithContext(ctx).
		Model(&user).
		Updates(updates).Error; dbErr != nil {
		s.logger.Error("resend-otp: update user failed", "error", dbErr)
		return ErrDBUnavailable
	}

	// Stub OTP email send.
	s.logger.Info("resend-otp: OTP email stub",
		"email", email,
		"otp", otpCode,
		"expires_at", otpExpiry,
	)

	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// generateOTP returns a zero-padded 6-digit numeric OTP string.
func generateOTP() string {
	// #nosec G404 — OTP does not need cryptographic randomness for this stub;
	// replace with crypto/rand for production hardening.
	n := rand.Intn(1_000_000) //nolint:gosec
	return fmt.Sprintf("%06d", n)
}

// issueJWT signs an HS256 JWT with sub=userID, iat=now, exp=iat+86400.
func (s *authService) issueJWT(userID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Unix() + 86400,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		s.logger.Error("issueJWT: signing failed", "error", err)
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}
	return signed, nil
}
