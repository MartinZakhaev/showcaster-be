package dto

// RegisterRequest holds the payload for the POST /api/auth/register endpoint.
type RegisterRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	FullName string `json:"fullName" validate:"required,max=100"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// LoginRequest holds the payload for the POST /api/auth/login endpoint.
type LoginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// VerifyOTPRequest holds the payload for the POST /api/auth/verify-otp endpoint.
type VerifyOTPRequest struct {
	Email string `json:"email" validate:"required,email"`
	OTP   string `json:"otp"   validate:"required,len=6,numeric"`
}

// ResendOTPRequest holds the payload for the POST /api/auth/resend-otp endpoint.
type ResendOTPRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// TokenResponse is returned on successful login or OTP verification.
type TokenResponse struct {
	Token string `json:"token"`
}
