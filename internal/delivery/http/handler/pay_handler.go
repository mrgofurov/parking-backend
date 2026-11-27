package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/click"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

// PayHandler serves the phone payment page behind the static QR sticker at
// each exit lane, and Click's SHOP API callbacks. All routes are public:
// drivers have no login, and Click authenticates with sign_string.
type PayHandler struct {
	cfg      *config.Config
	anprSvc  service.ANPRService
	display  *service.DisplayService
	clickSvc service.ClickService
	requests repository.PaymentRequestRepository
	branches repository.BranchRepository
}

func NewPayHandler(cfg *config.Config, anprSvc service.ANPRService, display *service.DisplayService,
	clickSvc service.ClickService, requests repository.PaymentRequestRepository, branches repository.BranchRepository) *PayHandler {
	return &PayHandler{cfg: cfg, anprSvc: anprSvc, display: display, clickSvc: clickSvc, requests: requests, branches: branches}
}

// Pay page states (besides the lane display states it mirrors).
const (
	PayStatePayable = "PAYABLE" // amount locked, Click link ready
	PayStateCashier = "CASHIER" // payment needed but online payment unavailable
)

type payInfo struct {
	CameraCode  string     `json:"camera_code"`
	LaneName    string     `json:"lane_name"`
	BranchName  string     `json:"branch_name"`
	State       string     `json:"state"`      // PAYABLE | CASHIER | lane display state (IDLE, PAID, FREE, ...)
	ClickMode   string     `json:"click_mode"` // live | mock | off
	PlateNumber string     `json:"plate_number,omitempty"`
	EntryTime   *time.Time `json:"entry_time,omitempty"`
	DurationMin int        `json:"duration_min,omitempty"`
	Amount      float64    `json:"amount,omitempty"`
	BillNumber  string     `json:"bill_number,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ClickURL    string     `json:"click_url,omitempty"`
	Message     string     `json:"message,omitempty"`
}

func (h *PayHandler) returnURL(cam *domain.Camera) string {
	base := strings.TrimRight(strings.TrimSpace(h.cfg.PublicPayURL), "/")
	if base == "" {
		return ""
	}
	code := cam.Code
	if code == "" {
		code = cam.ID.String()
	}
	return base + "/" + code
}

// GetPayInfo: GET /api/v1/pay/:camera — what the driver at this exit lane owes.
func (h *PayHandler) GetPayInfo(c *fiber.Ctx) error {
	cam, err := h.anprSvc.ResolveCamera(c.Params("camera"))
	if err != nil || cam == nil {
		return response.Error(c, fiber.StatusNotFound, "Yo'lak topilmadi")
	}
	if cam.Direction != domain.DirectionExit {
		return response.Error(c, fiber.StatusBadRequest, "Bu yo'lak chiqish yo'lagi emas")
	}

	st := h.display.Get(cam)
	out := payInfo{
		CameraCode:  cam.Code,
		LaneName:    cam.Name,
		State:       st.State,
		ClickMode:   h.clickSvc.Mode(),
		PlateNumber: st.PlateNumber,
		EntryTime:   st.EntryTime,
		DurationMin: st.DurationMin,
		Amount:      st.Amount,
	}
	if b, err := h.branches.GetByID(cam.BranchID); err == nil {
		out.BranchName = b.Name
	}

	switch st.State {
	case service.DisplayPaymentRequired:
		out.State = PayStateCashier
		out.Message = "Onlayn to'lov hozir mavjud emas. Iltimos, kassada to'lang"
		if st.PaymentRequestID == nil || !h.clickSvc.Enabled() {
			break
		}
		pr, err := h.requests.GetByID(*st.PaymentRequestID)
		if err != nil || pr.Status != domain.PaymentRequestPending {
			break
		}
		out.State = PayStatePayable
		out.Amount = pr.Amount
		out.BillNumber = pr.BillNumber
		out.ExpiresAt = &pr.ExpiresAt
		out.ClickURL = h.clickSvc.PayURL(pr, h.returnURL(cam))
		out.Message = ""
	case service.DisplayPaid, service.DisplayFree:
		out.Message = "To'lov qabul qilindi. Yo'lingiz ochiq!"
	default:
		out.Message = "Hozir bu yo'lakda to'lov kutilayotgan mashina yo'q"
	}
	return response.Success(c, out)
}

func clickRequest(c *fiber.Ctx) click.Request {
	return click.ParseRequest(func(key string) string { return c.FormValue(key) })
}

// ClickPrepare: POST /api/v1/click/prepare (Click SHOP API, action=0).
func (h *PayHandler) ClickPrepare(c *fiber.Ctx) error {
	return c.JSON(h.clickSvc.Prepare(clickRequest(c)))
}

// ClickComplete: POST /api/v1/click/complete (Click SHOP API, action=1).
func (h *PayHandler) ClickComplete(c *fiber.Ctx) error {
	return c.JSON(h.clickSvc.Complete(clickRequest(c)))
}

// ClickMockPay: POST /api/v1/click/mock-pay/:bill — CLICK_MODE=mock only.
// Simulates the driver paying in Click: signed Prepare + Complete.
func (h *PayHandler) ClickMockPay(c *fiber.Ctx) error {
	if h.clickSvc.Mode() != service.ClickModeMock {
		return response.Error(c, fiber.StatusNotFound, service.ErrClickNotMock.Error())
	}
	pr, err := h.clickSvc.MockPay(c.Params("bill"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, pr, "Sinov Click to'lovi qabul qilindi")
}
