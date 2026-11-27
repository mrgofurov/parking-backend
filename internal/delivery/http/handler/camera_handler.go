package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

type CameraHandler struct {
	repo       repository.CameraRepository
	branchRepo repository.BranchRepository
}

func NewCameraHandler(repo repository.CameraRepository, branchRepo repository.BranchRepository) *CameraHandler {
	return &CameraHandler{repo: repo, branchRepo: branchRepo}
}

func (h *CameraHandler) ListCameras(c *fiber.Ctx) error {
	branchID := parseBranchIDFromCtx(c)
	if branchID != nil {
		cameras, err := h.repo.ListByBranch(*branchID)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, err.Error())
		}
		return response.Success(c, cameras)
	}

	cameras, err := h.repo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, cameras)
}

type CameraDTO struct {
	Name          string               `json:"name"`
	Direction     domain.DirectionType `json:"direction"`
	Code          string               `json:"code"`
	Source        domain.CameraSource  `json:"source"`
	BarrierID     *uuid.UUID           `json:"barrier_id"`
	IPAddress     string               `json:"ip_address"`
	RTSPUrl       string               `json:"rtsp_url"`
	MinConfidence float64              `json:"min_confidence"`
	DisplayIP     string               `json:"display_ip"`
	DisplayRows   int                  `json:"display_rows"`
	BranchID      *uuid.UUID           `json:"branch_id"`
}

func normalizeSource(s domain.CameraSource) domain.CameraSource {
	if s == domain.CameraSourceCapture {
		return s
	}
	return domain.CameraSourceHikvision
}

func (h *CameraHandler) codeTaken(code string, self uuid.UUID) bool {
	if code == "" {
		return false
	}
	existing, err := h.repo.GetByCode(code)
	return err == nil && existing != nil && existing.ID != self
}

func (h *CameraHandler) CreateCamera(c *fiber.Ctx) error {
	var dto CameraDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	// BranchID ni header yoki body dan olish
	branchID := parseBranchIDFromCtx(c)
	if branchID == nil && dto.BranchID != nil {
		branchID = dto.BranchID
	}

	// Agar branchID yo'q bo'lsa, birinchi filialdan olish
	var finalBranchID uuid.UUID
	if branchID != nil {
		finalBranchID = *branchID
	} else {
		branch, err := h.branchRepo.GetFirst()
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "Filial topilmadi")
		}
		finalBranchID = branch.ID
	}

	if dto.Direction != domain.DirectionEntry && dto.Direction != domain.DirectionExit {
		return response.Error(c, fiber.StatusBadRequest, "Yo'nalish ENTRY yoki EXIT bo'lishi kerak")
	}
	newID := uuid.New()
	if h.codeTaken(dto.Code, newID) {
		return response.Error(c, fiber.StatusConflict, "Bu kod boshqa kameraga berilgan: "+dto.Code)
	}

	cam := &domain.Camera{
		ID:            newID,
		Name:          dto.Name,
		BranchID:      finalBranchID,
		Direction:     dto.Direction,
		Code:          dto.Code,
		Source:        normalizeSource(dto.Source),
		BarrierID:     dto.BarrierID,
		IPAddress:     dto.IPAddress,
		RTSPUrl:       dto.RTSPUrl,
		MinConfidence: dto.MinConfidence,
		DisplayIP:     strings.TrimSpace(dto.DisplayIP),
		DisplayRows:   service.NormalizeLEDRows(dto.DisplayRows),
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

func (h *CameraHandler) UpdateCamera(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	cam, err := h.repo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Kamera topilmadi")
	}

	var dto CameraDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if dto.Name != "" {
		cam.Name = dto.Name
	}
	if dto.Direction == domain.DirectionEntry || dto.Direction == domain.DirectionExit {
		cam.Direction = dto.Direction
	}
	if h.codeTaken(dto.Code, cam.ID) {
		return response.Error(c, fiber.StatusConflict, "Bu kod boshqa kameraga berilgan: "+dto.Code)
	}
	cam.Code = dto.Code
	if dto.Source != "" {
		cam.Source = normalizeSource(dto.Source)
	}
	cam.BarrierID = dto.BarrierID
	if cam.BarrierID != nil && *cam.BarrierID == uuid.Nil {
		cam.BarrierID = nil
	}
	cam.IPAddress = dto.IPAddress
	cam.RTSPUrl = dto.RTSPUrl
	cam.MinConfidence = dto.MinConfidence
	cam.DisplayIP = strings.TrimSpace(dto.DisplayIP)
	cam.DisplayRows = service.NormalizeLEDRows(dto.DisplayRows)
	if dto.BranchID != nil {
		cam.BranchID = *dto.BranchID
	}
	cam.UpdatedAt = time.Now()

	if err := h.repo.Update(cam); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, cam, "Kamera yangilandi")
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
