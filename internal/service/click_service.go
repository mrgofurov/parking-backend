package service

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/click"
	"github.com/smart-parking/parking-backend/internal/repository"
)

const (
	ClickModeLive = "live"
	ClickModeMock = "mock"
	ClickModeOff  = "off"

	// Keys used in mock mode when real ones are not configured. Mock mode
	// never talks to Click, it only signs its own simulated callbacks.
	mockClickServiceID  = "1"
	mockClickMerchantID = "1"
	mockClickSecretKey  = "mock-secret-key"
)

var (
	ErrClickDisabled = errors.New("Click to'lovi sozlanmagan")
	ErrClickNotMock  = errors.New("sinov to'lovi faqat CLICK_MODE=mock rejimida ishlaydi")
)

// ResolveClickMode: explicit CLICK_MODE wins (live needs keys); empty means
// live when the keys are present, else off.
func ResolveClickMode(cfg *config.Config) string {
	keys := clickConfigFrom(cfg).Configured()
	switch m := strings.ToLower(strings.TrimSpace(cfg.ClickMode)); m {
	case ClickModeMock, ClickModeOff:
		return m
	case ClickModeLive:
		if keys {
			return ClickModeLive
		}
		return ClickModeOff
	default:
		if keys {
			return ClickModeLive
		}
		return ClickModeOff
	}
}

func clickConfigFrom(cfg *config.Config) click.Config {
	return click.Config{
		ServiceID:      strings.TrimSpace(cfg.ClickServiceID),
		MerchantID:     strings.TrimSpace(cfg.ClickMerchantID),
		MerchantUserID: strings.TrimSpace(cfg.ClickMerchantUserID),
		SecretKey:      strings.TrimSpace(cfg.ClickSecretKey),
	}
}

// ClickService lets drivers pay exit PaymentRequests through Click. The
// request's bill_number is the Click transaction_param / merchant_trans_id.
type ClickService interface {
	Mode() string
	Enabled() bool
	// PayURL is the my.click.uz link for the request.
	PayURL(pr *domain.PaymentRequest, returnURL string) string
	Prepare(req click.Request) click.Response
	Complete(req click.Request) click.Response
	// MockPay runs the full signed Prepare + Complete exchange locally, as
	// Click would. Only in mock mode.
	MockPay(billNumber string) (*domain.PaymentRequest, error)
}

type clickService struct {
	repos *repository.RepositoryContainer
	qr    QRPaymentService
	cfg   click.Config
	mode  string

	// Prepare/Complete for the same transaction can arrive concurrently
	// (Click retries); one at a time keeps the state machine simple.
	mu sync.Mutex
}

func NewClickService(cfg *config.Config, repos *repository.RepositoryContainer, qr QRPaymentService) ClickService {
	mode := ResolveClickMode(cfg)
	cc := clickConfigFrom(cfg)
	switch {
	case mode == ClickModeMock && !cc.Configured():
		cc = click.Config{ServiceID: mockClickServiceID, MerchantID: mockClickMerchantID, SecretKey: mockClickSecretKey}
	case strings.EqualFold(strings.TrimSpace(cfg.ClickMode), ClickModeLive) && mode == ClickModeOff:
		log.Println("[Click] CLICK_MODE=live, lekin CLICK_SERVICE_ID / CLICK_MERCHANT_ID / CLICK_SECRET_KEY kiritilmagan — Click o'chirildi")
	}
	log.Printf("[Click] Rejim: %s\n", mode)
	return &clickService{repos: repos, qr: qr, cfg: cc, mode: mode}
}

func (s *clickService) Mode() string  { return s.mode }
func (s *clickService) Enabled() bool { return s.mode != ClickModeOff }

func (s *clickService) PayURL(pr *domain.PaymentRequest, returnURL string) string {
	if !s.Enabled() || pr == nil {
		return ""
	}
	return s.cfg.PayURL(pr.Amount, pr.BillNumber, returnURL)
}

