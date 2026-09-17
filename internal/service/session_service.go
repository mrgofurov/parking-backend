package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type SessionService interface {
	ListSessions(filter repository.SessionFilter) ([]domain.ParkingSession, int64, error)
	GetSessionByID(id uuid.UUID) (*domain.ParkingSession, error)
	ManualClose(sessionID uuid.UUID, reason string) error
	RecalculateFee(sessionID uuid.UUID) (*domain.ParkingSession, error)
	GetVehicleHistory(plate string) ([]domain.ParkingSession, error)
}

type sessionService struct {
	repos      *repository.RepositoryContainer
	tariffSvc  TariffService
	barrierSvc BarrierService
	hub        *WebSocketHub
}

func NewSessionService(
	repos *repository.RepositoryContainer,
	tariffSvc TariffService,
	barrierSvc BarrierService,
	hub *WebSocketHub,
) SessionService {
	return &sessionService{
		repos:      repos,
		tariffSvc:  tariffSvc,
		barrierSvc: barrierSvc,
		hub:        hub,
	}
}

func (s *sessionService) ListSessions(filter repository.SessionFilter) ([]domain.ParkingSession, int64, error) {
	return s.repos.ParkingSession.List(filter)
}

func (s *sessionService) GetSessionByID(id uuid.UUID) (*domain.ParkingSession, error) {
	return s.repos.ParkingSession.GetByID(id)
}

func (s *sessionService) ManualClose(sessionID uuid.UUID, reason string) error {
	session, err := s.repos.ParkingSession.GetByID(sessionID)
	if err != nil {
		return errors.New("sessiya topilmadi")
	}

	if session.Status == domain.SessionCompleted || session.Status == domain.SessionCancelled {
		return errors.New("sessiya allaqachon yopilgan")
	}

	now := time.Now()
	session.Status = domain.SessionCompleted
	if session.ExitTime == nil {
		session.ExitTime = &now
		session.DurationMin = int(now.Sub(session.EntryTime).Minutes())
	}
	session.Notes = session.Notes + " [Operator tomonidan qo'lda yopildi: " + reason + "]"
	session.UpdatedAt = now

	if err := s.repos.ParkingSession.Update(session); err != nil {
		return err
	}

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

	s.hub.Broadcast(WSEventExit, session)
	return nil
}

func (s *sessionService) RecalculateFee(sessionID uuid.UUID) (*domain.ParkingSession, error) {
	session, err := s.repos.ParkingSession.GetByID(sessionID)
	if err != nil {
		return nil, errors.New("sessiya topilmadi")
	}

	now := time.Now()
	durationMin := int(now.Sub(session.EntryTime).Minutes())
	if durationMin < 1 {
		durationMin = 1
	}

	calc := s.tariffSvc.CalculateFee(session.Tariff, session.Client, durationMin)
	session.DurationMin = durationMin
	session.CalculatedFee = calc.CalculatedFee
	session.DiscountAmount = calc.DiscountAmount
	session.FinalFee = calc.FinalFee

	return session, nil
}

func (s *sessionService) GetVehicleHistory(plate string) ([]domain.ParkingSession, error) {
	return s.repos.ParkingSession.GetVehicleHistory(plate)
}
