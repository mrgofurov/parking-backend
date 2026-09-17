package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type ClientHandler struct {
	repo repository.ClientRepository
}

func NewClientHandler(repo repository.ClientRepository) *ClientHandler {
	return &ClientHandler{repo: repo}
}

func (h *ClientHandler) ListClients(c *fiber.Ctx) error {
	clients, err := h.repo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, clients)
}

func (h *ClientHandler) GetClient(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	client, err := h.repo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Mijoz topilmadi")
	}

	return response.Success(c, client)
}

type ClientDTO struct {
	FullName      string              `json:"full_name"`
	Phone         string              `json:"phone"`
	PlateNumber   string              `json:"plate_number"`
	DiscountType  domain.DiscountType `json:"discount_type"`
	DiscountValue float64             `json:"discount_value"`
	Notes         string              `json:"notes"`
	Active        bool                `json:"active"`
}

func (h *ClientHandler) CreateClient(c *fiber.Ctx) error {
	var dto ClientDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if dto.FullName == "" || dto.PlateNumber == "" {
		return response.Error(c, fiber.StatusBadRequest, "Mijoz ismi va davlat raqami kiritilishi shart")
	}

	cleanPlate := strings.ToUpper(strings.ReplaceAll(dto.PlateNumber, " ", ""))

	// Check existing plate
	existing, _ := h.repo.GetByPlate(cleanPlate)
	if existing != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ushbu avtomobil raqami allaqachon ro'yxatdan o'tgan")
	}

	client := &domain.Client{
		ID:            uuid.New(),
		FullName:      dto.FullName,
		Phone:         dto.Phone,
		PlateNumber:   cleanPlate,
		DiscountType:  dto.DiscountType,
		DiscountValue: dto.DiscountValue,
		Notes:         dto.Notes,
		Active:        true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := h.repo.Create(client); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Created(c, client, "Mijoz muvaffaqiyatli qo'shildi")
}

func (h *ClientHandler) UpdateClient(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	client, err := h.repo.GetByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Mijoz topilmadi")
	}

	var dto ClientDTO
	if err := c.BodyParser(&dto); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Ma'lumotlar formati noto'g'ri")
	}

	if dto.FullName != "" {
		client.FullName = dto.FullName
	}
	if dto.Phone != "" {
		client.Phone = dto.Phone
	}
	if dto.PlateNumber != "" {
		client.PlateNumber = strings.ToUpper(strings.ReplaceAll(dto.PlateNumber, " ", ""))
	}
	if dto.DiscountType != "" {
		client.DiscountType = dto.DiscountType
	}
	client.DiscountValue = dto.DiscountValue
	client.Notes = dto.Notes
	client.Active = dto.Active
	client.UpdatedAt = time.Now()

	if err := h.repo.Update(client); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, client, "Mijoz ma'lumotlari yangilandi")
}

func (h *ClientHandler) DeleteClient(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.repo.Delete(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Mijoz o'chirildi")
}
