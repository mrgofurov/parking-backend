package service

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/hikvision"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type CreateBarrierRequest struct {
	Name           string                    `json:"name"`
	BranchID       *uuid.UUID                `json:"branch_id"`
	Direction      domain.DirectionType      `json:"direction"`
	ControlType    domain.BarrierControlType `json:"control_type"`
	RelayURL       string                    `json:"relay_url"`
	DeviceHost     string                    `json:"device_host"`
	DeviceUsername string                    `json:"device_username"`
	DevicePassword string                    `json:"device_password"`
	OutputID       int                       `json:"output_id"`
	CloseOutputID  int                       `json:"close_output_id"`
	CloseURL       string                    `json:"close_url"`
	AutoCloseSec   int                       `json:"auto_close_sec"`
}

// CloseResult tells the operator whether a physical close command was sent.
type CloseResult struct {
	HardwareSent bool   `json:"hardware_sent"`
	Message      string `json:"message"`
}

// UpdateBarrierRequest: an empty DevicePassword keeps the stored one.
type UpdateBarrierRequest = CreateBarrierRequest

type BarrierService interface {
	OpenBarrier(barrierID uuid.UUID, reason string) error
	// CloseBarrier: operator / sensor close — sends the close command to the
	// device when one is configured, then marks the barrier CLOSED.
	CloseBarrier(barrierID uuid.UUID, reason string) (*CloseResult, error)
	CarPassed(barrierID uuid.UUID) (*CloseResult, error)
	// ResolveLaneBarrier finds the barrier for a lane: the explicitly bound
	// barrier if any, otherwise the branch's barrier with that direction.
	ResolveLaneBarrier(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID) (*domain.Barrier, error)
	OpenForLane(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID, reason string) error
	ListBarriers() ([]domain.Barrier, error)
	GetByID(id uuid.UUID) (*domain.Barrier, error)
	CreateBarrier(req CreateBarrierRequest) (*domain.Barrier, error)
	UpdateBarrier(id uuid.UUID, req UpdateBarrierRequest) (*domain.Barrier, error)
	DeleteBarrier(id uuid.UUID) error
	PingBarrier(id uuid.UUID) (map[string]interface{}, error)
}

type barrierService struct {
	repo       repository.BarrierRepository
	hub        *WebSocketHub
	timers     map[uuid.UUID]*time.Timer
	timersLock sync.Mutex
}

func NewBarrierService(repo repository.BarrierRepository, hub *WebSocketHub) BarrierService {
	return &barrierService{
		repo:   repo,
		hub:    hub,
		timers: make(map[uuid.UUID]*time.Timer),
	}
}

// triggerHardware sends the physical "open" command. It is synchronous so the
// caller knows whether the barrier really received the command.
func (s *barrierService) triggerHardware(b *domain.Barrier) error {
	switch b.ControlType {
	case domain.BarrierControlNone:
		return nil
	case domain.BarrierControlHikvisionIO, domain.BarrierControlHikvisionGate:
		if b.DeviceHost == "" {
			return errors.New("qurilma manzili (device_host) kiritilmagan")
		}
		cli := hikvision.NewClient(b.DeviceHost, b.DeviceUsername, b.DevicePassword)
		var err error
		for attempt := 1; attempt <= 2; attempt++ {
			if b.ControlType == domain.BarrierControlHikvisionGate {
				err = cli.OpenBarrierGate(b.OutputID)
			} else {
				err = cli.TriggerOutput(b.OutputID)
			}
			if err == nil {
				return nil
			}
			log.Printf("[Barrier] %s: ISAPI urinish %d muvaffaqiyatsiz: %v\n", b.Name, attempt, err)
		}
		return err
	default: // HTTP_RELAY
		if b.RelayURL == "" {
			return errors.New("relay URL kiritilmagan")
		}
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Post(b.RelayURL, "application/json", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("relay HTTP %d qaytardi", resp.StatusCode)
		}
		return nil
	}
}

