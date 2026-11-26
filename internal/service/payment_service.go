package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type ProcessPaymentRequest struct {
	SessionID      uuid.UUID            `json:"session_id"`
	Amount         float64              `json:"amount"`
	PaymentMethod  domain.PaymentMethod `json:"payment_method"` // CASH, CARD, QR
	TransactionRef string               `json:"transaction_ref,omitempty"`
	OperatorID     *uuid.UUID           `json:"operator_id,omitempty"`
	OperatorName   string               `json:"operator_name,omitempty"`
}

type ReceiptData struct {
	ReceiptNumber  string               `json:"receipt_number"`
	ParkingName    string               `json:"parking_name"`
	Address        string               `json:"address"`
	PlateNumber    string               `json:"plate_number"`
	SessionCode    string               `json:"session_code"`
	EntryTime      time.Time            `json:"entry_time"`
	ExitTime       time.Time            `json:"exit_time"`
	DurationMin    int                  `json:"duration_min"`
	CalculatedFee  float64              `json:"calculated_fee"`
	DiscountAmount float64              `json:"discount_amount"`
	PaidAmount     float64              `json:"paid_amount"`
	PaymentMethod  domain.PaymentMethod `json:"payment_method"`
	OperatorName   string               `json:"operator_name"`
	PaidAt         time.Time            `json:"paid_at"`
	QRData         string               `json:"qr_data"`
}

type PaymentService interface {
	ProcessPayment(req *ProcessPaymentRequest) (*domain.Payment, error)
	// CompleteSessionPayment records a payment, closes the session, opens the
	// exit barrier of the lane the car stands at and updates its display.
	CompleteSessionPayment(sessionID uuid.UUID, in CompletePaymentInput) (*domain.Payment, error)
	GetReceipt(paymentID uuid.UUID) (*ReceiptData, error)
	ListPayments(page, limit int) ([]domain.Payment, int64, error)
}

type CompletePaymentInput struct {
	Amount           float64
	Method           domain.PaymentMethod
	TransactionRef   string
	OperatorID       *uuid.UUID
	OperatorName     string
	PaymentRequestID *uuid.UUID // set when paid through a QR request
}

type paymentService struct {
	repos      *repository.RepositoryContainer
	barrierSvc BarrierService
	display    *DisplayService
	hub        *WebSocketHub
	mu         sync.Mutex
}

func NewPaymentService(
	repos *repository.RepositoryContainer,
	barrierSvc BarrierService,
	display *DisplayService,
	hub *WebSocketHub,
) PaymentService {
	return &paymentService{
		repos:      repos,
		barrierSvc: barrierSvc,
		display:    display,
		hub:        hub,
	}
}

func (s *paymentService) ProcessPayment(req *ProcessPaymentRequest) (*domain.Payment, error) {
	if req.PaymentMethod == "" {
		req.PaymentMethod = domain.PaymentCash
	}
	if req.Amount < 0 {
		return nil, errors.New("to'lov summasi manfiy bo'lishi mumkin emas")
	}
	return s.CompleteSessionPayment(req.SessionID, CompletePaymentInput{
		Amount:         req.Amount,
		Method:         req.PaymentMethod,
		TransactionRef: req.TransactionRef,
		OperatorID:     req.OperatorID,
		OperatorName:   req.OperatorName,
	})
}

