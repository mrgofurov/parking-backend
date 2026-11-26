package service

import (
	"crypto/rand"
	"encoding/base64"
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
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

// ANPREventRequest is a plate read from any source: Hikvision ANPR camera,
// parking-capture-service, or the admin simulator.
type ANPREventRequest struct {
	PlateNumber    string               `json:"plate_number"`
	CameraID       string               `json:"camera_id,omitempty"` // camera UUID or code (e.g. "cam-entry")
	Direction      domain.DirectionType `json:"direction"`           // used only when the camera is unknown
	Confidence     float64              `json:"confidence"`          // 0-1 or 0-100
	Timestamp      *time.Time           `json:"timestamp,omitempty"`
	SnapshotBase64 string               `json:"snapshot_base64,omitempty"`
	SnapshotURL    string               `json:"snapshot_url,omitempty"`
	BranchID       *uuid.UUID           `json:"branch_id,omitempty"` // simulator: branch when no camera given
	Source         string               `json:"source,omitempty"`

	// Set by trusted callers (Hikvision handler) — never from JSON.
	Camera        *domain.Camera `json:"-"`
	SnapshotBytes []byte         `json:"-"`
}

type ANPRProcessResult struct {
	Session         *domain.ParkingSession `json:"session"`
	CameraID        *uuid.UUID             `json:"camera_id,omitempty"`
	Direction       domain.DirectionType   `json:"direction"`
	BarrierOpened   bool                   `json:"barrier_opened"`
	BarrierError    string                 `json:"barrier_error,omitempty"`
	RequiresPayment bool                   `json:"requires_payment"`
	AmountDue       float64                `json:"amount_due"`
	PaymentRequest  *domain.PaymentRequest `json:"payment_request,omitempty"`
	Ignored         bool                   `json:"ignored,omitempty"`
	Message         string                 `json:"message"`
}

type ANPRService interface {
	ProcessEvent(req *ANPREventRequest) (*ANPRProcessResult, error)
	ResolveCamera(idOrCode string) (*domain.Camera, error)
}

type anprService struct {
	cfg        *config.Config
	repos      *repository.RepositoryContainer
	tariffSvc  TariffService
	barrierSvc BarrierService
	qrSvc      QRPaymentService
	display    *DisplayService
	hub        *WebSocketHub
	uploadDir  string

	// Anti-duplicate debounce cache
	recentEvents map[string]time.Time
	cacheMu      sync.Mutex
	// one event at a time per plate so two cameras / retries can't race
	plateLocks sync.Map
}

func NewANPRService(
	cfg *config.Config,
	repos *repository.RepositoryContainer,
	tariffSvc TariffService,
	barrierSvc BarrierService,
	qrSvc QRPaymentService,
	display *DisplayService,
	hub *WebSocketHub,
) ANPRService {
	return &anprService{
		cfg:          cfg,
		repos:        repos,
		tariffSvc:    tariffSvc,
		barrierSvc:   barrierSvc,
		qrSvc:        qrSvc,
		display:      display,
		hub:          hub,
		uploadDir:    cfg.UploadDir,
		recentEvents: make(map[string]time.Time),
	}
}

const debounceWindow = 12 * time.Second

func (s *anprService) ResolveCamera(idOrCode string) (*domain.Camera, error) {
	idOrCode = strings.TrimSpace(idOrCode)
	if idOrCode == "" {
		return nil, errors.New("kamera ko'rsatilmagan")
	}
	if id, err := uuid.Parse(idOrCode); err == nil {
		return s.repos.Camera.GetByID(id)
	}
	return s.repos.Camera.GetByCode(idOrCode)
}

func normalizeConfidence(c float64) float64 {
	if c > 0 && c <= 1 {
		return c * 100
	}
	return c
}

// isDuplicate implements the debounce and prunes old keys.
func (s *anprService) isDuplicate(key string, now time.Time) bool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if last, ok := s.recentEvents[key]; ok && now.Sub(last) < debounceWindow && now.Sub(last) > -debounceWindow {
		return true
	}
	s.recentEvents[key] = now
	if len(s.recentEvents) > 500 {
		for k, t := range s.recentEvents {
			if now.Sub(t) > time.Minute {
				delete(s.recentEvents, k)
			}
		}
	}
	return false
}

