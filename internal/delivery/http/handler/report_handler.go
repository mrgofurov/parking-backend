package handler

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

type ReportHandler struct {
	reportSvc  service.ReportService
	branchRepo repository.BranchRepository
}

func NewReportHandler(reportSvc service.ReportService, branchRepo repository.BranchRepository) *ReportHandler {
	return &ReportHandler{reportSvc: reportSvc, branchRepo: branchRepo}
}

// parseBranchID - X-Branch-ID headeridan yoki ?branch_id= query dan UUID olish
func parseBranchIDFromCtx(c *fiber.Ctx) *uuid.UUID {
	rawID := c.Get("X-Branch-ID", c.Query("branch_id", ""))
	if rawID == "" {
		return nil
	}
	parsed, err := uuid.Parse(rawID)
	if err != nil {
		return nil
	}
	return &parsed
}

func (h *ReportHandler) GetDashboardStats(c *fiber.Ctx) error {
	branchID := parseBranchIDFromCtx(c)
	stats, err := h.reportSvc.GetDashboardStats(branchID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, stats)
}

func (h *ReportHandler) GetRevenueTrends(c *fiber.Ctx) error {
	days, _ := strconv.Atoi(c.Query("days", "7"))
	branchID := parseBranchIDFromCtx(c)
	trends, err := h.reportSvc.GetRevenueTrends(branchID, days)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, trends)
}

// =============================================
//  BRANCH SETTINGS (backward compat)
// =============================================

func (h *ReportHandler) GetBranchSettings(c *fiber.Ctx) error {
	branch, err := h.branchRepo.GetFirst()
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Filial topilmadi")
	}
	return response.Success(c, branch)
}

type UpdateBranchDTO struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	TotalSpaces int    `json:"total_spaces"`
}

func (h *ReportHandler) UpdateBranchSettings(c *fiber.Ctx) error {
	branch, err := h.branchRepo.GetFirst()
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Filial topilmadi")
	}

	var dto UpdateBranchDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri ma'lumotlar")
	}

	if dto.Name != "" {
		branch.Name = dto.Name
	}
	if dto.Address != "" {
		branch.Address = dto.Address
	}
	if dto.TotalSpaces > 0 {
		branch.TotalSpaces = dto.TotalSpaces
	}

	if err := h.branchRepo.Update(branch); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, branch, "Filial sozlamalari yangilandi")
}

// =============================================
//  BRANCHES CRUD (Multi-Branch)
// =============================================

func (h *ReportHandler) ListBranches(c *fiber.Ctx) error {
	branches, err := h.branchRepo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, branches)
}

func (h *ReportHandler) GetBranch(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz filial ID")
	}
	branch, err := h.branchRepo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Filial topilmadi")
	}
	return response.Success(c, branch)
}

type CreateBranchDTO struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	TotalSpaces int    `json:"total_spaces"`
}

func (h *ReportHandler) CreateBranch(c *fiber.Ctx) error {
	var dto CreateBranchDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri ma'lumotlar")
	}

	if dto.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "Filial nomi kiritilishi shart")
	}

	spaces := dto.TotalSpaces
	if spaces <= 0 {
		spaces = 100
	}

	branch := &domain.Branch{
		ID:          uuid.New(),
		Name:        dto.Name,
		Address:     dto.Address,
		TotalSpaces: spaces,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.branchRepo.Create(branch); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Created(c, branch, "Yangi filial yaratildi")
}

func (h *ReportHandler) UpdateBranch(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz filial ID")
	}

	branch, err := h.branchRepo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Filial topilmadi")
	}

	var dto UpdateBranchDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri ma'lumotlar")
	}

	if dto.Name != "" {
		branch.Name = dto.Name
	}
	if dto.Address != "" {
		branch.Address = dto.Address
	}
	if dto.TotalSpaces > 0 {
		branch.TotalSpaces = dto.TotalSpaces
	}
	branch.UpdatedAt = time.Now()

	if err := h.branchRepo.Update(branch); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, branch, "Filial yangilandi")
}

func (h *ReportHandler) DeleteBranch(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz filial ID")
	}

	// Birinchi filial o'chirilmasin (default)
	branches, _ := h.branchRepo.List()
	if len(branches) <= 1 {
		return response.Error(c, fiber.StatusBadRequest, "Kamida bitta filial qolishi shart")
	}

	if err := h.branchRepo.Delete(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Filial o'chirildi")
}