func (s *paymentService) CompleteSessionPayment(sessionID uuid.UUID, in CompletePaymentInput) (*domain.Payment, error) {
	// Serialise completions so an operator payment and a QR payment arriving at
	// the same moment cannot both close the session.
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := s.repos.ParkingSession.GetByID(sessionID)
	if err != nil {
		return nil, errors.New("sessiya topilmadi")
	}

	if session.Status == domain.SessionCompleted || session.Status == domain.SessionPaid {
		return nil, errors.New("bu sessiya uchun to'lov allaqachon amalga oshirilgan")
	}
	if session.Status == domain.SessionCancelled {
		return nil, errors.New("sessiya bekor qilingan")
	}

	now := time.Now()
	payment := &domain.Payment{
		ID:             uuid.New(),
		SessionID:      session.ID,
		Amount:         in.Amount,
		PaymentMethod:  in.Method,
		TransactionRef: in.TransactionRef,
		OperatorID:     in.OperatorID,
		OperatorName:   in.OperatorName,
		ReceiptNumber:  generateReceiptNumber(),
		PaidAt:         now,
		CreatedAt:      now,
	}

	if err := s.repos.Payment.Create(payment); err != nil {
		return nil, fmt.Errorf("to'lovni saqlashda xatolik: %w", err)
	}

	session.Status = domain.SessionCompleted
	if session.ExitTime == nil {
		session.ExitTime = &now
		session.DurationMin = int(now.Sub(session.EntryTime).Minutes())
	}
	session.FinalFee = in.Amount
	session.UpdatedAt = now
	session.Payments = nil
	if err := s.repos.ParkingSession.Update(session); err != nil {
		log.Printf("[Payment] Session %s update failed after payment %s: %v\n", session.SessionCode, payment.ReceiptNumber, err)
	}

	// Any other QR still waiting for this session is now void.
	if pending, err := s.repos.PaymentRequest.GetPendingBySession(session.ID); err == nil && pending != nil {
		if in.PaymentRequestID == nil || pending.ID != *in.PaymentRequestID {
			_, _ = s.repos.PaymentRequest.MarkStatus(pending.ID, domain.PaymentRequestPending, domain.PaymentRequestCancelled)
		}
	}

	// Occupancy of the session's branch
	if branch, err := s.repos.Branch.GetByID(session.BranchID); err == nil {
		activeCount, _ := s.repos.ParkingSession.CountActiveByBranch(&branch.ID)
		_ = s.repos.Branch.UpdateOccupiedSpaces(branch.ID, int(activeCount))
		s.hub.Broadcast(WSEventOccupancyUpdate, map[string]interface{}{
			"branch_id":        branch.ID,
			"occupied_spaces":  activeCount,
			"total_spaces":     branch.TotalSpaces,
			"available_spaces": branch.TotalSpaces - int(activeCount),
		})
	}

	// Open the exit barrier of the lane the car was seen at.
	var exitCam *domain.Camera
	var barrierID *uuid.UUID
	if session.ExitCameraID != nil {
		if cam, err := s.repos.Camera.GetByID(*session.ExitCameraID); err == nil {
			exitCam = cam
			barrierID = cam.BarrierID
		}
	}
	reason := fmt.Sprintf("To'lov qilindi (%.0f UZS, %s): %s", in.Amount, in.Method, session.PlateNumber)
	barrierErr := s.barrierSvc.OpenForLane(session.BranchID, domain.DirectionExit, barrierID, reason)

	if exitCam != nil && s.display != nil {
		msg := "To'lov qabul qilindi. Yo'lingiz ochiq!"
		state := DisplayPaid
		if barrierErr != nil {
			msg = "To'lov qabul qilindi. Shlagbaum ochilmadi — operatorga murojaat qiling."
			state = DisplayError
		}
		s.display.Set(exitCam, DisplayState{
			State:          state,
			PlateNumber:    session.PlateNumber,
			EntryTime:      &session.EntryTime,
			ExitTime:       session.ExitTime,
			DurationMin:    session.DurationMin,
			CalculatedFee:  session.CalculatedFee,
			DiscountAmount: session.DiscountAmount,
			Amount:         in.Amount,
			Message:        msg,
		})
	}

	s.hub.Broadcast(WSEventPayment, payment)
	s.hub.Broadcast(WSEventExit, session)

	log.Printf("[Payment] %s paid %.0f UZS via %s (receipt %s)\n", session.PlateNumber, in.Amount, in.Method, payment.ReceiptNumber)
	return payment, nil
}

func (s *paymentService) GetReceipt(paymentID uuid.UUID) (*ReceiptData, error) {
	payment, err := s.repos.Payment.GetByID(paymentID)
	if err != nil {
		return nil, errors.New("to'lov topilmadi")
	}

	session, err := s.repos.ParkingSession.GetByID(payment.SessionID)
	if err != nil {
		return nil, errors.New("bog'langan sessiya topilmadi")
	}

	branch, _ := s.repos.Branch.GetByID(session.BranchID)
	branchName := "Smart Parking"
	address := "Toshkent shahri"
	if branch != nil {
		branchName = branch.Name
		address = branch.Address
	}

	exitTime := payment.PaidAt
	if session.ExitTime != nil {
		exitTime = *session.ExitTime
	}

	qrData := fmt.Sprintf("VERIFY:%s:%s:%.0fUZS:%s", payment.ReceiptNumber, session.PlateNumber, payment.Amount, payment.PaidAt.Format(time.RFC3339))

	return &ReceiptData{
		ReceiptNumber:  payment.ReceiptNumber,
		ParkingName:    branchName,
		Address:        address,
		PlateNumber:    session.PlateNumber,
		SessionCode:    session.SessionCode,
		EntryTime:      session.EntryTime,
		ExitTime:       exitTime,
		DurationMin:    session.DurationMin,
		CalculatedFee:  session.CalculatedFee,
		DiscountAmount: session.DiscountAmount,
		PaidAmount:     payment.Amount,
		PaymentMethod:  payment.PaymentMethod,
		OperatorName:   payment.OperatorName,
		PaidAt:         payment.PaidAt,
		QRData:         qrData,
	}, nil
}

func (s *paymentService) ListPayments(page, limit int) ([]domain.Payment, int64, error) {
	return s.repos.Payment.List(page, limit)
}

func generateReceiptNumber() string {
	dateStr := time.Now().Format("20060102")
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	randHex := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("RCP-%s-%s", dateStr, randHex)
}
