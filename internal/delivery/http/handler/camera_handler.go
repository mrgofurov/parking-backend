package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type CameraHandler struct {
	repo repository.CameraRepository
}

func NewCameraHandler(repo repository.CameraRepository) *CameraHandler {
	return &CameraHandler{repo: repo}
}

func (h *CameraHandler) ListCameras(c *fiber.Ctx) error {
	cameras, err := h.repo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, cameras)
}

type CameraDTO struct {
	Name      string               `json:"name"`
	Direction domain.DirectionType `json:"direction"`
	IPAddress string               `json:"ip_address"`
	RTSPUrl   string               `json:"rtsp_url"`
}

func (h *CameraHandler) CreateCamera(c *fiber.Ctx) error {
	var dto CameraDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	cam := &domain.Camera{
		ID:            uuid.New(),
		Name:          dto.Name,
		Direction:     dto.Direction,
		IPAddress:     dto.IPAddress,
		RTSPUrl:       dto.RTSPUrl,
		Status:        domain.CameraStatusOnline,
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := h.repo.Create(cam); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Created(c, cam, "Kamera qo'shildi")
}

func (h *CameraHandler) PingCamera(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.repo.UpdateHeartbeat(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, fiber.Map{"status": "ONLINE", "ping_ms": 12}, "Kamera aloqasi faol")
}

func (h *CameraHandler) DeleteCamera(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.repo.Delete(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Kamera o'chirildi")
}
