package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/service"
)

type TariffHandler struct {
	tariffSvc service.TariffService
}

func NewTariffHandler(tariffSvc service.TariffService) *TariffHandler {
	return &TariffHandler{tariffSvc: tariffSvc}
}

func (h *TariffHandler) ListTariffs(c *fiber.Ctx) error {
	tariffs, err := h.tariffSvc.ListTariffs()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, tariffs)
}

func (h *TariffHandler) GetTariff(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	tariff, err := h.tariffSvc.GetTariffByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Tarif topilmadi")
	}

	return response.Success(c, tariff)
}

type TariffDTO struct {
	Name          string  `json:"name"`
	FreeMinutes   int     `json:"free_minutes"`
	HourlyPrice   float64 `json:"hourly_price"`
	DailyMaxPrice float64 `json:"daily_max_price"`
	IsDefault     bool    `json:"is_default"`
}

func (h *TariffHandler) CreateTariff(c *fiber.Ctx) error {
	var dto TariffDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if dto.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "Tarif nomi kiritilishi shart")
	}

	tariff := &domain.Tariff{
		ID:            uuid.New(),
		Name:          dto.Name,
		FreeMinutes:   dto.FreeMinutes,
		HourlyPrice:   dto.HourlyPrice,
		DailyMaxPrice: dto.DailyMaxPrice,
		IsDefault:     dto.IsDefault,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := h.tariffSvc.CreateTariff(tariff); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Created(c, tariff, "Yangi tarif muvaffaqiyatli saqlandi")
}

func (h *TariffHandler) UpdateTariff(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	tariff, err := h.tariffSvc.GetTariffByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Tarif topilmadi")
	}

	var dto TariffDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if dto.Name != "" {
		tariff.Name = dto.Name
	}
	tariff.FreeMinutes = dto.FreeMinutes
	tariff.HourlyPrice = dto.HourlyPrice
	tariff.DailyMaxPrice = dto.DailyMaxPrice
	tariff.IsDefault = dto.IsDefault
	tariff.UpdatedAt = time.Now()

	if err := h.tariffSvc.UpdateTariff(tariff); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, tariff, "Tarif yangilandi")
}

func (h *TariffHandler) DeleteTariff(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.tariffSvc.DeleteTariff(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Tarif o'chirildi")
}
