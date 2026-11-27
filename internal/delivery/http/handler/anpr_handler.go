package handler

import (
	"crypto/subtle"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/hikvision"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

type ANPRHandler struct {
	cfg     *config.Config
	anprSvc service.ANPRService
	camRepo repository.CameraRepository
}

func NewANPRHandler(cfg *config.Config, anprSvc service.ANPRService, camRepo repository.CameraRepository) *ANPRHandler {
	return &ANPRHandler{cfg: cfg, anprSvc: anprSvc, camRepo: camRepo}
}

// tokenOK checks the optional shared webhook token (?token=, X-ANPR-Token or
// Authorization: Bearer).
func (h *ANPRHandler) tokenOK(c *fiber.Ctx) bool {
	want := h.cfg.ANPRWebhookToken
	if want == "" {
		return true
	}
	got := c.Query("token")
	if got == "" {
		got = c.Get("X-ANPR-Token")
	}
	if got == "" {
		if a := c.Get("Authorization"); strings.HasPrefix(strings.ToLower(a), "bearer ") {
			got = strings.TrimSpace(a[7:])
		}
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// HandleEvent: JSON webhook used by parking-capture-service (and any generic
// ANPR source). camera_id may be the camera UUID or its code.
func (h *ANPRHandler) HandleEvent(c *fiber.Ctx) error {
	if !h.tokenOK(c) {
		return response.Error(c, fiber.StatusUnauthorized, "ANPR token noto'g'ri")
	}
	var req service.ANPREventRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri JSON formati: "+err.Error())
	}
	if req.PlateNumber == "" {
		return response.Error(c, fiber.StatusBadRequest, "Avtomobil raqami (plate_number) kiritilmadi")
	}
	if req.Source == "" {
		req.Source = "capture-service"
	}

	res, err := h.anprSvc.ProcessEvent(&req)
	if err != nil {
		return response.Error(c, fiber.StatusUnprocessableEntity, err.Error())
	}
	return response.Success(c, res, res.Message)
}

// HikvisionEvent receives HTTP Listening (alarm server) pushes from Hikvision
// ANPR cameras. Configure on the camera:
//
//	Configuration → Network → Advanced → HTTP Listening (or Alarm Server)
//	  Destination IP: <backend host>, Port: 8085
//	  URL: /api/v1/anpr/hikvision/<camera code or id>[?token=…]
//	and enable "Notify Surveillance Center" for the ANPR / vehicle detection event.
//
// The camera is identified by the URL path / ?camera=, falling back to the
// device IP inside the event or the connection's source IP. Always answers
// 200 so the camera does not keep retrying the same event.
func (h *ANPRHandler) HikvisionEvent(c *fiber.Ctx) error {
	if !h.tokenOK(c) {
		return c.Status(fiber.StatusUnauthorized).SendString("token")
	}

	evt, err := hikvision.ParseEvent(c.Get("Content-Type"), c.Body())
	if err != nil {
		log.Printf("[Hikvision] Parse xatosi (%s, %d bayt) %s: %v\n", c.Get("Content-Type"), len(c.Body()), c.IP(), err)
		return c.Status(fiber.StatusOK).SendString("ignored")
	}

	cam := h.findCamera(c, evt)
	if cam == nil {
		log.Printf("[Hikvision] Kamera aniqlanmadi: ip=%s src=%s plate=%q. Kamerani admin panelda qo'shing (IP yoki code).\n",
			evt.IPAddress, c.IP(), evt.Plate)
		return c.Status(fiber.StatusOK).SendString("unknown camera")
	}
	_ = h.camRepo.UpdateHeartbeat(cam.ID)

	if !evt.HasPlate() {
		// heartbeat / videoloss keep-alive / vehicle without readable plate
		return c.Status(fiber.StatusOK).SendString("ok")
	}

	snapshot := evt.SceneImage
	if snapshot == nil {
		snapshot = evt.PlateImage
	}
	req := &service.ANPREventRequest{
		PlateNumber:   evt.Plate,
		Confidence:    evt.Confidence,
		Camera:        cam,
		SnapshotBytes: snapshot,
		Source:        "hikvision",
	}
	res, err := h.anprSvc.ProcessEvent(req)
	if err != nil {
		log.Printf("[Hikvision] %s (%s) qayta ishlanmadi: %v\n", evt.Plate, cam.Name, err)
		return c.Status(fiber.StatusOK).SendString("error")
	}
	log.Printf("[Hikvision] %s → %s: %s\n", evt.Plate, cam.Name, res.Message)
	return c.Status(fiber.StatusOK).SendString("ok")
}

func (h *ANPRHandler) findCamera(c *fiber.Ctx, evt *hikvision.ANPREvent) *domain.Camera {
	for _, key := range []string{c.Params("camera"), c.Query("camera"), c.Query("camera_id")} {
		if key != "" {
			if cam, err := h.anprSvc.ResolveCamera(key); err == nil && cam != nil {
				return cam
			}
		}
	}
	for _, ip := range []string{evt.IPAddress, c.IP()} {
		if ip != "" {
			if cam, err := h.camRepo.GetByIP(ip); err == nil && cam != nil {
				return cam
			}
		}
	}
	return nil
}

type SimulationRequest struct {
	PlateNumber string               `json:"plate_number"`
	Direction   domain.DirectionType `json:"direction"` // ENTRY or EXIT
	CameraID    string               `json:"camera_id"`
}

// SimulateEvent: virtual gate for testing from the admin panel (authenticated).
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
		CameraID:    req.CameraID,
		Confidence:  98.5,
		Timestamp:   &now,
		BranchID:    parseBranchIDFromCtx(c),
		Source:      "simulator",
	}

	res, err := h.anprSvc.ProcessEvent(&eventReq)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, res, res.Message)
}

// Heartbeat lets edge devices report liveness: ?camera_id=<uuid|code>.
func (h *ANPRHandler) Heartbeat(c *fiber.Ctx) error {
	if key := c.Query("camera_id"); key != "" {
		if cam, err := h.anprSvc.ResolveCamera(key); err == nil && cam != nil {
			_ = h.camRepo.UpdateHeartbeat(cam.ID)
		}
	}
	return response.Success(c, fiber.Map{"status": "alive", "timestamp": time.Now()})
}

// ListCaptureCameras returns cameras that parking-capture-service must read
// (source CAPTURE_SERVICE) with their RTSP URLs. The URLs carry the camera
// login, so set ANPR_WEBHOOK_TOKEN wherever this endpoint is reachable from
// outside the parking network.
func (h *ANPRHandler) ListCaptureCameras(c *fiber.Ctx) error {
	if !h.tokenOK(c) {
		return response.Error(c, fiber.StatusUnauthorized, "ANPR token noto'g'ri")
	}
	cams, err := h.camRepo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	type capCam struct {
		ID        uuid.UUID            `json:"id"`
		Code      string               `json:"code"`
		Name      string               `json:"name"`
		Direction domain.DirectionType `json:"direction"`
		RTSPUrl   string               `json:"rtsp_url"`
		BranchID  uuid.UUID            `json:"branch_id"`
	}
	out := []capCam{}
	for _, cam := range cams {
		if cam.Source == domain.CameraSourceCapture {
			out = append(out, capCam{cam.ID, cam.Code, cam.Name, cam.Direction, cam.EffectiveRTSPURL(), cam.BranchID})
		}
	}
	return response.Success(c, out)
}
