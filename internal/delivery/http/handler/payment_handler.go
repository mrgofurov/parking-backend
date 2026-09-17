package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/service"
)

type PaymentHandler struct {
	paymentSvc service.PaymentService
}

func NewPaymentHandler(paymentSvc service.PaymentService) *PaymentHandler {
	return &PaymentHandler{paymentSvc: paymentSvc}
}

func (h *PaymentHandler) ProcessPayment(c *fiber.Ctx) error {
	var req service.ProcessPaymentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "To'lov ma'lumotlari noto'g'ri")
	}

	// Attach operator info from auth context if available
	if opID, ok := c.Locals("user_id").(uuid.UUID); ok {
		req.OperatorID = &opID
	}
	if opName, ok := c.Locals("username").(string); ok {
		req.OperatorName = opName
	}

	payment, err := h.paymentSvc.ProcessPayment(&req)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Success(c, payment, "To'lov muvaffaqiyatli qabul qilindi. Chiqish shlagbaumi ochildi.")
}

func (h *PaymentHandler) GetReceipt(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	receipt, err := h.paymentSvc.GetReceipt(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, err.Error())
	}

	return response.Success(c, receipt)
}

func (h *PaymentHandler) ListPayments(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	payments, total, err := h.paymentSvc.ListPayments(page, limit)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, fiber.Map{
		"items": payments,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
