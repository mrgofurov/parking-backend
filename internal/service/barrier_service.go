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
	"github.com/smart-parking/parking-backend/internal/repository"
)

type CreateBarrierRequest struct {
	Name         string               `json:"name"`
	Direction    domain.DirectionType `json:"direction"`
	RelayURL     string               `json:"relay_url"`
	AutoCloseSec int                  `json:"auto_close_sec"`
}

type UpdateBarrierRequest struct {
	Name         string               `json:"name"`
	Direction    domain.DirectionType `json:"direction"`
	RelayURL     string               `json:"relay_url"`
	AutoCloseSec int                  `json:"auto_close_sec"`
}

type BarrierService interface {
	OpenBarrier(barrierID uuid.UUID, reason string) error
	CloseBarrier(barrierID uuid.UUID) error
	CarPassed(barrierID uuid.UUID) error
	OpenByDirection(direction domain.DirectionType, reason string) error
	CloseByDirection(direction domain.DirectionType) error
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

func (s *barrierService) OpenBarrier(barrierID uuid.UUID, reason string) error {
	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return err
	}

	// Trigger hardware relay if configured
	if barrier.RelayURL != "" {
		go func(url string) {
			client := http.Client{Timeout: 2 * time.Second}
			resp, err := client.Post(url, "application/json", nil)
			if err != nil {
				log.Printf("[Barrier] Warning: hardware relay trigger failed for %s: %v\n", url, err)
				return
			}
			defer resp.Body.Close()
		}(barrier.RelayURL)
	}

	if err := s.repo.UpdateStatus(barrierID, domain.BarrierOpen); err != nil {
		return err
	}

	barrier.Status = domain.BarrierOpen
	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
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

	// Auto-close safety timer
	autoSec := barrier.AutoCloseSec
	if autoSec <= 0 {
		autoSec = 10
	}
	s.timers[barrierID] = time.AfterFunc(time.Duration(autoSec)*time.Second, func() {
		_ = s.CloseBarrier(barrierID)
	})
	s.timersLock.Unlock()

	log.Printf("[Barrier] Barrier %s (%s) OPENED. Reason: %s. Auto-close in %d s\n", barrier.Name, barrier.Direction, reason, autoSec)
	return nil
}

func (s *barrierService) CloseBarrier(barrierID uuid.UUID) error {
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

	barrier.Status = domain.BarrierClosed
	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     domain.BarrierClosed,
		"reason":     "auto_or_sensor_close",
	})

	log.Printf("[Barrier] Barrier %s (%s) CLOSED.\n", barrier.Name, barrier.Direction)
	return nil
}

func (s *barrierService) CarPassed(barrierID uuid.UUID) error {
	log.Printf("[Barrier] Sensor detected: Car passed barrier %s. Immediate close triggered.\n", barrierID)
	return s.CloseBarrier(barrierID)
}

func (s *barrierService) OpenByDirection(direction domain.DirectionType, reason string) error {
	barrier, err := s.repo.GetByDirection(direction)
	if err != nil {
		return err
	}
	return s.OpenBarrier(barrier.ID, reason)
}

func (s *barrierService) CloseByDirection(direction domain.DirectionType) error {
	barrier, err := s.repo.GetByDirection(direction)
	if err != nil {
		return err
	}
	return s.CloseBarrier(barrier.ID)
}

func (s *barrierService) ListBarriers() ([]domain.Barrier, error) {
	return s.repo.List()
}

func (s *barrierService) GetByID(id uuid.UUID) (*domain.Barrier, error) {
	return s.repo.GetByID(id)
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

	barrier := &domain.Barrier{
		ID:           uuid.New(),
		Name:         req.Name,
		Direction:    req.Direction,
		Status:       domain.BarrierClosed,
		RelayURL:     req.RelayURL,
		AutoCloseSec: req.AutoCloseSec,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.repo.Create(barrier); err != nil {
		return nil, err
	}

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
	barrier.RelayURL = req.RelayURL
	if req.AutoCloseSec > 0 {
		barrier.AutoCloseSec = req.AutoCloseSec
	}
	barrier.UpdatedAt = time.Now()

	if err := s.repo.Update(barrier); err != nil {
		return nil, err
	}

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
