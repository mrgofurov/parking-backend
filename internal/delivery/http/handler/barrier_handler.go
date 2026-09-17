package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/service"
)

type BarrierHandler struct {
	barrierSvc service.BarrierService
}

func NewBarrierHandler(barrierSvc service.BarrierService) *BarrierHandler {
	return &BarrierHandler{barrierSvc: barrierSvc}
}

func (h *BarrierHandler) ListBarriers(c *fiber.Ctx) error {
	barriers, err := h.barrierSvc.ListBarriers()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, barriers)
}

type BarrierOpenRequest struct {
	Reason string `json:"reason"`
}

func (h *BarrierHandler) OpenBarrier(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	var req BarrierOpenRequest
	_ = c.BodyParser(&req)
	if req.Reason == "" {
		req.Reason = "Operator tomonidan majburiy ochildi"
	}

	if err := h.barrierSvc.OpenBarrier(id, req.Reason); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Shlagbaum ochildi")
}

func (h *BarrierHandler) CloseBarrier(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.barrierSvc.CloseBarrier(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Shlagbaum yopildi")
}

// Induction loop sensor or button triggering "Car Passed"
func (h *BarrierHandler) CarPassed(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.barrierSvc.CarPassed(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Avtomobil o'tishi qayd etildi, shlagbaum yopildi")
}

func (h *BarrierHandler) CreateBarrier(c *fiber.Ctx) error {
	var req service.CreateBarrierRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri so'rov formati")
	}

	barrier, err := h.barrierSvc.CreateBarrier(req)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Created(c, barrier, "Yangi shlagbaum muvaffaqiyatli qo'shildi")
}

func (h *BarrierHandler) UpdateBarrier(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	var req service.UpdateBarrierRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri so'rov formati")
	}

	barrier, err := h.barrierSvc.UpdateBarrier(id, req)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Success(c, barrier, "Shlagbaum sozlamalari yangilandi")
}

func (h *BarrierHandler) DeleteBarrier(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	if err := h.barrierSvc.DeleteBarrier(id); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, nil, "Shlagbaum o'chirildi")
}

func (h *BarrierHandler) PingBarrier(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	res, err := h.barrierSvc.PingBarrier(id)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, res, "Shlagbaum relay aloqasi tekshirildi")
}

