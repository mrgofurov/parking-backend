package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/munis"
	"github.com/smart-parking/parking-backend/internal/repository"
)

const (
	QRModeLive = "live"
	QRModeMock = "mock"
	QRModeOff  = "off"

	ProviderMunis = "MUNIS"
	ProviderMock  = "MOCK"
)

var ErrQRDisabled = errors.New("QR to'lov (Munis) sozlanmagan")

// QRPaymentService issues Munis (UzQR) dynamic QR codes for exit payments and
// watches for the payment to arrive; on success the session is completed and
// the exit barrier opened via PaymentService.
type QRPaymentService interface {
	Mode() string
	// RequestForSession returns the pending QR for the session, creating one
	// when there is none (or the old one expired).
	RequestForSession(session *domain.ParkingSession, cameraID *uuid.UUID) (*domain.PaymentRequest, error)
	Check(id uuid.UUID) (*domain.PaymentRequest, error)
	MockPay(id uuid.UUID) (*domain.PaymentRequest, error)
	Cancel(id uuid.UUID) (*domain.PaymentRequest, error)
	List(status string, limit int) ([]domain.PaymentRequest, error)
	Run(ctx context.Context)
}

type qrPaymentService struct {
	repos      *repository.RepositoryContainer
	paymentSvc PaymentService
	display    *DisplayService
	client     *munis.Client
	mode       string
	ttl        time.Duration
	poll       time.Duration
}

func NewQRPaymentService(cfg *config.Config, repos *repository.RepositoryContainer, paymentSvc PaymentService, display *DisplayService) QRPaymentService {
	client := munis.NewClient(munis.Config{
		ApiURL:      strings.TrimRight(cfg.MunisApiURL, "/"),
		Username:    cfg.MunisApiUsername,
		Password:    cfg.MunisApiPassword,
		QRApiURL:    strings.TrimRight(cfg.MunisQRApiURL, "/"),
		QRAuthToken: cfg.MunisQRAuthToken,
		OriginalQR:  cfg.MunisOriginalQR,
	})

	mode := strings.ToLower(strings.TrimSpace(cfg.MunisMode))
	switch mode {
	case QRModeMock, QRModeOff:
	case QRModeLive:
		if !client.CanGenerate() {
			log.Println("[QRPay] MUNIS_MODE=live, lekin MUNIS_QR_API_URL / MUNIS_QR_AUTH_TOKEN kiritilmagan — QR o'chirildi")
			mode = QRModeOff
		}
	default:
		if client.CanGenerate() {
			mode = QRModeLive
		} else {
			mode = QRModeOff
		}
	}
	if mode == QRModeLive && !client.CanVerify() {
		log.Println("[QRPay] OGOHLANTIRISH: MUNIS_API_URL / USERNAME / PASSWORD yo'q — to'lovni avtomatik tekshirib bo'lmaydi")
	}

	ttl := time.Duration(cfg.PaymentQRTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	poll := time.Duration(cfg.PaymentPollSeconds) * time.Second
	if poll <= 0 {
		poll = 3 * time.Second
	}
	log.Printf("[QRPay] Rejim: %s (QR amal qilish muddati %s, tekshiruv har %s)\n", mode, ttl, poll)

	return &qrPaymentService{
		repos:      repos,
		paymentSvc: paymentSvc,
		display:    display,
		client:     client,
		mode:       mode,
		ttl:        ttl,
		poll:       poll,
	}
}

func (s *qrPaymentService) Mode() string { return s.mode }

// newBillNumber: 15 digits (yyMMddHHmm + 5 random) — well under Munis' 25-char
// receiptNumber limit and numeric like magnitgo-crm's sale_number.
func newBillNumber() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(100000))
	return fmt.Sprintf("%s%05d", time.Now().Format("0601021504"), n.Int64())
}

