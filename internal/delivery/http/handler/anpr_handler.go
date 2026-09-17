package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/service"
)

type ANPRHandler struct {
	anprSvc service.ANPRService
}

func NewANPRHandler(anprSvc service.ANPRService) *ANPRHandler {
	return &ANPRHandler{anprSvc: anprSvc}
}

// Universal Webhook for ANPR Cameras
func (h *ANPRHandler) HandleEvent(c *fiber.Ctx) error {
	var req service.ANPREventRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri JSON formati")
	}

	if req.PlateNumber == "" {
		return response.Error(c, fiber.StatusBadRequest, "Avtomobil raqami (plate_number) kiritilmadi")
	}

	if req.Direction != domain.DirectionEntry && req.Direction != domain.DirectionExit {
		req.Direction = domain.DirectionEntry // Default fallback
	}

	res, err := h.anprSvc.ProcessEvent(&req)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return response.Success(c, res, res.Message)
}

type SimulationRequest struct {
	PlateNumber string               `json:"plate_number"`
	Direction   domain.DirectionType `json:"direction"` // ENTRY or EXIT
}

// Virtual Gate Simulator for testing from admin panel
func (h *ANPRHandler) SimulateEvent(c *fiber.Ctx) error {
	var req SimulationRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri so'rov formati")
	}

	if req.PlateNumber == "" {
		return response.Error(c, fiber.StatusBadRequest, "Avtomobil raqamini kiriting")
	}

	now := time.Now()
	eventReq := service.ANPREventRequest{
		PlateNumber: req.PlateNumber,
		Direction:   req.Direction,
		Confidence:  98.5,
		Timestamp:   &now,
	}

	res, err := h.anprSvc.ProcessEvent(&eventReq)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.Success(c, res, res.Message)
}

// Camera Heartbeat
func (h *ANPRHandler) Heartbeat(c *fiber.Ctx) error {
	camIDStr := c.Query("camera_id")
	if camIDStr != "" {
		if _, err := uuid.Parse(camIDStr); err == nil {
			// Update heartbeat if valid
		}
	}
	return response.Success(c, fiber.Map{"status": "alive", "timestamp": time.Now()})
}