func (s *barrierService) OpenBarrier(barrierID uuid.UUID, reason string) error {
	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return err
	}

	if hwErr := s.triggerHardware(barrier); hwErr != nil {
		log.Printf("[Barrier] %s (%s) OCHILMADI: %v. Sabab: %s\n", barrier.Name, barrier.Direction, hwErr, reason)
		s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
			"barrier_id":     barrierID,
			"branch_id":      barrier.BranchID,
			"name":           barrier.Name,
			"direction":      barrier.Direction,
			"status":         barrier.Status,
			"reason":         reason,
			"hardware_error": hwErr.Error(),
		})
		return fmt.Errorf("shlagbaum ochilmadi: %w", hwErr)
	}

	if err := s.repo.UpdateStatus(barrierID, domain.BarrierOpen); err != nil {
		return err
	}

	barrier.Status = domain.BarrierOpen
	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
		"branch_id":  barrier.BranchID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     domain.BarrierOpen,
		"reason":     reason,
	})

	// Cancel existing timer if any
	s.timersLock.Lock()
	if t, exists := s.timers[barrierID]; exists {
		t.Stop()
	}

	// Auto-close: the physical barrier closes itself (loop detector / timer);
	// this only resets the software status.
	autoSec := barrier.AutoCloseSec
	if autoSec <= 0 {
		autoSec = 10
	}
	s.timers[barrierID] = time.AfterFunc(time.Duration(autoSec)*time.Second, func() {
		_ = s.markClosed(barrierID, "auto_close")
	})
	s.timersLock.Unlock()

	log.Printf("[Barrier] Barrier %s (%s) OPENED. Reason: %s. Auto-close in %d s\n", barrier.Name, barrier.Direction, reason, autoSec)
	return nil
}

// triggerHardwareClose sends the physical close command if the barrier has
// one configured. sent=false means the barrier closes by itself.
func (s *barrierService) triggerHardwareClose(b *domain.Barrier) (bool, error) {
	switch b.ControlType {
	case domain.BarrierControlHikvisionGate:
		if b.DeviceHost == "" {
			return false, errors.New("qurilma manzili (device_host) kiritilmagan")
		}
		return true, hikvision.NewClient(b.DeviceHost, b.DeviceUsername, b.DevicePassword).CloseBarrierGate(b.OutputID)
	case domain.BarrierControlHikvisionIO:
		if b.CloseOutputID <= 0 {
			return false, nil
		}
		if b.DeviceHost == "" {
			return false, errors.New("qurilma manzili (device_host) kiritilmagan")
		}
		return true, hikvision.NewClient(b.DeviceHost, b.DeviceUsername, b.DevicePassword).TriggerOutput(b.CloseOutputID)
	case domain.BarrierControlHTTPRelay:
		if b.CloseURL == "" {
			return false, nil
		}
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Post(b.CloseURL, "application/json", nil)
		if err != nil {
			return true, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return true, fmt.Errorf("relay HTTP %d qaytardi", resp.StatusCode)
		}
		return true, nil
	}
	return false, nil
}

func (s *barrierService) CloseBarrier(barrierID uuid.UUID, reason string) (*CloseResult, error) {
	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return nil, err
	}
	sent, hwErr := s.triggerHardwareClose(barrier)
	if hwErr != nil {
		log.Printf("[Barrier] %s YOPILMADI: %v\n", barrier.Name, hwErr)
		s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
			"barrier_id":     barrierID,
			"branch_id":      barrier.BranchID,
			"name":           barrier.Name,
			"direction":      barrier.Direction,
			"status":         barrier.Status,
			"reason":         reason,
			"hardware_error": hwErr.Error(),
		})
		return nil, fmt.Errorf("shlagbaum yopilmadi: %w", hwErr)
	}
	if err := s.markClosed(barrierID, reason); err != nil {
		return nil, err
	}
	res := &CloseResult{HardwareSent: sent, Message: "Yopish buyrug'i shlagbaumga yuborildi"}
	if !sent {
		res.Message = "Tizimda yopildi (shlagbaum o'zi yopiladi — yopish buyrug'i sozlanmagan)"
	}
	return res, nil
}

// markClosed updates the status only (auto-close timer, after a hardware close).
func (s *barrierService) markClosed(barrierID uuid.UUID, reason string) error {
	s.timersLock.Lock()
	if t, exists := s.timers[barrierID]; exists {
		t.Stop()
		delete(s.timers, barrierID)
	}
	s.timersLock.Unlock()

	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(barrierID, domain.BarrierClosed); err != nil {
		return err
	}

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
		"branch_id":  barrier.BranchID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     domain.BarrierClosed,
		"reason":     reason,
	})

	log.Printf("[Barrier] Barrier %s (%s) CLOSED (%s).\n", barrier.Name, barrier.Direction, reason)
	return nil
}

