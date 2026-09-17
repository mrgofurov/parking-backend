package service

import (
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
)

type FeeCalculationResult struct {
	DurationMin    int     `json:"duration_min"`
	FreeMinutes    int     `json:"free_minutes"`
	CalculatedFee  float64 `json:"calculated_fee"`
	DiscountAmount float64 `json:"discount_amount"`
	FinalFee       float64 `json:"final_fee"`
	Description    string  `json:"description"`
}

type TariffService interface {
	CalculateFee(tariff *domain.Tariff, client *domain.Client, durationMin int) FeeCalculationResult
	GetDefaultTariff() (*domain.Tariff, error)
	GetTariffByID(id uuid.UUID) (*domain.Tariff, error)
	ListTariffs() ([]domain.Tariff, error)
	CreateTariff(tariff *domain.Tariff) error
	UpdateTariff(tariff *domain.Tariff) error
	DeleteTariff(id uuid.UUID) error
}

type tariffService struct {
	repo repository.TariffRepository
}

func NewTariffService(repo repository.TariffRepository) TariffService {
	return &tariffService{repo: repo}
}

func (s *tariffService) CalculateFee(tariff *domain.Tariff, client *domain.Client, durationMin int) FeeCalculationResult {
	if tariff == nil {
		tariff = &domain.Tariff{
			FreeMinutes:   15,
			HourlyPrice:   5000,
			DailyMaxPrice: 60000,
		}
	}

	// 1. VIP 100% Free
	if client != nil && client.DiscountType == domain.DiscountFree100 {
		return FeeCalculationResult{
			DurationMin:    durationMin,
			FreeMinutes:    durationMin,
			CalculatedFee:  0,
			DiscountAmount: 0,
			FinalFee:       0,
			Description:    fmt.Sprintf("VIP Mijoz: %s (100%% Bepul)", client.FullName),
		}
	}

	// 2. Calculate Free Minutes
	baseFreeMin := tariff.FreeMinutes
	extraFreeMin := 0
	if client != nil {
		switch client.DiscountType {
		case domain.DiscountFree1H:
			extraFreeMin = 60
		case domain.DiscountFree3H:
			extraFreeMin = 180
		}
	}
	totalFreeMin := baseFreeMin + extraFreeMin

	// If within free time
	if durationMin <= totalFreeMin {
		desc := fmt.Sprintf("Bepul vaqt ichida (%d daq / ruxsat: %d daq)", durationMin, totalFreeMin)
		if extraFreeMin > 0 && client != nil {
			desc += fmt.Sprintf(" (Mijoz bonusi: +%d daq)", extraFreeMin)
		}
		return FeeCalculationResult{
			DurationMin:    durationMin,
			FreeMinutes:    totalFreeMin,
			CalculatedFee:  0,
			DiscountAmount: 0,
			FinalFee:       0,
			Description:    desc,
		}
	}

	// 3. Billable time
	billableMin := durationMin - totalFreeMin
	billableHours := int(math.Ceil(float64(billableMin) / 60.0))
	if billableHours < 1 {
		billableHours = 1
	}

	// Hourly fee
	calcFee := float64(billableHours) * tariff.HourlyPrice

	// Daily cap logic (1 day = 1440 min)
	if tariff.DailyMaxPrice > 0 {
		days := durationMin / 1440
		remHours := int(math.Ceil(float64(durationMin%1440) / 60.0))
		cappedDayFee := float64(days) * tariff.DailyMaxPrice
		remFee := float64(remHours) * tariff.HourlyPrice
		if remFee > tariff.DailyMaxPrice {
			remFee = tariff.DailyMaxPrice
		}
		totalCapped := cappedDayFee + remFee
		if totalCapped < calcFee {
			calcFee = totalCapped
		}
	}

	// 4. Percentage discount if applicable
	var discountAmount float64
	if client != nil && client.DiscountType == domain.DiscountPercent && client.DiscountValue > 0 {
		discountAmount = calcFee * (client.DiscountValue / 100.0)
	}

	finalFee := calcFee - discountAmount
	if finalFee < 0 {
		finalFee = 0
	}

	desc := fmt.Sprintf("%d soat uchun hisoblandi (%s so'm/soat)", billableHours, formatNumber(tariff.HourlyPrice))
	if discountAmount > 0 {
		desc += fmt.Sprintf(" - %.0f%% chegirma", client.DiscountValue)
	}

	return FeeCalculationResult{
		DurationMin:    durationMin,
		FreeMinutes:    totalFreeMin,
		CalculatedFee:  calcFee,
		DiscountAmount: discountAmount,
		FinalFee:       finalFee,
		Description:    desc,
	}
}

func formatNumber(n float64) string {
	return fmt.Sprintf("%.0f", n)
}

func (s *tariffService) GetDefaultTariff() (*domain.Tariff, error) {
	return s.repo.GetDefault()
}

func (s *tariffService) GetTariffByID(id uuid.UUID) (*domain.Tariff, error) {
	return s.repo.GetByID(id)
}

func (s *tariffService) ListTariffs() ([]domain.Tariff, error) {
	return s.repo.List()
}

func (s *tariffService) CreateTariff(tariff *domain.Tariff) error {
	return s.repo.Create(tariff)
}

func (s *tariffService) UpdateTariff(tariff *domain.Tariff) error {
	return s.repo.Update(tariff)
}

func (s *tariffService) DeleteTariff(id uuid.UUID) error {
	return s.repo.Delete(id)
}
