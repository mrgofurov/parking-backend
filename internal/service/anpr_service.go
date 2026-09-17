package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type ANPREventRequest struct {
	PlateNumber    string               `json:"plate_number"`
	CameraID       *uuid.UUID           `json:"camera_id,omitempty"`
	Direction      domain.DirectionType `json:"direction"` // ENTRY or EXIT
	Confidence     float64              `json:"confidence"`
	Timestamp      *time.Time           `json:"timestamp,omitempty"`
	SnapshotBase64 string               `json:"snapshot_base64,omitempty"`
	SnapshotURL    string               `json:"snapshot_url,omitempty"`
}

type ANPRProcessResult struct {
	Session        *domain.ParkingSession `json:"session"`
	BarrierOpened  bool                   `json:"barrier_opened"`
	RequiresPayment bool                  `json:"requires_payment"`
	AmountDue      float64                `json:"amount_due"`
	Message        string                 `json:"message"`
}

type ANPRService interface {
	ProcessEvent(req *ANPREventRequest) (*ANPRProcessResult, error)
}

type anprService struct {
	repos      *repository.RepositoryContainer
	tariffSvc  TariffService
	barrierSvc BarrierService
	hub        *WebSocketHub
	uploadDir  string

	// Anti-duplicate debounce cache
	recentEvents map[string]time.Time
	cacheMu      sync.Mutex
}

func NewANPRService(
	repos *repository.RepositoryContainer,
	tariffSvc TariffService,
	barrierSvc BarrierService,
	hub *WebSocketHub,
	uploadDir string,
) ANPRService {
	return &anprService{
		repos:        repos,
		tariffSvc:    tariffSvc,
		barrierSvc:   barrierSvc,
		hub:          hub,
		uploadDir:    uploadDir,
		recentEvents: make(map[string]time.Time),
	}
}

func (s *anprService) ProcessEvent(req *ANPREventRequest) (*ANPRProcessResult, error) {
	cleanPlate := NormalizePlate(req.PlateNumber)
	if cleanPlate == "" {
		return nil, errors.New("davlat raqami ko'rsatilmagan")
	}

	eventTime := time.Now()
	if req.Timestamp != nil {
		eventTime = *req.Timestamp
	}

	// 1. Anti-duplicate debounce (ignore exact same plate & direction within 12 seconds)
	debounceKey := fmt.Sprintf("%s:%s", cleanPlate, req.Direction)
	s.cacheMu.Lock()
	lastSeen, exists := s.recentEvents[debounceKey]
	if exists && time.Since(lastSeen) < 12*time.Second {
		s.cacheMu.Unlock()
		log.Printf("[ANPR] Duplicate event ignored for %s (%s)\n", cleanPlate, req.Direction)
		active, _ := s.repos.ParkingSession.FindActiveByPlate(cleanPlate)
		return &ANPRProcessResult{
			Session: active,
			Message: "Takroriy signal e'tiborga olinmadi (anti-duplicate)",
		}, nil
	}
	s.recentEvents[debounceKey] = eventTime
	s.cacheMu.Unlock()

	// 2. Save snapshot if provided
	snapshotPath := req.SnapshotURL
	if req.SnapshotBase64 != "" {
		savedPath, err := s.saveSnapshot(cleanPlate, req.SnapshotBase64)
		if err == nil {
			snapshotPath = savedPath
		}
	}

	// 3. Find Client (VIP / discounts)
	client, _ := s.repos.Client.GetByPlate(cleanPlate)

	// Get Branch
	branch, err := s.repos.Branch.GetFirst()
	if err != nil {
		return nil, fmt.Errorf("filial topilmadi: %w", err)
	}

	// 4. Handle ENTRY
	if req.Direction == domain.DirectionEntry {
		return s.handleEntry(cleanPlate, branch, client, req, snapshotPath, eventTime)
	}

	// 5. Handle EXIT
	return s.handleExit(cleanPlate, branch, client, req, snapshotPath, eventTime)
}

func (s *anprService) handleEntry(
	plate string,
	branch *domain.Branch,
	client *domain.Client,
	req *ANPREventRequest,
	snapshotPath string,
	entryTime time.Time,
) (*ANPRProcessResult, error) {
	// Check if already active
	active, _ := s.repos.ParkingSession.FindActiveByPlate(plate)
	if active != nil {
		// Vehicle already registered inside
		log.Printf("[ANPR] Warning: Vehicle %s is already inside parking (Session: %s)\n", plate, active.SessionCode)
		// Open barrier anyway if operator policy allows or return active
		_ = s.barrierSvc.OpenByDirection(domain.DirectionEntry, "Qayta kirish: "+plate)
		return &ANPRProcessResult{
			Session:       active,
			BarrierOpened: true,
			Message:       fmt.Sprintf("Avtomobil allaqachon ichkarida (Sessiya: %s)", active.SessionCode),
		}, nil
	}

	// Get default tariff
	tariff, err := s.repos.Tariff.GetDefault()
	if err != nil {
		return nil, fmt.Errorf("tarif topilmadi: %w", err)
	}

	var clientID *uuid.UUID
	if client != nil {
		clientID = &client.ID
	}

	sessionCode := generateSessionCode()
	session := &domain.ParkingSession{
		ID:            uuid.New(),
		SessionCode:   sessionCode,
		BranchID:      branch.ID,
		PlateNumber:   plate,
		EntryTime:     entryTime,
		EntryCameraID: req.CameraID,
		EntrySnapshot: snapshotPath,
		TariffID:      tariff.ID,
		ClientID:      clientID,
		Status:        domain.SessionActive,
		CreatedAt:     entryTime,
		UpdatedAt:     entryTime,
	}

	if err := s.repos.ParkingSession.Create(session); err != nil {
		return nil, fmt.Errorf("sessiyani saqlashda xatolik: %w", err)
	}

	// Increment occupancy
	activeCount, _ := s.repos.ParkingSession.CountActive()
	_ = s.repos.Branch.UpdateOccupiedSpaces(branch.ID, int(activeCount))

	// Open ENTRY barrier
	_ = s.barrierSvc.OpenByDirection(domain.DirectionEntry, "Kirish ruxsat etildi: "+plate)

	// Broadcast WS
	session.Tariff = tariff
	session.Client = client
	s.hub.Broadcast(WSEventEntry, session)
	s.hub.Broadcast(WSEventOccupancyUpdate, map[string]interface{}{
		"occupied_spaces": activeCount,
		"total_spaces":    branch.TotalSpaces,
		"available_spaces": branch.TotalSpaces - int(activeCount),
	})

	log.Printf("[ANPR] ENTRY processed successfully for plate %s (Code: %s)\n", plate, sessionCode)

	return &ANPRProcessResult{
		Session:       session,
		BarrierOpened: true,
		Message:       "Avtomobil muvaffaqiyatli kiritildi. Shlagbaum ochildi.",
	}, nil
}