func (s *anprService) lockPlate(plate string) func() {
	v, _ := s.plateLocks.LoadOrStore(plate, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (s *anprService) ProcessEvent(req *ANPREventRequest) (*ANPRProcessResult, error) {
	cleanPlate := NormalizePlate(req.PlateNumber)
	if len(cleanPlate) < 3 {
		return nil, errors.New("davlat raqami ko'rsatilmagan yoki juda qisqa")
	}

	// 1. Which lane? Camera decides direction and branch.
	cam := req.Camera
	if cam == nil && req.CameraID != "" {
		c, err := s.ResolveCamera(req.CameraID)
		if err != nil || c == nil {
			// Don't strand a car at the gate over a misconfigured code: fall
			// back to the direction the sender reported.
			log.Printf("[ANPR] OGOHLANTIRISH: kamera %q topilmadi — yo'nalish (%s) bo'yicha qayta ishlanadi. Admin panelda kamera kodini tekshiring.\n", req.CameraID, req.Direction)
		} else {
			cam = c
		}
	}

	direction := domain.DirectionType(strings.ToUpper(string(req.Direction)))
	var branch *domain.Branch
	var err error
	if cam != nil {
		direction = cam.Direction
		branch, err = s.repos.Branch.GetByID(cam.BranchID)
		_ = s.repos.Camera.UpdateHeartbeat(cam.ID)
	} else if req.BranchID != nil {
		branch, err = s.repos.Branch.GetByID(*req.BranchID)
	} else {
		branch, err = s.repos.Branch.GetFirst()
	}
	if err != nil || branch == nil {
		return nil, errors.New("filial topilmadi")
	}
	if direction != domain.DirectionEntry && direction != domain.DirectionExit {
		direction = domain.DirectionEntry
	}
	// Without an explicit camera, use the branch's first camera of that
	// direction so the lane display / barrier binding still work.
	if cam == nil {
		if cams, err := s.repos.Camera.ListByBranch(branch.ID); err == nil {
			for i := range cams {
				if cams[i].Direction == direction {
					cam = &cams[i]
					break
				}
			}
		}
	}

	var camID *uuid.UUID
	var barrierID *uuid.UUID
	if cam != nil {
		id := cam.ID
		camID = &id
		barrierID = cam.BarrierID
	}

	confidence := normalizeConfidence(req.Confidence)
	if cam != nil && cam.MinConfidence > 0 && confidence > 0 && confidence < cam.MinConfidence {
		log.Printf("[ANPR] %s (%s) ignored: confidence %.1f < %.1f\n", cleanPlate, direction, confidence, cam.MinConfidence)
		return &ANPRProcessResult{Ignored: true, CameraID: camID, Direction: direction,
			Message: fmt.Sprintf("Aniqlik past (%.0f%%), hodisa e'tiborga olinmadi", confidence)}, nil
	}

	eventTime := time.Now()
	if req.Timestamp != nil && !req.Timestamp.IsZero() && time.Since(*req.Timestamp) < 10*time.Minute && time.Until(*req.Timestamp) < time.Minute {
		eventTime = *req.Timestamp // trust device clocks only when sane
	}

	unlock := s.lockPlate(cleanPlate)
	defer unlock()

	// 2. Anti-duplicate (same plate + lane within the window)
	laneKey := string(direction)
	if camID != nil {
		laneKey = camID.String()
	}
	if s.isDuplicate(cleanPlate+":"+laneKey, time.Now()) {
		log.Printf("[ANPR] Duplicate event ignored for %s (%s)\n", cleanPlate, direction)
		active, _ := s.repos.ParkingSession.FindActiveByPlate(cleanPlate)
		return &ANPRProcessResult{Session: active, Ignored: true, CameraID: camID, Direction: direction,
			Message: "Takroriy signal e'tiborga olinmadi (anti-duplicate)"}, nil
	}

	// 3. Snapshot
	snapshotPath := req.SnapshotURL
	if len(req.SnapshotBytes) > 0 {
		if p, err := s.saveSnapshotBytes(cleanPlate, req.SnapshotBytes); err == nil {
			snapshotPath = p
		}
	} else if req.SnapshotBase64 != "" {
		if p, err := s.saveSnapshot(cleanPlate, req.SnapshotBase64); err == nil {
			snapshotPath = p
		}
	}

	client, _ := s.repos.Client.GetByPlate(cleanPlate)
	if client != nil && !client.Active {
		client = nil
	}

	lane := laneCtx{cam: cam, camID: camID, barrierID: barrierID, branch: branch, direction: direction}
	var res *ANPRProcessResult
	if direction == domain.DirectionEntry {
		res, err = s.handleEntry(cleanPlate, lane, client, snapshotPath, eventTime)
	} else {
		res, err = s.handleExit(cleanPlate, lane, client, snapshotPath, eventTime)
	}
	if res != nil {
		res.CameraID = camID
		res.Direction = direction
	}
	return res, err
}

type laneCtx struct {
	cam       *domain.Camera
	camID     *uuid.UUID
	barrierID *uuid.UUID
	branch    *domain.Branch
	direction domain.DirectionType
}

func (s *anprService) openLane(l laneCtx, reason string) (bool, string) {
	if err := s.barrierSvc.OpenForLane(l.branch.ID, l.direction, l.barrierID, reason); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (s *anprService) broadcastOccupancy(branch *domain.Branch) int64 {
	activeCount, _ := s.repos.ParkingSession.CountActiveByBranch(&branch.ID)
	_ = s.repos.Branch.UpdateOccupiedSpaces(branch.ID, int(activeCount))
	s.hub.Broadcast(WSEventOccupancyUpdate, map[string]interface{}{
		"branch_id":        branch.ID,
		"occupied_spaces":  activeCount,
		"total_spaces":     branch.TotalSpaces,
		"available_spaces": branch.TotalSpaces - int(activeCount),
	})
	return activeCount
}

func (s *anprService) handleEntry(
	plate string,
	l laneCtx,
	client *domain.Client,
	snapshotPath string,
	entryTime time.Time,
) (*ANPRProcessResult, error) {
	// Already inside according to the system (exit was missed): let it in and
	// keep the original session so the stay is still billed.
	active, _ := s.repos.ParkingSession.FindActiveByPlate(plate)
	if active != nil {
		log.Printf("[ANPR] Warning: Vehicle %s is already inside parking (Session: %s)\n", plate, active.SessionCode)
		opened, barrierErr := s.openLane(l, "Qayta kirish: "+plate)
		s.display.Set(l.cam, DisplayState{State: DisplayWelcome, PlateNumber: plate, EntryTime: &active.EntryTime,
			Message: "Xush kelibsiz!"})
		return &ANPRProcessResult{
			Session:       active,
			BarrierOpened: opened,
			BarrierError:  barrierErr,
			Message:       fmt.Sprintf("Avtomobil allaqachon ichkarida (Sessiya: %s)", active.SessionCode),
		}, nil
	}

	tariff, err := s.repos.Tariff.GetDefault()
	if err != nil {
		return nil, fmt.Errorf("tarif topilmadi: %w", err)
	}

	var clientID *uuid.UUID
	if client != nil {
		clientID = &client.ID
	}

	session := &domain.ParkingSession{
		ID:            uuid.New(),
		SessionCode:   generateSessionCode(),
		BranchID:      l.branch.ID,
		PlateNumber:   plate,
		EntryTime:     entryTime,
		EntryCameraID: l.camID,
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

	opened, barrierErr := s.openLane(l, "Kirish ruxsat etildi: "+plate)

	session.Tariff = tariff
	session.Client = client
	s.hub.Broadcast(WSEventEntry, session)
	s.broadcastOccupancy(l.branch)
	s.display.Set(l.cam, DisplayState{State: DisplayWelcome, PlateNumber: plate, EntryTime: &session.EntryTime,
		Message: "Xush kelibsiz!"})

	log.Printf("[ANPR] ENTRY %s (Code: %s, barrier opened: %v)\n", plate, session.SessionCode, opened)

	msg := "Avtomobil muvaffaqiyatli kiritildi. Shlagbaum ochildi."
	if !opened {
		msg = "Avtomobil kiritildi, lekin shlagbaum ochilmadi: " + barrierErr
	}
	return &ANPRProcessResult{
		Session:       session,
		BarrierOpened: opened,
		BarrierError:  barrierErr,
		Message:       msg,
	}, nil
}

func (s *anprService) handleExit(
	plate string,
	l laneCtx,
	client *domain.Client,
	snapshotPath string,
	exitTime time.Time,
) (*ANPRProcessResult, error) {
	session, err := s.repos.ParkingSession.FindActiveByPlate(plate)
	if err != nil || session == nil {
		// Paid moments ago (operator / earlier QR)? Let it out.
		grace := time.Duration(s.cfg.ExitGraceMinutes) * time.Minute
		if grace > 0 {
			if recent, err := s.repos.ParkingSession.FindRecentlyClosedByPlate(plate, time.Now().Add(-grace)); err == nil && recent != nil {
				opened, barrierErr := s.openLane(l, "Oldindan to'langan: "+plate)
				s.display.Set(l.cam, DisplayState{State: DisplayPaid, PlateNumber: plate, EntryTime: &recent.EntryTime,
					ExitTime: recent.ExitTime, DurationMin: recent.DurationMin, Amount: recent.FinalFee,
					Message: "To'lov qilingan. Yo'lingiz ochiq!"})
				return &ANPRProcessResult{Session: recent, BarrierOpened: opened, BarrierError: barrierErr,
					Message: "Sessiya oldindan to'langan, shlagbaum ochildi"}, nil
			}
		}

		log.Printf("[ANPR] Warning: Exit detected for plate %s, but no active entry session found!\n", plate)
		s.display.Set(l.cam, DisplayState{State: DisplayNoSession, PlateNumber: plate,
			Message: "Kirish qayd etilmagan. Iltimos, operatorga murojaat qiling."})
		s.hub.Broadcast(WSEventExitUnknown, map[string]interface{}{
			"plate_number": plate,
			"branch_id":    l.branch.ID,
			"camera_id":    l.camID,
			"snapshot":     snapshotPath,
			"time":         exitTime,
		})
		return &ANPRProcessResult{
			RequiresPayment: true,
			Message:         fmt.Sprintf("Avtomobilning (%s) kirish sessiyasi topilmadi!", plate),
		}, nil
	}

	durationMin := int(exitTime.Sub(session.EntryTime).Minutes())
	if durationMin < 1 {
		durationMin = 1
	}
	if client == nil {
		client = session.Client
	}
	calc := s.tariffSvc.CalculateFee(session.Tariff, client, durationMin)

	// A QR already shown to this car keeps its amount (driver may have scanned it).
	var pending *domain.PaymentRequest
	if pr, err := s.repos.PaymentRequest.GetPendingBySession(session.ID); err == nil && pr != nil && time.Now().Before(pr.ExpiresAt) {
		pending = pr
	}

	session.ExitTime = &exitTime
	session.ExitCameraID = l.camID
	session.ExitSnapshot = snapshotPath
	session.DurationMin = durationMin
	session.CalculatedFee = calc.CalculatedFee
	session.DiscountAmount = calc.DiscountAmount
	session.FinalFee = calc.FinalFee
	if pending != nil {
		session.FinalFee = pending.Amount
	}
	session.UpdatedAt = time.Now()
	session.Payments = nil

	// Case 1: nothing to pay
	if session.FinalFee == 0 {
		session.Status = domain.SessionCompleted
		if err := s.repos.ParkingSession.Update(session); err != nil {
			return nil, err
		}
		opened, barrierErr := s.openLane(l, fmt.Sprintf("Chiqish ruxsat etildi (%s): %s", calc.Description, plate))
		s.hub.Broadcast(WSEventExit, session)
		s.broadcastOccupancy(l.branch)
		s.display.Set(l.cam, DisplayState{State: DisplayFree, PlateNumber: plate, EntryTime: &session.EntryTime,
			ExitTime: session.ExitTime, DurationMin: durationMin, CalculatedFee: calc.CalculatedFee,
			DiscountAmount: calc.DiscountAmount, Description: calc.Description, Message: "Oq yo'l!"})

		log.Printf("[ANPR] EXIT FREE: %s (Duration: %d min)\n", plate, durationMin)
		return &ANPRProcessResult{
			Session:       session,
			BarrierOpened: opened,
			BarrierError:  barrierErr,
			Message:       fmt.Sprintf("Chiqish tasdiqlandi. %s.", calc.Description),
		}, nil
	}

	// Case 2: payment required — barrier stays closed until paid.
	if err := s.repos.ParkingSession.Update(session); err != nil {
		return nil, err
	}
	s.hub.Broadcast(WSEventExit, session)

	res := &ANPRProcessResult{
		Session:         session,
		RequiresPayment: true,
		AmountDue:       session.FinalFee,
		Message:         fmt.Sprintf("To'lov talab qilinadi: %.0f so'm (%s)", session.FinalFee, calc.Description),
	}

	disp := DisplayState{
		State:          DisplayPaymentRequired,
		PlateNumber:    plate,
		EntryTime:      &session.EntryTime,
		ExitTime:       session.ExitTime,
		DurationMin:    durationMin,
		CalculatedFee:  calc.CalculatedFee,
		DiscountAmount: calc.DiscountAmount,
		Amount:         session.FinalFee,
		Description:    calc.Description,
		Message:        "QR kodni skanerlab to'lang",
	}

	pr, qrErr := s.qrSvc.RequestForSession(session, l.camID)
	switch {
	case qrErr == nil && pr != nil && pr.Status == domain.PaymentRequestPaid:
		// Paid between reads (expired QR paid at the last second) — completion already opened the barrier.
		res.RequiresPayment = false
		res.Message = "To'lov allaqachon qabul qilingan"
		return res, nil
	case qrErr == nil && pr != nil:
		res.PaymentRequest = pr
		disp.QRPayload = pr.QRPayload
		disp.PaymentRequestID = &pr.ID
		disp.ExpiresAt = &pr.ExpiresAt
		disp.Amount = pr.Amount
	case errors.Is(qrErr, ErrQRDisabled):
		disp.Message = "Iltimos, to'lovni kassada amalga oshiring"
	default:
		log.Printf("[ANPR] %s: QR yaratilmadi: %v\n", plate, qrErr)
		disp.Message = "QR vaqtincha mavjud emas. To'lovni kassada amalga oshiring"
	}
	s.display.Set(l.cam, disp)

	log.Printf("[ANPR] EXIT PAYMENT REQUIRED: %s (Amount due: %.0f UZS)\n", plate, session.FinalFee)
	return res, nil
}

func (s *anprService) saveSnapshotBytes(plate string, data []byte) (string, error) {
	filename := fmt.Sprintf("%s_%d.jpg", plate, time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(s.uploadDir, filename), data, 0644); err != nil {
		return "", err
	}
	return "/uploads/" + filename, nil
}

func (s *anprService) saveSnapshot(plate, base64Data string) (string, error) {
	// Strip prefix if exists
	parts := strings.Split(base64Data, ",")
	rawData := parts[len(parts)-1]

	decoded, err := base64.StdEncoding.DecodeString(rawData)
	if err != nil {
		// Try hex if not standard base64
		decoded, err = hex.DecodeString(rawData)
		if err != nil {
			return "", err
		}
	}

	filename := fmt.Sprintf("%s_%d.jpg", plate, time.Now().UnixNano())
	fullPath := filepath.Join(s.uploadDir, filename)
	if err := os.WriteFile(fullPath, decoded, 0644); err != nil {
		return "", err
	}

	return "/uploads/" + filename, nil
}

// NormalizePlate upper-cases and keeps only latin letters and digits
// ("01 A 777 AA", "01-A777AA|" → "01A777AA").
func NormalizePlate(p string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(p) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func generateSessionCode() string {
	dateStr := time.Now().Format("20060102")
	// 4 random bytes: 2 bytes collided with the unique index after a few
	// hundred sessions a day (birthday bound) and made the entry fail.
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	randHex := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("PK-%s-%s", dateStr, randHex)
}
