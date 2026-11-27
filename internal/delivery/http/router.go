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
	Display *handler.DisplayHandler
	Pay     *handler.PayHandler
	Agent   *handler.AgentHandler
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
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Branch-ID",
		AllowCredentials: false,
	}))

	// Static Files for Snapshots
	app.Static("/uploads", cfg.UploadDir)

	// Handlers Init
	h := &Handlers{
		Auth:    handler.NewAuthHandler(repos.User, cfg),
		ANPR:    handler.NewANPRHandler(cfg, services.ANPR, repos.Camera),
		Session: handler.NewSessionHandler(services.Session),
		Client:  handler.NewClientHandler(repos.Client),
		Tariff:  handler.NewTariffHandler(services.Tariff),
		Payment: handler.NewPaymentHandler(services.Payment),
		Barrier: handler.NewBarrierHandler(services.Barrier),
		Camera:  handler.NewCameraHandler(repos.Camera, repos.Branch),
		Report:  handler.NewReportHandler(services.Report, repos.Branch),
		Display: handler.NewDisplayHandler(services.ANPR, services.Display, services.QRPayment, repos.Camera, repos.Branch),
		Pay: handler.NewPayHandler(cfg, services.ANPR, services.Display, services.Click,
			repos.PaymentRequest, repos.Branch),
		Agent: handler.NewAgentHandler(repos.Agent, services.Agents),
		WS:    handler.NewWSHandler(services.Hub),
	}

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "smart-parking-backend"})
	})

	// WebSocket Live Endpoint
	app.Get("/ws", h.WS.UpgradeMiddleware(), h.WS.HandleWS())

	api := app.Group("/api/v1")

	// Device-agents on parking LANs connect here (agent token, not JWT)
	api.Get("/agent/ws", h.Agent.Upgrade(), h.Agent.HandleWS())

	// Public Auth & device webhooks (protected by ANPR_WEBHOOK_TOKEN when set)
	api.Post("/auth/login", h.Auth.Login)
	api.Post("/anpr/event", h.ANPR.HandleEvent)
	api.Post("/anpr/hikvision", h.ANPR.HikvisionEvent)
	api.Post("/anpr/hikvision/:camera", h.ANPR.HikvisionEvent)
	api.Get("/anpr/heartbeat", h.ANPR.Heartbeat)
	api.Get("/anpr/capture-cameras", h.ANPR.ListCaptureCameras)

	// Customer-facing lane display (gate monitor has no login)
	api.Get("/display", h.Display.ListLanes)
	api.Get("/display/:camera", h.Display.GetLaneState)

	// Phone payment page behind the static exit QR sticker + Click SHOP API
	// callbacks (authenticated by sign_string, not JWT)
	api.Get("/pay/:camera", h.Pay.GetPayInfo)
	api.Post("/click/prepare", h.Pay.ClickPrepare)
	api.Post("/click/complete", h.Pay.ClickComplete)
	api.Post("/click/mock-pay/:bill", h.Pay.ClickMockPay) // CLICK_MODE=mock only

	// Protected Endpoints
	authMW := middleware.AuthMiddleware(cfg.JWTSecret)
	protected := api.Group("", authMW)

	// Auth me
	protected.Get("/auth/me", h.Auth.GetMe)

	// Virtual gate simulator (was public — anyone could open barriers)
	protected.Post("/anpr/simulate", h.ANPR.SimulateEvent)

	// QR (Munis / UzQR) payment requests
	protected.Get("/payment-requests", h.Display.ListPaymentRequests)
	protected.Post("/payment-requests/:id/check", h.Display.CheckPaymentRequest)
	protected.Post("/payment-requests/:id/mock-pay", h.Display.MockPayPaymentRequest)
	protected.Post("/payment-requests/:id/cancel", h.Display.CancelPaymentRequest)

	// Users (Admin only)
	adminMW := middleware.RequireRole(domain.RoleAdmin)
	protected.Get("/users", adminMW, h.Auth.ListUsers)
	protected.Post("/users", adminMW, h.Auth.CreateUser)
	protected.Put("/users/:id", adminMW, h.Auth.UpdateUser)
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
	protected.Post("/barriers", h.Barrier.CreateBarrier)
	protected.Put("/barriers/:id", h.Barrier.UpdateBarrier)
	protected.Delete("/barriers/:id", h.Barrier.DeleteBarrier)
	protected.Post("/barriers/:id/ping", h.Barrier.PingBarrier)
	protected.Post("/barriers/:id/open", h.Barrier.OpenBarrier)
	protected.Post("/barriers/:id/close", h.Barrier.CloseBarrier)
	protected.Post("/barriers/:id/car-passed", h.Barrier.CarPassed)

	// Device-agents (admin)
	protected.Get("/agents", h.Agent.ListAgents)
	protected.Post("/agents", adminMW, h.Agent.CreateAgent)
	protected.Put("/agents/:id", adminMW, h.Agent.UpdateAgent)
	protected.Post("/agents/:id/rotate-token", adminMW, h.Agent.RotateToken)
	protected.Delete("/agents/:id", adminMW, h.Agent.DeleteAgent)

	// Cameras
	protected.Get("/cameras", h.Camera.ListCameras)
	protected.Post("/cameras", adminMW, h.Camera.CreateCamera)
	protected.Put("/cameras/:id", adminMW, h.Camera.UpdateCamera)
	protected.Post("/cameras/:id/ping", h.Camera.PingCamera)
	protected.Delete("/cameras/:id", adminMW, h.Camera.DeleteCamera)

	// Reports & Dashboard
	protected.Get("/reports/dashboard", h.Report.GetDashboardStats)
	protected.Get("/reports/revenue", h.Report.GetRevenueTrends)

	// Branch Settings (backward compat)
	protected.Get("/settings/branch", h.Report.GetBranchSettings)
	protected.Put("/settings/branch", adminMW, h.Report.UpdateBranchSettings)

	// Branches CRUD (Multi-Branch)
	protected.Get("/branches", h.Report.ListBranches)
	protected.Post("/branches", adminMW, h.Report.CreateBranch)
	protected.Get("/branches/:id", h.Report.GetBranch)
	protected.Put("/branches/:id", adminMW, h.Report.UpdateBranch)
	protected.Delete("/branches/:id", adminMW, h.Report.DeleteBranch)
}

type Services struct {
	Hub       *service.WebSocketHub
	Barrier   service.BarrierService
	Tariff    service.TariffService
	ANPR      service.ANPRService
	Session   service.SessionService
	Payment   service.PaymentService
	Report    service.ReportService
	Display   *service.DisplayService
	QRPayment service.QRPaymentService
	Click     service.ClickService
	Agents    *service.AgentHub
}
