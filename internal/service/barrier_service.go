package service

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type BarrierService interface {
	OpenBarrier(barrierID uuid.UUID, reason string) error
	CloseBarrier(barrierID uuid.UUID) error
	CarPassed(barrierID uuid.UUID) error
	OpenByDirection(direction domain.DirectionType, reason string) error
	CloseByDirection(direction domain.DirectionType) error
	ListBarriers() ([]domain.Barrier, error)
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
