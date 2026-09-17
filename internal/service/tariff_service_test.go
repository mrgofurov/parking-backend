package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
)

func TestCalculateFee_FreeMinutes(t *testing.T) {
	svc := NewTariffService(nil)
	tariff := &domain.Tariff{
		FreeMinutes:   15,
		HourlyPrice:   5000,
		DailyMaxPrice: 50000,
	}

	// 10 minutes (less than 15) -> 0 UZS
	res := svc.CalculateFee(tariff, nil, 10)
	if res.FinalFee != 0 {
		t.Errorf("Expected 0 UZS within free minutes, got %.0f", res.FinalFee)
	}

	// 15 minutes exact -> 0 UZS
	res = svc.CalculateFee(tariff, nil, 15)
	if res.FinalFee != 0 {
		t.Errorf("Expected 0 UZS for exact free minutes, got %.0f", res.FinalFee)
	}
}

func TestCalculateFee_HourlyPricing(t *testing.T) {
	svc := NewTariffService(nil)
	tariff := &domain.Tariff{
		FreeMinutes:   15,
		HourlyPrice:   5000,
		DailyMaxPrice: 60000,
	}

	// 30 minutes (15 min free + 15 min billable = 1 hour) -> 5,000 UZS
	res := svc.CalculateFee(tariff, nil, 30)
	if res.FinalFee != 5000 {
		t.Errorf("Expected 5000 UZS for 30 minutes, got %.0f", res.FinalFee)
	}

	// 90 minutes (15 min free + 75 min billable = 2 hours) -> 10,000 UZS
	res = svc.CalculateFee(tariff, nil, 90)
	if res.FinalFee != 10000 {
		t.Errorf("Expected 10000 UZS for 90 minutes, got %.0f", res.FinalFee)
	}
}

func TestCalculateFee_VIP100Discount(t *testing.T) {
	svc := NewTariffService(nil)
	tariff := &domain.Tariff{
		FreeMinutes:   15,
		HourlyPrice:   5000,
		DailyMaxPrice: 60000,
	}
	vipClient := &domain.Client{
		ID:           uuid.New(),
		FullName:     "VIP Rahbar",
		DiscountType: domain.DiscountFree100,
	}

	// 300 minutes (5 hours) -> should be 0 UZS
	res := svc.CalculateFee(tariff, vipClient, 300)
	if res.FinalFee != 0 {
		t.Errorf("Expected 0 UZS for 100%% VIP discount, got %.0f", res.FinalFee)
	}
}

func TestCalculateFee_Free1HDiscount(t *testing.T) {
	svc := NewTariffService(nil)
	tariff := &domain.Tariff{
		FreeMinutes:   15,
		HourlyPrice:   5000,
		DailyMaxPrice: 60000,
	}
	client1H := &domain.Client{
		ID:           uuid.New(),
		FullName:     "Jamshid",
		DiscountType: domain.DiscountFree1H, // 15 + 60 = 75 min free
	}

	// 70 minutes (under 75 min) -> 0 UZS
	res := svc.CalculateFee(tariff, client1H, 70)
	if res.FinalFee != 0 {
		t.Errorf("Expected 0 UZS within 75 min free period, got %.0f", res.FinalFee)
	}

	// 90 minutes (75 min free + 15 min billable = 1 hour) -> 5,000 UZS
	res = svc.CalculateFee(tariff, client1H, 90)
	if res.FinalFee != 5000 {
		t.Errorf("Expected 5000 UZS for 90 min with 1H discount, got %.0f", res.FinalFee)
	}
}

func TestCalculateFee_DailyMaxCap(t *testing.T) {
	svc := NewTariffService(nil)
	tariff := &domain.Tariff{
		FreeMinutes:   15,
		HourlyPrice:   5000,
		DailyMaxPrice: 30000, // 30,000 UZS cap
	}

	// 10 hours (600 min) -> normally 10 * 5000 = 50,000, capped at 30,000 UZS
	res := svc.CalculateFee(tariff, nil, 600)
	if res.FinalFee != 30000 {
		t.Errorf("Expected 30000 UZS daily cap, got %.0f", res.FinalFee)
	}
}