func (s *barrierService) CarPassed(barrierID uuid.UUID) (*CloseResult, error) {
	log.Printf("[Barrier] Car passed barrier %s — closing.\n", barrierID)
	return s.CloseBarrier(barrierID, "Mashina o'tdi")
}

func (s *barrierService) ResolveLaneBarrier(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID) (*domain.Barrier, error) {
	if barrierID != nil && *barrierID != uuid.Nil {
		if b, err := s.repo.GetByID(*barrierID); err == nil {
			return b, nil
		}
	}
	if branchID != uuid.Nil {
		if b, err := s.repo.GetByBranchDirection(branchID, direction); err == nil {
			return b, nil
		}
	}
	b, err := s.repo.GetByDirection(direction)
	if err != nil {
		return nil, fmt.Errorf("%s yo'nalishi uchun shlagbaum topilmadi", direction)
	}
	return b, nil
}

func (s *barrierService) OpenForLane(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID, reason string) error {
	b, err := s.ResolveLaneBarrier(branchID, direction, barrierID)
	if err != nil {
		log.Printf("[Barrier] %v\n", err)
		return err
	}
	return s.OpenBarrier(b.ID, reason)
}

func (s *barrierService) ListBarriers() ([]domain.Barrier, error) {
	return s.repo.List()
}

func (s *barrierService) GetByID(id uuid.UUID) (*domain.Barrier, error) {
	return s.repo.GetByID(id)
}

func normalizeControlType(t domain.BarrierControlType) domain.BarrierControlType {
	switch t {
	case domain.BarrierControlHikvisionIO, domain.BarrierControlHikvisionGate, domain.BarrierControlNone, domain.BarrierControlHTTPRelay:
		return t
	}
	return domain.BarrierControlHTTPRelay
}

func (s *barrierService) CreateBarrier(req CreateBarrierRequest) (*domain.Barrier, error) {
	if req.Name == "" {
		return nil, errors.New("shlagbaum nomi kiritilishi shart")
	}
	if req.Direction != domain.DirectionEntry && req.Direction != domain.DirectionExit {
		req.Direction = domain.DirectionEntry
	}
	if req.AutoCloseSec <= 0 {
		req.AutoCloseSec = 10
	}
	if req.OutputID <= 0 {
		req.OutputID = 1
	}

	barrier := &domain.Barrier{
		ID:             uuid.New(),
		Name:           req.Name,
		Direction:      req.Direction,
		Status:         domain.BarrierClosed,
		ControlType:    normalizeControlType(req.ControlType),
		RelayURL:       req.RelayURL,
		DeviceHost:     req.DeviceHost,
		DeviceUsername: req.DeviceUsername,
		DevicePassword: req.DevicePassword,
		OutputID:       req.OutputID,
		CloseOutputID:  req.CloseOutputID,
		CloseURL:       req.CloseURL,
		AutoCloseSec:   req.AutoCloseSec,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if req.BranchID != nil {
		barrier.BranchID = *req.BranchID
	}

	if err := s.repo.Create(barrier); err != nil {
		return nil, err
	}
	barrier.HasDevicePassword = barrier.DevicePassword != ""

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrier.ID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     barrier.Status,
		"action":     "created",
	})

	return barrier, nil
}

func (s *barrierService) UpdateBarrier(id uuid.UUID, req UpdateBarrierRequest) (*domain.Barrier, error) {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		barrier.Name = req.Name
	}
	if req.Direction == domain.DirectionEntry || req.Direction == domain.DirectionExit {
		barrier.Direction = req.Direction
	}
	if req.BranchID != nil {
		barrier.BranchID = *req.BranchID
	}
	if req.ControlType != "" {
		barrier.ControlType = normalizeControlType(req.ControlType)
	}
	barrier.RelayURL = req.RelayURL
	barrier.DeviceHost = req.DeviceHost
	barrier.DeviceUsername = req.DeviceUsername
	if req.DevicePassword != "" {
		barrier.DevicePassword = req.DevicePassword
	}
	if req.OutputID > 0 {
		barrier.OutputID = req.OutputID
	}
	if req.CloseOutputID >= 0 {
		barrier.CloseOutputID = req.CloseOutputID
	}
	barrier.CloseURL = req.CloseURL
	if req.AutoCloseSec > 0 {
		barrier.AutoCloseSec = req.AutoCloseSec
	}
	barrier.UpdatedAt = time.Now()

	if err := s.repo.Update(barrier); err != nil {
		return nil, err
	}
	barrier.HasDevicePassword = barrier.DevicePassword != ""

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrier.ID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     barrier.Status,
		"action":     "updated",
	})

	return barrier, nil
}

