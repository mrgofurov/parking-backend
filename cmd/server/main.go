package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/smart-parking/parking-backend/internal/config"
	httpdelivery "github.com/smart-parking/parking-backend/internal/delivery/http"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

func main() {
	log.Println("==================================================")
	log.Println("     Starting Smart Parking Commercial Backend    ")
	log.Println("==================================================")

	// 1. Config
	cfg := config.LoadConfig()
	log.Printf("[Config] Running on port: %s, DB driver: %s\n", cfg.Port, cfg.DBDriver)

	// 2. Database
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		log.Fatalf("[Database] Connection failed: %v", err)
	}
	log.Println("[Database] Connected and schema migrated successfully.")

	// 3. Repositories
	repos := repository.NewRepositoryContainer(db)

	// 4. Services
	wsHub := service.NewWebSocketHub()
	barrierSvc := service.NewBarrierService(repos.Barrier, wsHub)
	tariffSvc := service.NewTariffService(repos.Tariff)
	anprSvc := service.NewANPRService(repos, tariffSvc, barrierSvc, wsHub, cfg.UploadDir)
	sessionSvc := service.NewSessionService(repos, tariffSvc, barrierSvc, wsHub)
	paymentSvc := service.NewPaymentService(repos, barrierSvc, wsHub)
	reportSvc := service.NewReportService(repos)

	services := &httpdelivery.Services{
		Hub:     wsHub,
		Barrier: barrierSvc,
		Tariff:  tariffSvc,
		ANPR:    anprSvc,
		Session: sessionSvc,
		Payment: paymentSvc,
		Report:  reportSvc,
	}

	// 5. Fiber App & Routes
	app := fiber.New(fiber.Config{
		AppName: "Smart Parking System API v1.0",
	})

	httpdelivery.SetupRoutes(app, cfg, repos, services)

	// 6. Start Server
	log.Printf("[Server] Smart Parking Backend listening on :%s\n", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("[Server] Failed to run: %v", err)
	}
}