func (s *anprService) handleExit(
	plate string,
	branch *domain.Branch,
	client *domain.Client,
	req *ANPREventRequest,
	snapshotPath string,
	exitTime time.Time,
) (*ANPRProcessResult, error) {
	// Find active session
	session, err := s.repos.ParkingSession.FindActiveByPlate(plate)
	if err != nil || session == nil {
		log.Printf("[ANPR] Warning: Exit detected for plate %s, but no active entry session found!\n", plate)
		// Return warning to operator
		return &ANPRProcessResult{
			BarrierOpened:   false,
			RequiresPayment: true,
			Message:         fmt.Sprintf("Avtomobilning (%s) kirish sessiyasi topilmadi!", plate),
		}, nil
	}

	// Calculate duration
	durationMin := int(exitTime.Sub(session.EntryTime).Minutes())
	if durationMin < 1 {
		durationMin = 1
	}

	// Calculate fee
	calcResult := s.tariffSvc.CalculateFee(session.Tariff, client, durationMin)

	session.ExitTime = &exitTime
	session.ExitCameraID = req.CameraID
	session.ExitSnapshot = snapshotPath
	session.DurationMin = durationMin
	session.CalculatedFee = calcResult.CalculatedFee
	session.DiscountAmount = calcResult.DiscountAmount
	session.FinalFee = calcResult.FinalFee
	session.UpdatedAt = exitTime

	// Case 1: Free of charge (Free minutes or VIP 100% discount)
	if calcResult.FinalFee == 0 {
		session.Status = domain.SessionCompleted
		if err := s.repos.ParkingSession.Update(session); err != nil {
			return nil, err
		}

		// Decrement occupancy
		activeCount, _ := s.repos.ParkingSession.CountActive()
		_ = s.repos.Branch.UpdateOccupiedSpaces(branch.ID, int(activeCount))

		// Open EXIT barrier
		_ = s.barrierSvc.OpenByDirection(domain.DirectionExit, fmt.Sprintf("Chiqish ruxsat etildi (%s): %s", calcResult.Description, plate))

		s.hub.Broadcast(WSEventExit, session)
		s.hub.Broadcast(WSEventOccupancyUpdate, map[string]interface{}{
			"occupied_spaces": activeCount,
			"total_spaces":    branch.TotalSpaces,
			"available_spaces": branch.TotalSpaces - int(activeCount),
		})

		log.Printf("[ANPR] EXIT FREE: %s (Duration: %d min)\n", plate, durationMin)

		return &ANPRProcessResult{
			Session:       session,
			BarrierOpened: true,
			Message:       fmt.Sprintf("Chiqish tasdiqlandi. %s. Shlagbaum ochildi.", calcResult.Description),
		}, nil
	}

	// Case 2: Payment required
	if err := s.repos.ParkingSession.Update(session); err != nil {
		return nil, err
	}

	s.hub.Broadcast(WSEventExit, session)

	log.Printf("[ANPR] EXIT PAYMENT REQUIRED: %s (Amount due: %.0f UZS)\n", plate, calcResult.FinalFee)

	return &ANPRProcessResult{
		Session:         session,
		BarrierOpened:   false,
		RequiresPayment: true,
		AmountDue:       calcResult.FinalFee,
		Message:         fmt.Sprintf("To'lov talab qilinadi: %.0f so'm (%s)", calcResult.FinalFee, calcResult.Description),
	}, nil
}

func (s *anprService) saveSnapshot(plate, base64Data string) (string, error) {
	// Strip prefix if exists
	parts := strings.Split(base64Data, ",")
	rawData := parts[len(parts)-1]

	decoded, err := hex.DecodeString(rawData)
	if err != nil {
		// Try standard base64 if not hex
		return "", err
	}

	filename := fmt.Sprintf("%s_%d.jpg", plate, time.Now().UnixNano())
	fullPath := filepath.Join(s.uploadDir, filename)
	if err := os.WriteFile(fullPath, decoded, 0644); err != nil {
		return "", err
	}

	return "/uploads/" + filename, nil
}

func NormalizePlate(p string) string {
	p = strings.ToUpper(strings.TrimSpace(p))
	p = strings.ReplaceAll(p, " ", "")
	p = strings.ReplaceAll(p, "-", "")
	p = strings.ReplaceAll(p, "_", "")
	return p
}

func generateSessionCode() string {
	dateStr := time.Now().Format("20060102")
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	randHex := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("PK-%s-%s", dateStr, randHex)
}