func sumsMatch(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func reply(req click.Request, code int) click.Response {
	id, _ := strconv.ParseInt(req.ClickTransID, 10, 64)
	return click.Response{ClickTransID: id, MerchantTransID: req.MerchantTransID, Error: code, ErrorNote: click.ErrorNote(code)}
}

// validate does the checks shared by Prepare and Complete.
func (s *clickService) validate(req click.Request, action int) (clickTransID int64, amount float64, code int) {
	if !s.Enabled() {
		return 0, 0, click.ErrBadRequest
	}
	if !req.SignValid(s.cfg.SecretKey) {
		return 0, 0, click.ErrSignCheckFailed
	}
	if a, err := req.ActionInt(); err != nil || a != action {
		return 0, 0, click.ErrActionNotFound
	}
	if req.ServiceID != s.cfg.ServiceID || req.MerchantTransID == "" {
		return 0, 0, click.ErrBadRequest
	}
	id, err := strconv.ParseInt(req.ClickTransID, 10, 64)
	if err != nil {
		return 0, 0, click.ErrBadRequest
	}
	amt, err := req.AmountFloat()
	if err != nil || amt <= 0 {
		return 0, 0, click.ErrIncorrectAmount
	}
	return id, amt, click.ErrSuccess
}

// payable reports why a request cannot take a new payment (0 = it can).
func (s *clickService) payable(pr *domain.PaymentRequest) int {
	switch pr.Status {
	case domain.PaymentRequestPaid:
		return click.ErrAlreadyPaid
	case domain.PaymentRequestPending, domain.PaymentRequestExpired:
	default: // cancelled (paid at the cashier) or failed
		return click.ErrTransactionCanceled
	}
	session, err := s.repos.ParkingSession.GetByID(pr.SessionID)
	if err != nil {
		return click.ErrUserNotFound
	}
	switch session.Status {
	case domain.SessionActive:
		return click.ErrSuccess
	case domain.SessionCancelled:
		return click.ErrTransactionCanceled
	default:
		return click.ErrAlreadyPaid
	}
}

func (s *clickService) Prepare(req click.Request) click.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	clickTransID, amount, code := s.validate(req, click.ActionPrepare)
	if code != click.ErrSuccess {
		log.Printf("[Click] Prepare rad etildi trans=%s bill=%s: %d %s\n", req.ClickTransID, req.MerchantTransID, code, click.ErrorNote(code))
		return reply(req, code)
	}

	pr, err := s.repos.PaymentRequest.GetByBillNumber(req.MerchantTransID)
	if err != nil {
		return reply(req, click.ErrUserNotFound)
	}

	// Click retrying a Prepare it already sent gets the same answer.
	if tx, err := s.repos.ClickTx.GetByClickTransID(clickTransID); err == nil {
		switch tx.Status {
		case domain.ClickTxCancelled:
			return reply(req, click.ErrTransactionCanceled)
		case domain.ClickTxConfirmed:
			return reply(req, click.ErrAlreadyPaid)
		}
		resp := reply(req, click.ErrSuccess)
		resp.MerchantPrepareID = &tx.ID
		return resp
	}

	if req.ErrorInt() < 0 {
		return reply(req, click.ErrTransactionCanceled)
	}
	if code := s.payable(pr); code != click.ErrSuccess {
		return reply(req, code)
	}
	if !sumsMatch(amount, pr.Amount) {
		log.Printf("[Click] %s bill=%s: summa mos emas (keldi %.2f, kutilgan %.0f)\n", pr.PlateNumber, pr.BillNumber, amount, pr.Amount)
		return reply(req, click.ErrIncorrectAmount)
	}

	paydoc, _ := strconv.ParseInt(req.ClickPaydocID, 10, 64)
	now := time.Now()
	tx := &domain.ClickTransaction{
		ClickTransID:     clickTransID,
		ClickPaydocID:    paydoc,
		PaymentRequestID: pr.ID,
		BillNumber:       pr.BillNumber,
		Amount:           amount,
		Status:           domain.ClickTxPrepared,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repos.ClickTx.Create(tx); err != nil {
		log.Printf("[Click] Prepare saqlanmadi trans=%d: %v\n", clickTransID, err)
		return reply(req, click.ErrFailedToUpdate)
	}
	log.Printf("[Click] Prepare OK %s bill=%s trans=%d summa=%.0f prepare_id=%d\n", pr.PlateNumber, pr.BillNumber, clickTransID, amount, tx.ID)
	resp := reply(req, click.ErrSuccess)
	resp.MerchantPrepareID = &tx.ID
	return resp
}

func (s *clickService) Complete(req click.Request) click.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	clickTransID, amount, code := s.validate(req, click.ActionComplete)
	if code != click.ErrSuccess {
		log.Printf("[Click] Complete rad etildi trans=%s bill=%s: %d %s\n", req.ClickTransID, req.MerchantTransID, code, click.ErrorNote(code))
		return reply(req, code)
	}

	prepareID, err := strconv.ParseInt(req.MerchantPrepareID, 10, 64)
	if err != nil {
		return reply(req, click.ErrTransactionNotFound)
	}
	tx, err := s.repos.ClickTx.GetByID(prepareID)
	if err != nil || tx.ClickTransID != clickTransID || tx.BillNumber != req.MerchantTransID {
		return reply(req, click.ErrTransactionNotFound)
	}

	confirmed := func() click.Response {
		resp := reply(req, click.ErrSuccess)
		resp.MerchantConfirmID = &tx.ID
		return resp
	}
	switch tx.Status {
	case domain.ClickTxConfirmed:
		resp := confirmed()
		resp.Error, resp.ErrorNote = click.ErrAlreadyPaid, click.ErrorNote(click.ErrAlreadyPaid)
		return resp
	case domain.ClickTxCancelled:
		return reply(req, click.ErrTransactionCanceled)
	}

	cancel := func(note string) click.Response {
		tx.Status = domain.ClickTxCancelled
		tx.Note = note
		tx.UpdatedAt = time.Now()
		_ = s.repos.ClickTx.Update(tx)
		log.Printf("[Click] trans=%d bill=%s bekor qilindi: %s\n", clickTransID, tx.BillNumber, note)
		return reply(req, click.ErrTransactionCanceled)
	}

	// Click itself failed the payment: void our side.
	if req.ErrorInt() < 0 {
		return cancel("Click xatosi: " + req.ErrorNote)
	}
	if !sumsMatch(amount, tx.Amount) {
		return reply(req, click.ErrIncorrectAmount)
	}

	pr, err := s.repos.PaymentRequest.GetByID(tx.PaymentRequestID)
	if err != nil {
		return cancel("to'lov so'rovi topilmadi")
	}
	// Paid elsewhere in the meantime (cashier / UzQR): -9 makes Click return
	// the money instead of charging the driver twice.
	if code := s.payable(pr); code != click.ErrSuccess {
		return cancel(fmt.Sprintf("so'rov endi to'lanmaydi (%s)", click.ErrorNote(code)))
	}

	ref := fmt.Sprintf("CLICK:%d", clickTransID)
	done, err := s.qr.CompleteExternal(pr.ID, amount, ref, "Click")
	if err != nil || done == nil || done.Status != domain.PaymentRequestPaid || done.ExternalPaymentID != ref {
		return cancel(fmt.Sprintf("yakunlanmadi: %v", err))
	}

	tx.Status = domain.ClickTxConfirmed
	tx.Note = ""
	tx.UpdatedAt = time.Now()
	if err := s.repos.ClickTx.Update(tx); err != nil {
		log.Printf("[Click] DIQQAT: trans=%d to'landi, lekin holati saqlanmadi: %v\n", clickTransID, err)
	}
	log.Printf("[Click] Complete OK %s bill=%s trans=%d summa=%.0f\n", pr.PlateNumber, pr.BillNumber, clickTransID, amount)
	return confirmed()
}

