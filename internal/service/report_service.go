package service

import (
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type DashboardStats struct {
	VehiclesInside     int64                   `json:"vehicles_inside"`
	TodayRevenue       float64                 `json:"today_revenue"` // In UZS
	TotalSpaces        int                     `json:"total_spaces"`
	OccupiedSpaces     int                     `json:"occupied_spaces"`
	AvailableSpaces    int                     `json:"available_spaces"`
	TodayTotalVehicles int64                   `json:"today_total_vehicles"`
	AvgDurationMin     float64                 `json:"avg_duration_min"`
	RecentActivity     []domain.ParkingSession `json:"recent_activity"`
	BranchID           *uuid.UUID              `json:"branch_id,omitempty"`
}

type ReportService interface {
	GetDashboardStats(branchID *uuid.UUID) (*DashboardStats, error)
	GetRevenueTrends(branchID *uuid.UUID, days int) ([]map[string]interface{}, error)
}

type reportService struct {
	repos *repository.RepositoryContainer
}

func NewReportService(repos *repository.RepositoryContainer) ReportService {
	return &reportService{repos: repos}
}

func (s *reportService) GetDashboardStats(branchID *uuid.UUID) (*DashboardStats, error) {
	activeCount, _ := s.repos.ParkingSession.CountActiveByBranch(branchID)
	todayVehicles, _ := s.repos.ParkingSession.CountTodayByBranch(branchID)
	todayRevenue, _ := s.repos.Payment.GetTodayRevenueByBranch(branchID)
	avgDuration, _ := s.repos.ParkingSession.GetAverageDurationByBranch(branchID)

	var totalSpaces int
	var occupiedSpaces int

	if branchID != nil {
		// Tanlangan filial uchun
		branch, err := s.repos.Branch.GetByID(*branchID)
		if err == nil && branch != nil {
			totalSpaces = branch.TotalSpaces
			occupiedSpaces = branch.OccupiedSpaces
			if occupiedSpaces < int(activeCount) {
				occupiedSpaces = int(activeCount)
			}
		}
	} else {
		// Barcha filiallar uchun yig'indisi
		branches, _ := s.repos.Branch.List()
		for _, b := range branches {
			totalSpaces += b.TotalSpaces
			occupiedSpaces += b.OccupiedSpaces
		}
		if occupiedSpaces < int(activeCount) {
			occupiedSpaces = int(activeCount)
		}
	}

	if totalSpaces == 0 {
		totalSpaces = 100
	}

	availableSpaces := totalSpaces - occupiedSpaces
	if availableSpaces < 0 {
		availableSpaces = 0
	}

	recentSessions, _, _ := s.repos.ParkingSession.List(repository.SessionFilter{
		BranchID: branchID,
		Page:     1,
		Limit:    8,
	})

	return &DashboardStats{
		VehiclesInside:     activeCount,
		TodayRevenue:       todayRevenue,
		TotalSpaces:        totalSpaces,
		OccupiedSpaces:     occupiedSpaces,
		AvailableSpaces:    availableSpaces,
		TodayTotalVehicles: todayVehicles,
		AvgDurationMin:     avgDuration,
		RecentActivity:     recentSessions,
		BranchID:           branchID,
	}, nil
}

func (s *reportService) GetRevenueTrends(branchID *uuid.UUID, days int) ([]map[string]interface{}, error) {
	if days <= 0 {
		days = 7
	}
	return s.repos.Payment.GetRevenueByDaysByBranch(branchID, days)
}
