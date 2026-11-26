package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

// DisplayHandler serves the customer-facing lane monitor (public: the screen
// at the gate has no operator login) and QR payment management (protected).
type DisplayHandler struct {
	anprSvc  service.ANPRService
	display  *service.DisplayService
	qrSvc    service.QRPaymentService
	camRepo  repository.CameraRepository
	branches repository.BranchRepository
}

func NewDisplayHandler(anprSvc service.ANPRService, display *service.DisplayService, qrSvc service.QRPaymentService,
	camRepo repository.CameraRepository, branches repository.BranchRepository) *DisplayHandler {
	return &DisplayHandler{anprSvc: anprSvc, display: display, qrSvc: qrSvc, camRepo: camRepo, branches: branches}
}

type displayResponse struct {
	service.DisplayState
	BranchName string `json:"branch_name"`
	QRMode     string `json:"qr_mode"`
}

// GetLaneState: GET /api/v1/display/:camera (camera UUID or code).
func (h *DisplayHandler) GetLaneState(c *fiber.Ctx) error {
	cam, err := h.anprSvc.ResolveCamera(c.Params("camera"))
	if err != nil || cam == nil {
		return response.Error(c, fiber.StatusNotFound, "Kamera topilmadi")
	}
	out := displayResponse{DisplayState: h.display.Get(cam), QRMode: h.qrSvc.Mode()}
	if b, err := h.branches.GetByID(cam.BranchID); err == nil {
		out.BranchName = b.Name
	}
	return response.Success(c, out)
}

// ListLanes: GET /api/v1/display — cameras a display can be bound to.
func (h *DisplayHandler) ListLanes(c *fiber.Ctx) error {
	cams, err := h.camRepo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	type lane struct {
		ID        uuid.UUID            `json:"id"`
		Code      string               `json:"code"`
		Name      string               `json:"name"`
		Direction domain.DirectionType `json:"direction"`
		BranchID  uuid.UUID            `json:"branch_id"`
	}
	out := make([]lane, 0, len(cams))
	for _, cam := range cams {
		out = append(out, lane{cam.ID, cam.Code, cam.Name, cam.Direction, cam.BranchID})
	}
	return response.Success(c, out)
}

// ---- QR payment requests (protected) ----

func (h *DisplayHandler) ListPaymentRequests(c *fiber.Ctx) error {
	list, err := h.qrSvc.List(c.Query("status"), c.QueryInt("limit", 100))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, fiber.Map{"mode": h.qrSvc.Mode(), "items": list})
}

func (h *DisplayHandler) CheckPaymentRequest(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}
	pr, err := h.qrSvc.Check(id)
	if err != nil && pr == nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, pr, "Tekshirildi")
}

func (h *DisplayHandler) MockPayPaymentRequest(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}
	pr, err := h.qrSvc.MockPay(id)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, pr, "Sinov to'lovi qabul qilindi")
}

func (h *DisplayHandler) CancelPaymentRequest(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}
	pr, err := h.qrSvc.Cancel(id)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, pr, "QR bekor qilindi")
}
