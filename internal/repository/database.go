package repository

import (
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/password"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	if cfg.DBDriver == "postgres" {
		dialector = postgres.Open(cfg.DBSource)
	} else {
		dialector = sqlite.Open(cfg.DBSource)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// Auto Migrate models
	err = db.AutoMigrate(
		&domain.User{},
		&domain.Branch{},
		&domain.Camera{},
		&domain.Barrier{},
		&domain.Client{},
		&domain.Tariff{},
		&domain.ParkingSession{},
		&domain.Payment{},
		&domain.AuditLog{},
	)
	if err != nil {
		return nil, err
	}

	// Seed default data
	seedDefaultData(db, cfg)

	return db, nil
}

func seedDefaultData(db *gorm.DB, cfg *config.Config) {
	// 1. Branch
	var branchCount int64
	db.Model(&domain.Branch{}).Count(&branchCount)
	var branch domain.Branch
	if branchCount == 0 {
		branch = domain.Branch{
			ID:             uuid.New(),
			Name:           cfg.DefaultBranch,
			Address:        "Toshkent shahri, Amir Temur shox ko'chasi 15-uy",
			TotalSpaces:    120,
			OccupiedSpaces: 0,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		db.Create(&branch)
		log.Println("[Database] Seeded default branch:", branch.Name)
	} else {
		db.First(&branch)
	}

	// 2. Default Tariff
	var tariffCount int64
	db.Model(&domain.Tariff{}).Count(&tariffCount)
	if tariffCount == 0 {
		tariff := domain.Tariff{
			ID:            uuid.New(),
			Name:          "Standart Tarif (15 daq bepul)",
			BranchID:      &branch.ID,
			FreeMinutes:   15,
			HourlyPrice:   5000,  // 5000 UZS/hour
			DailyMaxPrice: 60000, // 60000 UZS max per day
			IsDefault:     true,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		db.Create(&tariff)
		log.Println("[Database] Seeded default tariff:", tariff.Name)
	}

	// 3. Admin & Operator Users
	var userCount int64
	db.Model(&domain.User{}).Count(&userCount)
	if userCount == 0 {
		hashedAdminPass, _ := password.HashPassword("admin123")
		admin := domain.User{
			ID:        uuid.New(),
			Username:  "admin",
			Password:  hashedAdminPass,
			FullName:  "Tizim Administratori",
			Role:      domain.RoleAdmin,
			BranchID:  &branch.ID,
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		db.Create(&admin)

		hashedOpPass, _ := password.HashPassword("operator123")
		operator := domain.User{
			ID:        uuid.New(),
			Username:  "operator",
			Password:  hashedOpPass,
			FullName:  "Asosiy Operator",
			Role:      domain.RoleOperator,
			BranchID:  &branch.ID,
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		db.Create(&operator)
		log.Println("[Database] Seeded default users: admin/admin123, operator/operator123")
	}

	// 4. Default Cameras (Entry & Exit)
	var cameraCount int64
	db.Model(&domain.Camera{}).Count(&cameraCount)
	if cameraCount == 0 {
		camEntry := domain.Camera{
			ID:            uuid.New(),
			Name:          "Kirish ANPR Kamera (Yo'lak 1)",
			BranchID:      branch.ID,
			Direction:     domain.DirectionEntry,
			IPAddress:     "192.168.1.101",
			Status:        domain.CameraStatusOnline,
			LastHeartbeat: time.Now(),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		camExit := domain.Camera{
			ID:            uuid.New(),
			Name:          "Chiqish ANPR Kamera (Yo'lak 2)",
			BranchID:      branch.ID,
			Direction:     domain.DirectionExit,
			IPAddress:     "192.168.1.102",
			Status:        domain.CameraStatusOnline,
			LastHeartbeat: time.Now(),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		db.Create(&camEntry)
		db.Create(&camExit)
		log.Println("[Database] Seeded default cameras")
	}

	// 5. Default Barriers (Entry & Exit)
	var barrierCount int64
	db.Model(&domain.Barrier{}).Count(&barrierCount)
	if barrierCount == 0 {
		bEntry := domain.Barrier{
			ID:           uuid.New(),
			Name:         "Kirish Shlagbaumi (Yo'lak 1)",
			BranchID:     branch.ID,
			Direction:    domain.DirectionEntry,
			Status:       domain.BarrierClosed,
			RelayURL:     "http://192.168.1.201/relay/open",
			AutoCloseSec: 10,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		bExit := domain.Barrier{
			ID:           uuid.New(),
			Name:         "Chiqish Shlagbaumi (Yo'lak 2)",
			BranchID:     branch.ID,
			Direction:    domain.DirectionExit,
			Status:       domain.BarrierClosed,
			RelayURL:     "http://192.168.1.202/relay/open",
			AutoCloseSec: 10,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		db.Create(&bEntry)
		db.Create(&bExit)
		log.Println("[Database] Seeded default barriers")
	}

	// 6. Demo VIP Clients with discounts
	var clientCount int64
	db.Model(&domain.Client{}).Count(&clientCount)
	if clientCount == 0 {
		clients := []domain.Client{
			{
				ID:           uuid.New(),
				FullName:     "Aziz Rahimov (Direktor)",
				Phone:        "+998901234567",
				PlateNumber:  "01A777AA",
				DiscountType: domain.DiscountFree100, // 100% free VIP
				Notes:        "Rahbariyat avtomobili, doimiy bepul",
				Active:       true,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			{
				ID:           uuid.New(),
				FullName:     "Jamshid Karimov (Mijoz)",
				Phone:        "+998939876543",
				PlateNumber:  "01B123BB",
				DiscountType: domain.DiscountFree1H, // 1 hour free discount
				Notes:        "1 soat bepul chegirma biriktirilgan",
				Active:       true,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			{
				ID:           uuid.New(),
				FullName:     "Sardor Aliyev (Xodim)",
				Phone:        "+998971112233",
				PlateNumber:  "01C999CC",
				DiscountType: domain.DiscountFree3H, // 3 hours free discount
				Notes:        "3 soat bepul chegirma biriktirilgan",
				Active:       true,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
		}
		for _, c := range clients {
			db.Create(&c)
		}
		log.Println("[Database] Seeded demo VIP clients with discounts")
	}
}
