package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
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

func (h *ReportHandler) GetDashboardStats(c *fiber.Ctx) error {
	stats, err := h.reportSvc.GetDashboardStats()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, stats)
}

func (h *ReportHandler) GetRevenueTrends(c *fiber.Ctx) error {
	days, _ := strconv.Atoi(c.Query("days", "7"))
	trends, err := h.reportSvc.GetRevenueTrends(days)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, trends)
}

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