func (s *barrierService) DeleteBarrier(id uuid.UUID) error {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": id,
		"name":       barrier.Name,
		"action":     "deleted",
	})

	return nil
}

func (s *barrierService) PingBarrier(id uuid.UUID) (map[string]interface{}, error) {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if barrier.ControlType == domain.BarrierControlNone {
		return map[string]interface{}{
			"status":   "SOFTWARE",
			"ping_ms":  0,
			"is_alive": true,
			"message":  "Dasturiy rejim: qurilma ulanmagan",
		}, nil
	}

	if barrier.ControlType == domain.BarrierControlHikvisionIO || barrier.ControlType == domain.BarrierControlHikvisionGate {
		if barrier.DeviceHost == "" {
			return map[string]interface{}{"status": "NO_URL", "ping_ms": 0, "is_alive": false,
				"message": "Qurilma manzili (device_host) kiritilmagan"}, nil
		}
		start := time.Now()
		_, err := hikvision.NewClient(barrier.DeviceHost, barrier.DeviceUsername, barrier.DevicePassword).DeviceInfo()
		ms := time.Since(start).Milliseconds()
		if err != nil {
			return map[string]interface{}{"status": "OFFLINE", "ping_ms": ms, "is_alive": false,
				"message": "Hikvision qurilmasiga ulanib bo'lmadi: " + err.Error()}, nil
		}
		return map[string]interface{}{"status": "ONLINE", "ping_ms": ms, "is_alive": true,
			"message": fmt.Sprintf("Hikvision ISAPI aloqasi va login to'g'ri (%d ms)", ms)}, nil
	}

	if barrier.RelayURL == "" {
		return map[string]interface{}{
			"status":   "NO_URL",
			"ping_ms":  0,
			"is_alive": false,
			"message":  "Hardware Relay URL kiritilmagan",
		}, nil
	}

	start := time.Now()

	// 1. Try HTTP probe if it's an HTTP URL
	client := http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequest("HEAD", barrier.RelayURL, nil)
	var resp *http.Response
	if err == nil {
		resp, err = client.Do(req)
	}

	latency := time.Since(start).Milliseconds()
	if latency == 0 {
		latency = 1
	}

	if err == nil && resp != nil {
		defer resp.Body.Close()
		return map[string]interface{}{
			"status":      "ONLINE",
			"ping_ms":     latency,
			"relay_url":   barrier.RelayURL,
			"status_code": resp.StatusCode,
			"is_alive":    true,
			"message":     fmt.Sprintf("Relay aloqasi barqaror (%d ms)", latency),
		}, nil
	}

	// 2. Fallback to TCP Socket probe
	target := barrier.RelayURL
	if strings.HasPrefix(target, "http://") {
		target = strings.TrimPrefix(target, "http://")
	} else if strings.HasPrefix(target, "https://") {
		target = strings.TrimPrefix(target, "https://")
	}
	if idx := strings.Index(target, "/"); idx != -1 {
		target = target[:idx]
	}
	if !strings.Contains(target, ":") {
		target += ":80"
	}

	tcpStart := time.Now()
	conn, tcpErr := net.DialTimeout("tcp", target, 1200*time.Millisecond)
	if tcpErr == nil {
		conn.Close()
		tcpLatency := time.Since(tcpStart).Milliseconds()
		if tcpLatency == 0 {
			tcpLatency = 1
		}
		return map[string]interface{}{
			"status":    "ONLINE",
			"ping_ms":   tcpLatency,
			"relay_url": barrier.RelayURL,
			"is_alive":  true,
			"message":   fmt.Sprintf("Relay porti ochiq (%d ms)", tcpLatency),
		}, nil
	}

	return map[string]interface{}{
		"status":    "OFFLINE",
		"ping_ms":   latency,
		"relay_url": barrier.RelayURL,
		"is_alive":  false,
		"message":   "Relay kontrolleriga ulanib bo'lmadi (Aloqa yo'q)",
	}, nil
}