func (s *qrPaymentService) RequestForSession(session *domain.ParkingSession, cameraID *uuid.UUID) (*domain.PaymentRequest, error) {
	if s.mode == QRModeOff {
		return nil, ErrQRDisabled
	}
	amount := munis.ChargedSum(session.FinalFee)
	if amount <= 0 {
		return nil, errors.New("to'lov summasi 0")
	}

	// Reuse a still-valid QR: the amount is locked for its lifetime so a driver
	// who already scanned it is never asked for a different sum.
	if existing, err := s.repos.PaymentRequest.GetPendingBySession(session.ID); err == nil && existing != nil {
		if time.Now().Before(existing.ExpiresAt) {
			if cameraID != nil && (existing.CameraID == nil || *existing.CameraID != *cameraID) {
				existing.CameraID = cameraID
				existing.UpdatedAt = time.Now()
				_ = s.repos.PaymentRequest.Update(existing)
			}
			return existing, nil
		}
		// Expired: check one last time before replacing it.
		if checked, _ := s.checkOne(existing); checked != nil && checked.Status == domain.PaymentRequestPaid {
			return checked, nil
		}
		if _, err := s.repos.PaymentRequest.MarkStatus(existing.ID, domain.PaymentRequestPending, domain.PaymentRequestExpired); err != nil {
			log.Printf("[QRPay] %s: could not expire old QR %s: %v\n", session.PlateNumber, existing.BillNumber, err)
		}
	}

	now := time.Now()
	pr := &domain.PaymentRequest{
		ID:          uuid.New(),
		SessionID:   session.ID,
		BranchID:    session.BranchID,
		CameraID:    cameraID,
		PlateNumber: session.PlateNumber,
		BillNumber:  newBillNumber(),
		Amount:      amount,
		Status:      domain.PaymentRequestPending,
		ExpiresAt:   now.Add(s.ttl),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if s.mode == QRModeMock {
		pr.Provider = ProviderMock
		pr.QRPayload = fmt.Sprintf("MOCK-UZQR|bill=%s|amount=%.0f|plate=%s", pr.BillNumber, amount, session.PlateNumber)
	} else {
		pr.Provider = ProviderMunis
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		qr, err := s.client.GenerateQR(ctx, amount, pr.BillNumber)
		if err != nil {
			return nil, fmt.Errorf("munis QR yaratilmadi: %w", err)
		}
		pr.QRPayload = qr
	}

	if err := s.repos.PaymentRequest.Create(pr); err != nil {
		return nil, fmt.Errorf("QR so'rovini saqlab bo'lmadi: %w", err)
	}
	log.Printf("[QRPay] %s: QR yaratildi bill=%s summa=%.0f (%s)\n", session.PlateNumber, pr.BillNumber, amount, pr.Provider)
	return pr, nil
}

// checkOne asks Munis whether the request was paid and completes it if so.
func (s *qrPaymentService) checkOne(pr *domain.PaymentRequest) (*domain.PaymentRequest, error) {
	if pr.Status != domain.PaymentRequestPending || pr.Provider != ProviderMunis || !s.client.CanVerify() {
		return pr, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()
	pr.LastCheckedAt = &now
	res, err := s.client.GetPayment(ctx, pr.BillNumber)
	if err != nil {
		pr.LastError = err.Error()
		pr.UpdatedAt = now
		_ = s.repos.PaymentRequest.Update(pr)
		return pr, err
	}
	if !res.Found() {
		pr.LastError = ""
		pr.UpdatedAt = now
		_ = s.repos.PaymentRequest.Update(pr)
		return pr, nil
	}

	paid := res.ResponseBody.PaidSum()
	ref := fmt.Sprintf("MUNIS:%d", res.ResponseBody.PaymentID)
	if !munis.AmountMatches(paid, pr.Amount) {
		log.Printf("[QRPay] %s: summa mos emas bill=%s to'landi=%.2f kutilgan=%.0f\n", pr.PlateNumber, pr.BillNumber, paid, pr.Amount)
		pr.ExternalPaymentID = ref
		pr.PaidAmount = paid
		pr.LastError = fmt.Sprintf("to'langan summa (%.0f) kutilgan summaga (%.0f) mos emas", paid, pr.Amount)
		pr.UpdatedAt = now
		_ = s.repos.PaymentRequest.Update(pr)
		if won, _ := s.repos.PaymentRequest.MarkStatus(pr.ID, domain.PaymentRequestPending, domain.PaymentRequestFailed); won {
			pr.Status = domain.PaymentRequestFailed
			s.showError(pr, "To'lov summasi mos kelmadi. Operatorga murojaat qiling.")
		}
		return pr, nil
	}
	return s.complete(pr, paid, ref, "UzQR (Munis)")
}

func (s *qrPaymentService) complete(pr *domain.PaymentRequest, paid float64, ref, operatorName string) (*domain.PaymentRequest, error) {
	won, err := s.repos.PaymentRequest.MarkStatus(pr.ID, domain.PaymentRequestPending, domain.PaymentRequestPaid)
	if err != nil {
		return pr, err
	}
	if !won {
		return s.repos.PaymentRequest.GetByID(pr.ID)
	}

	now := time.Now()
	pr.Status = domain.PaymentRequestPaid
	pr.PaidAt = &now
	pr.PaidAmount = paid
	pr.ExternalPaymentID = ref
	pr.LastError = ""

	payment, err := s.paymentSvc.CompleteSessionPayment(pr.SessionID, CompletePaymentInput{
		Amount:           paid,
		Method:           domain.PaymentQR,
		TransactionRef:   ref,
		OperatorName:     operatorName,
		PaymentRequestID: &pr.ID,
	})
	if err != nil {
		// Money arrived but the session was already closed (e.g. operator took
		// cash). Keep the record so it can be refunded.
		log.Printf("[QRPay] DIQQAT: %s bill=%s to'lov keldi, lekin sessiya yopilmadi: %v\n", pr.PlateNumber, pr.BillNumber, err)
		pr.LastError = "to'lov keldi, lekin sessiya yopilmadi: " + err.Error()
	} else {
		pr.PaymentID = &payment.ID
	}
	pr.UpdatedAt = time.Now()
	_ = s.repos.PaymentRequest.Update(pr)
	return pr, nil
}

func (s *qrPaymentService) showError(pr *domain.PaymentRequest, msg string) {
	if s.display == nil || pr.CameraID == nil {
		return
	}
	cam, err := s.repos.Camera.GetByID(*pr.CameraID)
	if err != nil {
		return
	}
	s.display.Set(cam, DisplayState{State: DisplayError, PlateNumber: pr.PlateNumber, Amount: pr.Amount, Message: msg})
}

func (s *qrPaymentService) Check(id uuid.UUID) (*domain.PaymentRequest, error) {
	pr, err := s.repos.PaymentRequest.GetByID(id)
	if err != nil {
		return nil, errors.New("to'lov so'rovi topilmadi")
	}
	return s.checkOne(pr)
}

func (s *qrPaymentService) MockPay(id uuid.UUID) (*domain.PaymentRequest, error) {
	pr, err := s.repos.PaymentRequest.GetByID(id)
	if err != nil {
		return nil, errors.New("to'lov so'rovi topilmadi")
	}
	if pr.Provider != ProviderMock {
		return nil, errors.New("faqat MOCK rejimidagi so'rovni sinov uchun to'lash mumkin")
	}
	if pr.Status != domain.PaymentRequestPending {
		return nil, fmt.Errorf("so'rov holati: %s", pr.Status)
	}
	return s.complete(pr, pr.Amount, "MOCK:"+pr.BillNumber, "UzQR (sinov)")
}

func (s *qrPaymentService) Cancel(id uuid.UUID) (*domain.PaymentRequest, error) {
	pr, err := s.repos.PaymentRequest.GetByID(id)
	if err != nil {
		return nil, errors.New("to'lov so'rovi topilmadi")
	}
	if won, _ := s.repos.PaymentRequest.MarkStatus(id, domain.PaymentRequestPending, domain.PaymentRequestCancelled); !won {
		return nil, fmt.Errorf("so'rovni bekor qilib bo'lmaydi (holati: %s)", pr.Status)
	}
	pr.Status = domain.PaymentRequestCancelled
	return pr, nil
}

func (s *qrPaymentService) List(status string, limit int) ([]domain.PaymentRequest, error) {
	return s.repos.PaymentRequest.List(status, limit)
}

// reissueIfDisplayed: the car is still standing at the exit looking at an
// expired QR — give it a fresh one for the same amount.
func (s *qrPaymentService) reissueIfDisplayed(old *domain.PaymentRequest) {
	if s.display == nil {
		return
	}
	camID, ok := s.display.FindByPaymentRequest(old.ID)
	if !ok {
		return
	}
	cam, err := s.repos.Camera.GetByID(camID)
	if err != nil {
		return
	}
	session, err := s.repos.ParkingSession.GetByID(old.SessionID)
	if err != nil || session.Status != domain.SessionActive {
		return
	}
	pr, err := s.RequestForSession(session, &cam.ID)
	if err != nil {
		s.display.Set(cam, DisplayState{State: DisplayError, PlateNumber: session.PlateNumber, Amount: session.FinalFee,
			Message: "QR muddati tugadi. Operatorga murojaat qiling."})
		return
	}
	cur := s.display.Get(cam)
	cur.QRPayload = pr.QRPayload
	cur.PaymentRequestID = &pr.ID
	cur.ExpiresAt = &pr.ExpiresAt
	cur.Amount = pr.Amount
	s.display.Set(cam, cur)
}

// Run polls pending requests until ctx is cancelled.
func (s *qrPaymentService) Run(ctx context.Context) {
	if s.mode == QRModeOff {
		return
	}
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce()
		}
	}
}

func (s *qrPaymentService) pollOnce() {
	pending, err := s.repos.PaymentRequest.ListPending()
	if err != nil {
		log.Printf("[QRPay] pending ro'yxatini o'qib bo'lmadi: %v\n", err)
		return
	}
	now := time.Now()
	for i := range pending {
		pr := &pending[i]
		checked, _ := s.checkOne(pr)
		if checked != nil && checked.Status == domain.PaymentRequestPending && now.After(pr.ExpiresAt) {
			if won, _ := s.repos.PaymentRequest.MarkStatus(pr.ID, domain.PaymentRequestPending, domain.PaymentRequestExpired); won {
				log.Printf("[QRPay] %s: QR muddati tugadi bill=%s\n", pr.PlateNumber, pr.BillNumber)
				s.reissueIfDisplayed(pr)
			}
		}
	}
}
