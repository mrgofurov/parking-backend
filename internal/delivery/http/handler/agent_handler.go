package handler

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/agentsetup"
	"github.com/smart-parking/parking-backend/internal/pkg/response"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
)

// AgentHandler: admin CRUD for device-agents and the WebSocket they connect to.
type AgentHandler struct {
	repo repository.AgentRepository
	hub  *service.AgentHub
	cfg  *config.Config
}

func NewAgentHandler(repo repository.AgentRepository, hub *service.AgentHub, cfg *config.Config) *AgentHandler {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &AgentHandler{repo: repo, hub: hub, cfg: cfg}
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
	if agent == nil { // the error response is already written
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
	if agent == nil { // the error response is already written
		return err
	}
	token, err := h.rotate(c, agent)
	if token == "" {
		return err
	}
	return response.Success(c, agentWithToken{Agent: agent, Token: token},
		"Yangi token yaratildi. Uni agent sozlamasiga yozing")
}

// rotate returns "" after writing the error response.
func (h *AgentHandler) rotate(c *fiber.Ctx, agent *domain.Agent) (string, error) {
	token, err := service.NewAgentToken()
	if err != nil {
		return "", response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	agent.TokenHash = service.HashAgentToken(token)
	agent.UpdatedAt = time.Now()
	if err := h.repo.Update(agent); err != nil {
		return "", response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	h.hub.Disconnect(agent.ID)
	return token, nil
}

type installerRequest struct {
	// ServerURL is the backend address the agent connects to, as the admin
	// panel knows it (its public API URL). Empty = derived from the request.
	ServerURL string `json:"server_url"`
}

// WindowsInstaller returns parking-agent-setup.exe with this agent's settings
// inside (backend URL, a new token, the ANPR token): on the parking PC it is
// downloaded and double-clicked, nothing to type. Only token hashes are
// stored, so a new token is issued and the agent's old one stops working.
func (h *AgentHandler) WindowsInstaller(c *fiber.Ctx) error {
	agent, err := h.find(c)
	if agent == nil { // the error response is already written
		return err
	}
	var req installerRequest
	_ = c.BodyParser(&req)
	serverURL, err := installerServerURL(req.ServerURL, c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	exe, err := os.ReadFile(h.cfg.AgentWindowsExe)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable,
			"Windows agent fayli serverda yo'q ("+h.cfg.AgentWindowsExe+"). Backend Docker image'ini qayta yig'ing yoki AGENT_WINDOWS_EXE ni ko'rsating")
	}
	token, err := h.rotate(c, agent)
	if token == "" {
		return err
	}
	out, err := agentsetup.Append(exe, WindowsAgentEnv(serverURL, token, h.cfg.ANPRWebhookToken, agent.Name))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	c.Set(fiber.HeaderContentType, "application/vnd.microsoft.portable-executable")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="parking-agent-setup.exe"`)
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Send(out)
}

func installerServerURL(raw string, c *fiber.Ctx) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		proto := c.Get("X-Forwarded-Proto")
		if proto == "" {
			proto = c.Protocol()
		}
		raw = proto + "://" + c.Hostname()
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("backend manzili noto'g'ri: %q (http:// yoki https:// bilan)", raw)
	}
	return raw, nil
}

// WindowsAgentEnv is the agent.env text embedded in the setup .exe.
func WindowsAgentEnv(serverURL, token, anprToken, name string) string {
	clean := strings.NewReplacer("\r", " ", "\n", " ").Replace(name)
	return fmt.Sprintf(`# Smart Parking device-agent: %s
# Built by the admin panel's "Windows o'rnatuvchi" (%s).
AGENT_SERVER_URL=%s
AGENT_TOKEN=%s
AGENT_LISTEN=:8090
AGENT_ANPR_TOKEN=%s
AGENT_QUEUE_DIR=C:\ProgramData\parking-agent\queue
AGENT_QUEUE_MAX_AGE=30m
AGENT_LOG_FILE=C:\ProgramData\parking-agent\agent.log
`, clean, time.Now().Format("2006-01-02 15:04"), serverURL, token, anprToken)
}

func (h *AgentHandler) DeleteAgent(c *fiber.Ctx) error {
	agent, err := h.find(c)
	if agent == nil { // the error response is already written
		return err
	}
	if err := h.repo.Delete(agent.ID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	h.hub.Disconnect(agent.ID)
	return response.Success(c, nil, "Agent o'chirildi")
}

// find returns nil after writing the error response (whose error, from
// c.JSON, is usually nil: callers check the agent, not the error).
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
