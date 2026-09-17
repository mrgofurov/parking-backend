package service

import (
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
}

type ReportService interface {
	GetDashboardStats() (*DashboardStats, error)
	GetRevenueTrends(days int) ([]map[string]interface{}, error)
}

type reportService struct {
	repos *repository.RepositoryContainer
}

func NewReportService(repos *repository.RepositoryContainer) ReportService {
	return &reportService{repos: repos}
}

func (s *reportService) GetDashboardStats() (*DashboardStats, error) {
	activeCount, _ := s.repos.ParkingSession.CountActive()
	todayVehicles, _ := s.repos.ParkingSession.CountToday()
	todayRevenue, _ := s.repos.Payment.GetTodayRevenue()
	avgDuration, _ := s.repos.ParkingSession.GetAverageDurationMinutes()

	branch, _ := s.repos.Branch.GetFirst()
	totalSpaces := 100
	occupiedSpaces := int(activeCount)
	if branch != nil {
		totalSpaces = branch.TotalSpaces
		occupiedSpaces = branch.OccupiedSpaces
		if occupiedSpaces < int(activeCount) {
			occupiedSpaces = int(activeCount)
		}
	}
	availableSpaces := totalSpaces - occupiedSpaces
	if availableSpaces < 0 {
		availableSpaces = 0
	}

	recentSessions, _, _ := s.repos.ParkingSession.List(repository.SessionFilter{
		Page:  1,
		Limit: 8,
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
	}, nil
}

func (s *reportService) GetRevenueTrends(days int) ([]map[string]interface{}, error) {
	if days <= 0 {
		days = 7
	}
	return s.repos.Payment.GetRevenueByDays(days)
}
