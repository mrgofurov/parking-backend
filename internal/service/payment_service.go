package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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
	GetReceipt(paymentID uuid.UUID) (*ReceiptData, error)
	ListPayments(page, limit int) ([]domain.Payment, int64, error)
}

type paymentService struct {
	repos      *repository.RepositoryContainer
	barrierSvc BarrierService
	hub        *WebSocketHub
}

func NewPaymentService(
	repos *repository.RepositoryContainer,
	barrierSvc BarrierService,
	hub *WebSocketHub,
) PaymentService {
	return &paymentService{
		repos:      repos,
		barrierSvc: barrierSvc,
		hub:        hub,
	}
}

func (s *paymentService) ProcessPayment(req *ProcessPaymentRequest) (*domain.Payment, error) {
	session, err := s.repos.ParkingSession.GetByID(req.SessionID)
	if err != nil {
		return nil, errors.New("sessiya topilmadi")
	}

	if session.Status == domain.SessionCompleted || session.Status == domain.SessionPaid {
		return nil, errors.New("bu sessiya uchun to'lov allaqachon amalga oshirilgan")
	}

	now := time.Now()
	receiptNum := generateReceiptNumber()

	payment := &domain.Payment{
		ID:             uuid.New(),
		SessionID:      session.ID,
		Amount:         req.Amount,
		PaymentMethod:  req.PaymentMethod,
		TransactionRef: req.TransactionRef,
		OperatorID:     req.OperatorID,
		OperatorName:   req.OperatorName,
		ReceiptNumber:  receiptNum,
		PaidAt:         now,
		CreatedAt:      now,
	}

	if err := s.repos.Payment.Create(payment); err != nil {
		return nil, fmt.Errorf("to'lovni saqlashda xatolik: %w", err)
	}

	// Update session
	session.Status = domain.SessionCompleted
	if session.ExitTime == nil {
		session.ExitTime = &now
		session.DurationMin = int(now.Sub(session.EntryTime).Minutes())
	}
	session.FinalFee = req.Amount
	session.UpdatedAt = now
	_ = s.repos.ParkingSession.Update(session)

	// Decrement occupancy
	branch, _ := s.repos.Branch.GetFirst()
	if branch != nil {
		activeCount, _ := s.repos.ParkingSession.CountActive()
		_ = s.repos.Branch.UpdateOccupiedSpaces(branch.ID, int(activeCount))

		s.hub.Broadcast(WSEventOccupancyUpdate, map[string]interface{}{
			"occupied_spaces": activeCount,
			"total_spaces":    branch.TotalSpaces,
			"available_spaces": branch.TotalSpaces - int(activeCount),
		})
	}

	// Open EXIT barrier
	_ = s.barrierSvc.OpenByDirection(domain.DirectionExit, fmt.Sprintf("To'lov qilindi (%.0f UZS): %s", req.Amount, session.PlateNumber))

	// Broadcast WS
	s.hub.Broadcast(WSEventPayment, payment)
	s.hub.Broadcast(WSEventExit, session)

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

	branch, _ := s.repos.Branch.GetFirst()
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
