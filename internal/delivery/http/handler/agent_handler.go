package handler

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

// AgentHandler: admin CRUD for device-agents and the WebSocket they connect to.
type AgentHandler struct {
	repo repository.AgentRepository
	hub  *service.AgentHub
}

func NewAgentHandler(repo repository.AgentRepository, hub *service.AgentHub) *AgentHandler {
	return &AgentHandler{repo: repo, hub: hub}
}

type agentRequest struct {
	Name     string     `json:"name"`
	BranchID *uuid.UUID `json:"branch_id"`
}

// agentWithToken is returned once, when a token is created or rotated.
type agentWithToken struct {
	Agent *domain.Agent `json:"agent"`
	Token string        `json:"token"`
}

func (h *AgentHandler) ListAgents(c *fiber.Ctx) error {
	agents, err := h.repo.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	h.hub.MarkOnline(agents)
	return response.Success(c, agents, "")
}

func (h *AgentHandler) CreateAgent(c *fiber.Ctx) error {
	var req agentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri so'rov formati")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "Agent nomi kiritilishi shart")
	}
	token, err := service.NewAgentToken()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	agent := &domain.Agent{
		ID:        uuid.New(),
		Name:      req.Name,
		TokenHash: service.HashAgentToken(token),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if req.BranchID != nil {
		agent.BranchID = *req.BranchID
	}
	if err := h.repo.Create(agent); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, agentWithToken{Agent: agent, Token: token},
		"Agent yaratildi. Tokenni saqlab qo'ying — u qayta ko'rsatilmaydi")
}

func (h *AgentHandler) UpdateAgent(c *fiber.Ctx) error {
	agent, err := h.find(c)
	if err != nil {
		return err
	}
	var req agentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Noto'g'ri so'rov formati")
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		agent.Name = name
	}
	if req.BranchID != nil {
		agent.BranchID = *req.BranchID
	}
	agent.UpdatedAt = time.Now()
	if err := h.repo.Update(agent); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	agent.Online = h.hub.IsOnline(agent.ID)
	return response.Success(c, agent, "Agent yangilandi")
}

// RotateToken issues a new token and drops the agent's current connection,
// so a leaked token stops working at once.
func (h *AgentHandler) RotateToken(c *fiber.Ctx) error {
	agent, err := h.find(c)
	if err != nil {
		return err
	}
	token, err := service.NewAgentToken()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	agent.TokenHash = service.HashAgentToken(token)
	agent.UpdatedAt = time.Now()
	if err := h.repo.Update(agent); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	h.hub.Disconnect(agent.ID)
	return response.Success(c, agentWithToken{Agent: agent, Token: token},
		"Yangi token yaratildi. Uni agent sozlamasiga yozing")
}

func (h *AgentHandler) DeleteAgent(c *fiber.Ctx) error {
	agent, err := h.find(c)
	if err != nil {
		return err
	}
	if err := h.repo.Delete(agent.ID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	h.hub.Disconnect(agent.ID)
	return response.Success(c, nil, "Agent o'chirildi")
}

func (h *AgentHandler) find(c *fiber.Ctx) (*domain.Agent, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return nil, response.Error(c, fiber.StatusBadRequest, "Yaroqsiz ID")
	}
	agent, err := h.repo.GetByID(id)
	if err != nil {
		return nil, response.Error(c, fiber.StatusNotFound, "Agent topilmadi")
	}
	return agent, nil
}

// Upgrade authenticates the agent by its token (Authorization: Bearer …)
// before switching to WebSocket.
func (h *AgentHandler) Upgrade() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}
		token := strings.TrimSpace(strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), "Bearer "))
		agent, err := h.hub.Authenticate(token)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, err.Error())
		}
		remote := c.IP()
		if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
			remote = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		c.Locals("agent", agent)
		c.Locals("remote", remote)
		return c.Next()
	}
}

func (h *AgentHandler) HandleWS() fiber.Handler {
	return websocket.New(func(c *websocket.Conn) {
		agent, _ := c.Locals("agent").(*domain.Agent)
		remote, _ := c.Locals("remote").(string)
		if agent == nil {
			_ = c.Close()
			return
		}
		h.hub.Serve(agent, c, remote)
	})
}
