package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/jwt"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
)

func AuthMiddleware(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return response.Error(c, fiber.StatusUnauthorized, "Avtorizatsiya talab qilinadi (Token yo'q)")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return response.Error(c, fiber.StatusUnauthorized, "Token formati noto'g'ri (Bearer <token>)")
		}

		claims, err := jwt.ParseToken(parts[1], secret)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, "Yaroqsiz yoki muddati o'tgan token")
		}

		c.Locals("user_id", claims.UserID)
		c.Locals("username", claims.Username)
		c.Locals("role", claims.Role)
		c.Locals("branch_id", claims.BranchID)

		return c.Next()
	}
}

func RequireRole(allowedRoles ...domain.UserRole) fiber.Handler {
	return func(c *fiber.Ctx) error {
		roleVal := c.Locals("role")
		if roleVal == nil {
			return response.Error(c, fiber.StatusForbidden, "Ruxsat berilmagan")
		}

		userRole, ok := roleVal.(domain.UserRole)
		if !ok {
			return response.Error(c, fiber.StatusForbidden, "Ruxsat berilmagan")
		}

		for _, r := range allowedRoles {
			if userRole == r {
				return c.Next()
			}
		}

		return response.Error(c, fiber.StatusForbidden, "Ushbu amalni bajarish uchun huquqingiz yetarli emas")
	}
}