func (s *clickService) MockPay(billNumber string) (*domain.PaymentRequest, error) {
	if s.mode != ClickModeMock {
		return nil, ErrClickNotMock
	}
	pr, err := s.repos.PaymentRequest.GetByBillNumber(billNumber)
	if err != nil {
		return nil, errors.New("to'lov so'rovi topilmadi")
	}

	transID := strconv.FormatInt(time.Now().UnixNano()/1000, 10)
	req := click.Request{
		ClickTransID:    transID,
		ServiceID:       s.cfg.ServiceID,
		ClickPaydocID:   transID,
		MerchantTransID: pr.BillNumber,
		Amount:          click.FormatAmount(pr.Amount),
		Action:          strconv.Itoa(click.ActionPrepare),
		Error:           "0",
		ErrorNote:       "Success",
		SignTime:        time.Now().Format("2006-01-02 15:04:05"),
	}
	req.SignString = req.ExpectedSign(s.cfg.SecretKey)
	prep := s.Prepare(req)
	if prep.Error != click.ErrSuccess || prep.MerchantPrepareID == nil {
		return nil, fmt.Errorf("Prepare: %d %s", prep.Error, prep.ErrorNote)
	}

	req.Action = strconv.Itoa(click.ActionComplete)
	req.MerchantPrepareID = strconv.FormatInt(*prep.MerchantPrepareID, 10)
	req.SignString = req.ExpectedSign(s.cfg.SecretKey)
	if comp := s.Complete(req); comp.Error != click.ErrSuccess {
		return nil, fmt.Errorf("Complete: %d %s", comp.Error, comp.ErrorNote)
	}
	return s.repos.PaymentRequest.GetByID(pr.ID)
}
