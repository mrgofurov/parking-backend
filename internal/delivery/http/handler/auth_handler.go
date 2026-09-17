package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/jwt"
	"github.com/smart-parking/parking-backend/internal/pkg/password"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type AuthHandler struct {
	repo repository.UserRepository
	cfg  *config.Config
}

func NewAuthHandler(repo repository.UserRepository, cfg *config.Config) *AuthHandler {
	return &AuthHandler{repo: repo, cfg: cfg}
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	user, err := h.repo.GetByUsername(req.Username)
	if err != nil || !user.Active {
		return response.Error(c, fiber.StatusUnauthorized, "Login yoki parol noto'g'ri")
	}

	if !password.CheckPassword(req.Password, user.Password) {
		return response.Error(c, fiber.StatusUnauthorized, "Login yoki parol noto'g'ri")
	}

	token, err := jwt.GenerateToken(user, h.cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "Token yaratishda xatolik yuz berdi")
	}

	return response.Success(c, fiber.Map{
		"token": token,
		"user": fiber.Map{
			"id":        user.ID,
			"username":  user.Username,
			"full_name": user.FullName,
			"role":      user.Role,
			"branch_id": user.BranchID,
			"active":    user.Active,
		},
	}, "Muvaffaqiyatli tizimga kirildi")
}

func (h *AuthHandler) GetMe(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return response.Error(c, fiber.StatusUnauthorized, "Avtorizatsiya talab qilinadi")
	}

	user, err := h.repo.GetByID(userID)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Foydalanuvchi topilmadi")
	}

	return response.Success(c, fiber.Map{
		"id":        user.ID,
		"username":  user.Username,
		"full_name": user.FullName,
		"role":      user.Role,
		"branch_id": user.BranchID,
		"active":    user.Active,
	})
}

func (h *AuthHandler) ListUsers(c *fiber.Ctx) error {
	users, err := h.repo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, users)
}

type CreateUserRequest struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	FullName string          `json:"full_name"`
	Role     domain.UserRole `json:"role"`
	BranchID *uuid.UUID      `json:"branch_id"`
}

func (h *AuthHandler) CreateUser(c *fiber.Ctx) error {
	var req CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if req.Username == "" || req.Password == "" {
		return response.Error(c, fiber.StatusBadRequest, "Username va parol kiritilishi shart")
	}

	hashed, err := password.HashPassword(req.Password)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "Parolni shifrlashda xatolik")
	}

	user := &domain.User{
		ID:        uuid.New(),
		Username:  req.Username,
		Password:  hashed,
		FullName:  req.FullName,
		Role:      req.Role,
		BranchID:  req.BranchID,
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.repo.Create(user); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Foydalanuvchi yaratishda xatolik (username band bo'lishi mumkin)")
	}

	return response.Created(c, user, "Foydalanuvchi yaratildi")
}

type UpdateUserRequest struct {
	FullName string          `json:"full_name"`
	Role     domain.UserRole `json:"role"`
	BranchID *uuid.UUID      `json:"branch_id"`
	Active   *bool           `json:"active"`
	Password string          `json:"password"`
}

func (h *AuthHandler) UpdateUser(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	user, err := h.repo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Foydalanuvchi topilmadi")
	}

	var req UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	// branch_id explicitly set (even to nil to unassign)
	user.BranchID = req.BranchID

	if req.Active != nil {
		user.Active = *req.Active
	}
	if req.Password != "" {
		hashed, err := password.HashPassword(req.Password)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "Parolni shifrlashda xatolik")
		}
		user.Password = hashed
	}
	user.UpdatedAt = time.Now()

	if err := h.repo.Update(user); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, user, "Foydalanuvchi ma'lumotlari yangilandi")
}

func (h *AuthHandler) DeleteUser(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.repo.Delete(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Foydalanuvchi o'chirildi")
}
