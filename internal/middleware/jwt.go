package middleware

import (
	"github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// JWTMiddleware returns a Fiber handler that validates Bearer JWTs.
// Requests with a missing, malformed, expired, or incorrectly-signed token
// receive HTTP 401 {"error": "unauthorized"}.
func JWTMiddleware(secret string) fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: []byte(secret)},
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		},
	})
}

// ExtractUserID reads the validated JWT from the Fiber context (stored under
// the "user" local by gofiber/contrib/jwt) and returns the "sub" claim value.
func ExtractUserID(c *fiber.Ctx) string {
	token, ok := c.Locals("user").(*jwtv5.Token)
	if !ok || token == nil {
		return ""
	}
	claims, ok := token.Claims.(jwtv5.MapClaims)
	if !ok {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}
