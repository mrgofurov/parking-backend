package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

type SessionHandler struct {
	sessionSvc service.SessionService
}

func NewSessionHandler(sessionSvc service.SessionService) *SessionHandler {
	return &SessionHandler{sessionSvc: sessionSvc}
}

func (h *SessionHandler) ListSessions(c *fiber.Ctx) error {
	plate := c.Query("plate")
	status := domain.SessionStatus(c.Query("status"))
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	filter := repository.SessionFilter{
		PlateNumber: plate,
		Status:      status,
		Page:        page,
		Limit:       limit,
	}

	sessions, total, err := h.sessionSvc.ListSessions(filter)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, fiber.Map{
		"items": sessions,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *SessionHandler) GetSession(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	session, err := h.sessionSvc.GetSessionByID(id)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Sessiya topilmadi")
	}

	return response.Success(c, session)
}

type ManualCloseRequest struct {
	Reason string `json:"reason"`
}

func (h *SessionHandler) ManualClose(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	var req ManualCloseRequest
	_ = c.BodyParser(&req)
	if req.Reason == "" {
		req.Reason = "Operator tomonidan yopildi"
	}

	if err := h.sessionSvc.ManualClose(id, req.Reason); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Success(c, nil, "Sessiya muvaffaqiyatli yopildi")
}

func (h *SessionHandler) Recalculate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}

	session, err := h.sessionSvc.RecalculateFee(id)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Success(c, session)
}

func (h *SessionHandler) VehicleHistory(c *fiber.Ctx) error {
	plate := c.Query("plate")
	if plate == "" {
		return response.Error(c, fiber.StatusBadRequest, "Avtomobil raqami talab qilinadi")
	}

	history, err := h.sessionSvc.GetVehicleHistory(plate)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, history)
}
