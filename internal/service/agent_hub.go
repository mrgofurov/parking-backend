package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/agentproto"
	"github.com/smart-parking/parking-backend/internal/pkg/barrierctl"
	"github.com/smart-parking/parking-backend/internal/repository"
)

// WebSocket message types (RFC 6455), so this file does not depend on a
// particular WebSocket library.
const (
	wsTextMessage  = 1
	wsCloseMessage = 8
	wsPingMessage  = 9
)

var (
	ErrAgentOffline = errors.New("device-agent ulanmagan (oflayn)")
	ErrAgentTimeout = errors.New("device-agent javob bermadi")
)

// AgentSocket is the part of a WebSocket connection the hub needs. The
// gofiber/fasthttp WebSocket connection satisfies it.
type AgentSocket interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	Close() error
}

// AgentExecutor runs a barrier hardware command on a device-agent.
type AgentExecutor interface {
	Execute(ctx context.Context, agentID uuid.UUID, op string, spec barrierctl.Spec) (agentproto.Message, error)
	IsOnline(agentID uuid.UUID) bool
}

// AgentHub keeps the live WebSocket of every connected device-agent and
// routes commands to it.
type AgentHub struct {
	repo repository.AgentRepository

	// PingInterval / PongWait: the backend pings each agent; an agent that
	// stays silent for PongWait is dropped.
	PingInterval time.Duration
	PongWait     time.Duration

	mu    sync.Mutex
	conns map[uuid.UUID]*agentConn
}

type agentConn struct {
	agentID uuid.UUID
	sock    AgentSocket

	writeMu sync.Mutex

	pendMu  sync.Mutex
	pending map[string]chan agentproto.Message

	closeOnce sync.Once
	closed    chan struct{}
}

func NewAgentHub(repo repository.AgentRepository) *AgentHub {
	return &AgentHub{
		repo:         repo,
		PingInterval: 20 * time.Second,
		PongWait:     60 * time.Second,
		conns:        map[uuid.UUID]*agentConn{},
	}
}

// NewAgentToken returns a fresh random agent token. Only its hash is stored.
func NewAgentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pa_" + hex.EncodeToString(b), nil
}

func HashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// Authenticate finds the agent a token belongs to.
func (h *AgentHub) Authenticate(token string) (*domain.Agent, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("agent token yo'q")
	}
	a, err := h.repo.GetByTokenHash(HashAgentToken(token))
	if err != nil {
		return nil, errors.New("agent token noto'g'ri")
	}
	return a, nil
}

func (h *AgentHub) IsOnline(agentID uuid.UUID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[agentID]
	return ok
}

// MarkOnline fills Agent.Online for API responses.
func (h *AgentHub) MarkOnline(agents []domain.Agent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range agents {
		_, agents[i].Online = h.conns[agents[i].ID]
	}
}

// Disconnect drops an agent's connection (after its token was rotated or it
// was deleted).
func (h *AgentHub) Disconnect(agentID uuid.UUID) {
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c != nil {
		c.close()
	}
}

// Serve runs one agent connection until it drops. A newer connection of the
// same agent replaces an older one.
func (h *AgentHub) Serve(agent *domain.Agent, sock AgentSocket, remoteAddr string) {
	c := &agentConn{
		agentID: agent.ID,
		sock:    sock,
		pending: map[string]chan agentproto.Message{},
		closed:  make(chan struct{}),
	}

	h.mu.Lock()
	old := h.conns[agent.ID]
	h.conns[agent.ID] = c
	h.mu.Unlock()
	if old != nil {
		old.close()
	}

	version, hostname := agent.Version, agent.Hostname
	_ = h.repo.Touch(agent.ID, version, hostname, remoteAddr)
	log.Printf("[Agent] %s ulandi (%s)\n", agent.Name, remoteAddr)

	defer func() {
		h.mu.Lock()
		if h.conns[agent.ID] == c {
			delete(h.conns, agent.ID)
		}
		h.mu.Unlock()
		c.close()
		_ = h.repo.Touch(agent.ID, version, hostname, remoteAddr)
		log.Printf("[Agent] %s uzildi\n", agent.Name)
	}()

	_ = sock.SetReadDeadline(time.Now().Add(h.PongWait))
	sock.SetPongHandler(func(string) error {
		return sock.SetReadDeadline(time.Now().Add(h.PongWait))
	})

	go func() {
		t := time.NewTicker(h.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-t.C:
				c.writeMu.Lock()
				err := sock.WriteControl(wsPingMessage, nil, time.Now().Add(5*time.Second))
				c.writeMu.Unlock()
				if err != nil {
					c.close()
					return
				}
			}
		}
	}()

	for {
		_, data, err := sock.ReadMessage()
		if err != nil {
			return
		}
		_ = sock.SetReadDeadline(time.Now().Add(h.PongWait))

		var msg agentproto.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[Agent] %s: noto'g'ri xabar: %v\n", agent.Name, err)
			continue
		}
		switch msg.Type {
		case agentproto.TypeHello:
			version, hostname = msg.Version, msg.Hostname
			_ = h.repo.Touch(agent.ID, version, hostname, remoteAddr)
			log.Printf("[Agent] %s: versiya %s, host %s\n", agent.Name, version, hostname)
		case agentproto.TypeResult:
			c.pendMu.Lock()
			ch := c.pending[msg.ID]
			c.pendMu.Unlock()
			if ch != nil {
				ch <- msg // buffered, one result per command
			}
		}
	}
}

// Execute sends a command to the agent and waits for its result.
func (h *AgentHub) Execute(ctx context.Context, agentID uuid.UUID, op string, spec barrierctl.Spec) (agentproto.Message, error) {
	h.mu.Lock()
	c := h.conns[agentID]
	h.mu.Unlock()
	if c == nil {
		return agentproto.Message{}, ErrAgentOffline
	}

	id := uuid.NewString()
	ch := make(chan agentproto.Message, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
	}()

	data, err := json.Marshal(agentproto.Message{Type: agentproto.TypeCommand, ID: id, Op: op, Spec: &spec})
	if err != nil {
		return agentproto.Message{}, err
	}
	c.writeMu.Lock()
	_ = c.sock.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err = c.sock.WriteMessage(wsTextMessage, data)
	c.writeMu.Unlock()
	if err != nil {
		c.close()
		return agentproto.Message{}, fmt.Errorf("device-agent'ga yuborib bo'lmadi: %w", err)
	}

	select {
	case res := <-ch:
		if !res.OK {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-ctx.Done():
		return agentproto.Message{}, ErrAgentTimeout
	case <-c.closed:
		return agentproto.Message{}, errors.New("device-agent aloqasi uzildi")
	}
}

func (c *agentConn) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		// A close frame tells the agent at once (it then reconnects, or gets
		// 401 after a token rotation). WriteControl is safe to call
		// concurrently with other writes.
		_ = c.sock.WriteControl(wsCloseMessage, []byte{0x03, 0xE8}, time.Now().Add(time.Second)) // 1000 normal closure
		_ = c.sock.Close()
	})
}
