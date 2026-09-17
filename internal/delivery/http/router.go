package httpdelivery

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/delivery/http/handler"
	"github.com/smart-parking/parking-backend/internal/delivery/http/middleware"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

type Handlers struct {
	Auth    *handler.AuthHandler
	ANPR    *handler.ANPRHandler
	Session *handler.SessionHandler
	Client  *handler.ClientHandler
	Tariff  *handler.TariffHandler
	Payment *handler.PaymentHandler
	Barrier *handler.BarrierHandler
	Camera  *handler.CameraHandler
	Report  *handler.ReportHandler
	WS      *handler.WSHandler
}

func SetupRoutes(
	app *fiber.App,
	cfg *config.Config,
	repos *repository.RepositoryContainer,
	services *Services,
) {
	// Global Middlewares
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: false,
	}))

	// Static Files for Snapshots
	app.Static("/uploads", cfg.UploadDir)

	// Handlers Init
	h := &Handlers{
		Auth:    handler.NewAuthHandler(repos.User, cfg),
		ANPR:    handler.NewANPRHandler(services.ANPR),
		Session: handler.NewSessionHandler(services.Session),
		Client:  handler.NewClientHandler(repos.Client),
		Tariff:  handler.NewTariffHandler(services.Tariff),
		Payment: handler.NewPaymentHandler(services.Payment),
		Barrier: handler.NewBarrierHandler(services.Barrier),
		Camera:  handler.NewCameraHandler(repos.Camera),
		Report:  handler.NewReportHandler(services.Report, repos.Branch),
		WS:      handler.NewWSHandler(services.Hub),
	}

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "smart-parking-backend"})
	})

	// WebSocket Live Endpoint
	app.Get("/ws", h.WS.UpgradeMiddleware(), h.WS.HandleWS())

	api := app.Group("/api/v1")

	// Public Auth & Webhook
	api.Post("/auth/login", h.Auth.Login)
	api.Post("/anpr/event", h.ANPR.HandleEvent)
	api.Post("/anpr/simulate", h.ANPR.SimulateEvent)
	api.Get("/anpr/heartbeat", h.ANPR.Heartbeat)

	// Protected Endpoints
	authMW := middleware.AuthMiddleware(cfg.JWTSecret)
	protected := api.Group("", authMW)

	// Auth me
	protected.Get("/auth/me", h.Auth.GetMe)

	// Users (Admin only)
	adminMW := middleware.RequireRole(domain.RoleAdmin)
	protected.Get("/users", adminMW, h.Auth.ListUsers)
	protected.Post("/users", adminMW, h.Auth.CreateUser)
	protected.Delete("/users/:id", adminMW, h.Auth.DeleteUser)

	// Parking Sessions
	protected.Get("/sessions", h.Session.ListSessions)
	protected.Get("/sessions/history", h.Session.VehicleHistory)
	protected.Get("/sessions/:id", h.Session.GetSession)
	protected.Post("/sessions/:id/close", h.Session.ManualClose)
	protected.Post("/sessions/:id/recalculate", h.Session.Recalculate)

	// Clients & Discounts
	protected.Get("/clients", h.Client.ListClients)
	protected.Get("/clients/:id", h.Client.GetClient)
	protected.Post("/clients", h.Client.CreateClient)
	protected.Put("/clients/:id", h.Client.UpdateClient)
	protected.Delete("/clients/:id", h.Client.DeleteClient)

	// Tariffs
	protected.Get("/tariffs", h.Tariff.ListTariffs)
	protected.Get("/tariffs/:id", h.Tariff.GetTariff)
	protected.Post("/tariffs", adminMW, h.Tariff.CreateTariff)
	protected.Put("/tariffs/:id", adminMW, h.Tariff.UpdateTariff)
	protected.Delete("/tariffs/:id", adminMW, h.Tariff.DeleteTariff)

	// Payments & Receipts
	protected.Get("/payments", h.Payment.ListPayments)
	protected.Post("/payments", h.Payment.ProcessPayment)
	protected.Get("/payments/:id/receipt", h.Payment.GetReceipt)

	// Barriers
	protected.Get("/barriers", h.Barrier.ListBarriers)
	protected.Post("/barriers/:id/open", h.Barrier.OpenBarrier)
	protected.Post("/barriers/:id/close", h.Barrier.CloseBarrier)
	protected.Post("/barriers/:id/car-passed", h.Barrier.CarPassed)

	// Cameras
	protected.Get("/cameras", h.Camera.ListCameras)
	protected.Post("/cameras", adminMW, h.Camera.CreateCamera)
	protected.Post("/cameras/:id/ping", h.Camera.PingCamera)
	protected.Delete("/cameras/:id", adminMW, h.Camera.DeleteCamera)

	// Reports & Dashboard
	protected.Get("/reports/dashboard", h.Report.GetDashboardStats)
	protected.Get("/reports/revenue", h.Report.GetRevenueTrends)

	// Branch Settings
	protected.Get("/settings/branch", h.Report.GetBranchSettings)
	protected.Put("/settings/branch", adminMW, h.Report.UpdateBranchSettings)
}

type Services struct {
	Hub     *service.WebSocketHub
	Barrier service.BarrierService
	Tariff  service.TariffService
	ANPR    service.ANPRService
	Session service.SessionService
	Payment service.PaymentService
	Report  service.ReportService
}
