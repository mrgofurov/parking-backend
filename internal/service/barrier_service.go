package service

import (
	"context"
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

type CreateBarrierRequest struct {
	Name           string                    `json:"name"`
	BranchID       *uuid.UUID                `json:"branch_id"`
	Direction      domain.DirectionType      `json:"direction"`
	ControlType    domain.BarrierControlType `json:"control_type"`
	RelayURL       string                    `json:"relay_url"`
	DeviceHost     string                    `json:"device_host"`
	DeviceUsername string                    `json:"device_username"`
	DevicePassword string                    `json:"device_password"`
	OutputID       int                       `json:"output_id"`
	DevicePort     int                       `json:"device_port"`
	CloseOutputID  int                       `json:"close_output_id"`
	CloseURL       string                    `json:"close_url"`
	AutoCloseSec   int                       `json:"auto_close_sec"`
	// AgentID: nil = unchanged (update) / none (create), "" = unbind,
	// otherwise the device-agent that executes this barrier's commands.
	AgentID *string `json:"agent_id"`
}

// CloseResult tells the operator whether a physical close command was sent.
type CloseResult struct {
	HardwareSent bool   `json:"hardware_sent"`
	Message      string `json:"message"`
}

// UpdateBarrierRequest: an empty DevicePassword keeps the stored one.
type UpdateBarrierRequest = CreateBarrierRequest

type BarrierService interface {
	OpenBarrier(barrierID uuid.UUID, reason string) error
	// CloseBarrier: operator / sensor close — sends the close command to the
	// device when one is configured, then marks the barrier CLOSED.
	CloseBarrier(barrierID uuid.UUID, reason string) (*CloseResult, error)
	CarPassed(barrierID uuid.UUID) (*CloseResult, error)
	// ResolveLaneBarrier finds the barrier for a lane: the explicitly bound
	// barrier if any, otherwise the branch's barrier with that direction.
	ResolveLaneBarrier(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID) (*domain.Barrier, error)
	OpenForLane(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID, reason string) error
	ListBarriers() ([]domain.Barrier, error)
	GetByID(id uuid.UUID) (*domain.Barrier, error)
	CreateBarrier(req CreateBarrierRequest) (*domain.Barrier, error)
	UpdateBarrier(id uuid.UUID, req UpdateBarrierRequest) (*domain.Barrier, error)
	DeleteBarrier(id uuid.UUID) error
	PingBarrier(id uuid.UUID) (map[string]interface{}, error)
}

type barrierService struct {
	repo       repository.BarrierRepository
	hub        *WebSocketHub
	agents     AgentExecutor // nil = no device-agents
	timers     map[uuid.UUID]*time.Timer
	timersLock sync.Mutex
}

// agentCommandTimeout covers a Hikvision pulse (1 s) plus its retries.
const agentCommandTimeout = 20 * time.Second

func NewBarrierService(repo repository.BarrierRepository, hub *WebSocketHub, agents AgentExecutor) BarrierService {
	return &barrierService{
		repo:   repo,
		hub:    hub,
		agents: agents,
		timers: make(map[uuid.UUID]*time.Timer),
	}
}

// deviceAddress joins the device IP and its HTTP port ("ip" when the port is
// the default 80 or the host already names a port).
func deviceAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 || port == 80 || strings.Contains(strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://"), ":") {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func specFromBarrier(b *domain.Barrier) barrierctl.Spec {
	return barrierctl.Spec{
		Name:          b.Name,
		Type:          string(b.ControlType),
		RelayURL:      b.RelayURL,
		CloseURL:      b.CloseURL,
		Host:          deviceAddress(b.DeviceHost, b.DevicePort),
		Username:      b.DeviceUsername,
		Password:      b.DevicePassword,
		OutputID:      b.OutputID,
		CloseOutputID: b.CloseOutputID,
	}
}

// viaAgent reports whether the barrier's commands go through a device-agent.
func (s *barrierService) viaAgent(b *domain.Barrier) bool {
	return b.AgentID != nil && *b.AgentID != uuid.Nil && s.agents != nil
}

func (s *barrierService) runOnAgent(b *domain.Barrier, op string) (agentproto.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentCommandTimeout)
	defer cancel()
	res, err := s.agents.Execute(ctx, *b.AgentID, op, specFromBarrier(b))
	if err != nil {
		return res, fmt.Errorf("agent orqali: %w", err)
	}
	return res, nil
}

// triggerHardware sends the physical "open" command. It is synchronous so the
// caller knows whether the barrier really received the command.
func (s *barrierService) triggerHardware(b *domain.Barrier) error {
	if b.ControlType == domain.BarrierControlNone {
		return nil
	}
	if s.viaAgent(b) {
		_, err := s.runOnAgent(b, agentproto.OpOpen)
		return err
	}
	return barrierctl.Open(specFromBarrier(b))
}

func (s *barrierService) OpenBarrier(barrierID uuid.UUID, reason string) error {
	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return err
	}

	if hwErr := s.triggerHardware(barrier); hwErr != nil {
		log.Printf("[Barrier] %s (%s) OCHILMADI: %v. Sabab: %s\n", barrier.Name, barrier.Direction, hwErr, reason)
		s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
			"barrier_id":     barrierID,
			"branch_id":      barrier.BranchID,
			"name":           barrier.Name,
			"direction":      barrier.Direction,
			"status":         barrier.Status,
			"reason":         reason,
			"hardware_error": hwErr.Error(),
		})
		return fmt.Errorf("shlagbaum ochilmadi: %w", hwErr)
	}

	if err := s.repo.UpdateStatus(barrierID, domain.BarrierOpen); err != nil {
		return err
	}

	barrier.Status = domain.BarrierOpen
	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
		"branch_id":  barrier.BranchID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     domain.BarrierOpen,
		"reason":     reason,
	})

	// Cancel existing timer if any
	s.timersLock.Lock()
	if t, exists := s.timers[barrierID]; exists {
		t.Stop()
	}

	// Auto-close. A barrier with its own close channel (a close relay output or
	// a close URL) is sent the close command, because its controller has no
	// timer of its own. Otherwise the barrier closes itself (loop detector /
	// its own timer) and only the software status is reset.
	autoSec := barrier.AutoCloseSec
	if autoSec <= 0 {
		autoSec = 10
	}
	s.timers[barrierID] = time.AfterFunc(time.Duration(autoSec)*time.Second, func() {
		b, err := s.repo.GetByID(barrierID)
		if err == nil && hasCloseChannel(b) {
			_, _ = s.CloseBarrier(barrierID, "auto_close")
			return
		}
		_ = s.markClosed(barrierID, "auto_close")
	})
	s.timersLock.Unlock()

	log.Printf("[Barrier] Barrier %s (%s) OPENED. Reason: %s. Auto-close in %d s\n", barrier.Name, barrier.Direction, reason, autoSec)
	return nil
}

// hasCloseChannel reports whether a dedicated close command is configured.
func hasCloseChannel(b *domain.Barrier) bool {
	return barrierctl.HasCloseChannel(specFromBarrier(b))
}

// triggerHardwareClose sends the physical close command if the barrier has
// one configured. sent=false means the barrier closes by itself.
func (s *barrierService) triggerHardwareClose(b *domain.Barrier) (bool, error) {
	if s.viaAgent(b) {
		if b.ControlType != domain.BarrierControlHikvisionGate && !hasCloseChannel(b) {
			return false, nil // nothing to send: no need to bother the agent
		}
		res, err := s.runOnAgent(b, agentproto.OpClose)
		return err != nil || res.Sent, err
	}
	return barrierctl.Close(specFromBarrier(b))
}

func (s *barrierService) CloseBarrier(barrierID uuid.UUID, reason string) (*CloseResult, error) {
	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return nil, err
	}
	sent, hwErr := s.triggerHardwareClose(barrier)
	if hwErr != nil {
		log.Printf("[Barrier] %s YOPILMADI: %v\n", barrier.Name, hwErr)
		s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
			"barrier_id":     barrierID,
			"branch_id":      barrier.BranchID,
			"name":           barrier.Name,
			"direction":      barrier.Direction,
			"status":         barrier.Status,
			"reason":         reason,
			"hardware_error": hwErr.Error(),
		})
		return nil, fmt.Errorf("shlagbaum yopilmadi: %w", hwErr)
	}
	if err := s.markClosed(barrierID, reason); err != nil {
		return nil, err
	}
	res := &CloseResult{HardwareSent: sent, Message: "Yopish buyrug'i shlagbaumga yuborildi"}
	if !sent {
		res.Message = "Tizimda yopildi (shlagbaum o'zi yopiladi — yopish buyrug'i sozlanmagan)"
	}
	return res, nil
}

// markClosed updates the status only (auto-close timer, after a hardware close).
func (s *barrierService) markClosed(barrierID uuid.UUID, reason string) error {
	s.timersLock.Lock()
	if t, exists := s.timers[barrierID]; exists {
		t.Stop()
		delete(s.timers, barrierID)
	}
	s.timersLock.Unlock()

	barrier, err := s.repo.GetByID(barrierID)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(barrierID, domain.BarrierClosed); err != nil {
		return err
	}

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrierID,
		"branch_id":  barrier.BranchID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     domain.BarrierClosed,
		"reason":     reason,
	})

	log.Printf("[Barrier] Barrier %s (%s) CLOSED (%s).\n", barrier.Name, barrier.Direction, reason)
	return nil
}

func (s *barrierService) CarPassed(barrierID uuid.UUID) (*CloseResult, error) {
	log.Printf("[Barrier] Car passed barrier %s — closing.\n", barrierID)
	return s.CloseBarrier(barrierID, "Mashina o'tdi")
}

func (s *barrierService) ResolveLaneBarrier(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID) (*domain.Barrier, error) {
	if barrierID != nil && *barrierID != uuid.Nil {
		if b, err := s.repo.GetByID(*barrierID); err == nil {
			return b, nil
		}
	}
	if branchID != uuid.Nil {
		if b, err := s.repo.GetByBranchDirection(branchID, direction); err == nil {
			return b, nil
		}
	}
	b, err := s.repo.GetByDirection(direction)
	if err != nil {
		return nil, fmt.Errorf("%s yo'nalishi uchun shlagbaum topilmadi", direction)
	}
	return b, nil
}

func (s *barrierService) OpenForLane(branchID uuid.UUID, direction domain.DirectionType, barrierID *uuid.UUID, reason string) error {
	b, err := s.ResolveLaneBarrier(branchID, direction, barrierID)
	if err != nil {
		log.Printf("[Barrier] %v\n", err)
		return err
	}
	return s.OpenBarrier(b.ID, reason)
}

func (s *barrierService) ListBarriers() ([]domain.Barrier, error) {
	return s.repo.List()
}

func (s *barrierService) GetByID(id uuid.UUID) (*domain.Barrier, error) {
	return s.repo.GetByID(id)
}

func normalizeControlType(t domain.BarrierControlType) domain.BarrierControlType {
	switch t {
	case domain.BarrierControlHikvisionIO, domain.BarrierControlHikvisionGate, domain.BarrierControlNone, domain.BarrierControlHTTPRelay:
		return t
	}
	return domain.BarrierControlHTTPRelay
}

func (s *barrierService) CreateBarrier(req CreateBarrierRequest) (*domain.Barrier, error) {
	if req.Name == "" {
		return nil, errors.New("shlagbaum nomi kiritilishi shart")
	}
	if req.Direction != domain.DirectionEntry && req.Direction != domain.DirectionExit {
		req.Direction = domain.DirectionEntry
	}
	if req.AutoCloseSec <= 0 {
		req.AutoCloseSec = 10
	}
	if req.OutputID <= 0 {
		req.OutputID = 1
	}
	agentID, _, err := parseAgentID(req.AgentID)
	if err != nil {
		return nil, err
	}

	barrier := &domain.Barrier{
		ID:             uuid.New(),
		Name:           req.Name,
		Direction:      req.Direction,
		Status:         domain.BarrierClosed,
		ControlType:    normalizeControlType(req.ControlType),
		RelayURL:       req.RelayURL,
		DeviceHost:     req.DeviceHost,
		DeviceUsername: req.DeviceUsername,
		DevicePassword: req.DevicePassword,
		DevicePort:     defaultPort(req.DevicePort),
		OutputID:       req.OutputID,
		CloseOutputID:  req.CloseOutputID,
		CloseURL:       req.CloseURL,
		AutoCloseSec:   req.AutoCloseSec,
		AgentID:        agentID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if req.BranchID != nil {
		barrier.BranchID = *req.BranchID
	}

	if err := s.repo.Create(barrier); err != nil {
		return nil, err
	}
	barrier.HasDevicePassword = barrier.DevicePassword != ""

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrier.ID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     barrier.Status,
		"action":     "created",
	})

	return barrier, nil
}

func (s *barrierService) UpdateBarrier(id uuid.UUID, req UpdateBarrierRequest) (*domain.Barrier, error) {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		barrier.Name = req.Name
	}
	if req.Direction == domain.DirectionEntry || req.Direction == domain.DirectionExit {
		barrier.Direction = req.Direction
	}
	if req.BranchID != nil {
		barrier.BranchID = *req.BranchID
	}
	if req.ControlType != "" {
		barrier.ControlType = normalizeControlType(req.ControlType)
	}
	barrier.RelayURL = req.RelayURL
	barrier.DeviceHost = req.DeviceHost
	barrier.DeviceUsername = req.DeviceUsername
	if req.DevicePassword != "" {
		barrier.DevicePassword = req.DevicePassword
	}
	if req.OutputID > 0 {
		barrier.OutputID = req.OutputID
	}
	if req.DevicePort > 0 {
		barrier.DevicePort = req.DevicePort
	}
	if req.CloseOutputID >= 0 {
		barrier.CloseOutputID = req.CloseOutputID
	}
	barrier.CloseURL = req.CloseURL
	if req.AutoCloseSec > 0 {
		barrier.AutoCloseSec = req.AutoCloseSec
	}
	if agentID, given, err := parseAgentID(req.AgentID); err != nil {
		return nil, err
	} else if given {
		barrier.AgentID = agentID
	}
	barrier.UpdatedAt = time.Now()

	if err := s.repo.Update(barrier); err != nil {
		return nil, err
	}
	barrier.HasDevicePassword = barrier.DevicePassword != ""

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": barrier.ID,
		"name":       barrier.Name,
		"direction":  barrier.Direction,
		"status":     barrier.Status,
		"action":     "updated",
	})

	return barrier, nil
}

func (s *barrierService) DeleteBarrier(id uuid.UUID) error {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	s.hub.Broadcast(WSEventBarrierChange, map[string]interface{}{
		"barrier_id": id,
		"name":       barrier.Name,
		"action":     "deleted",
	})

	return nil
}

func (s *barrierService) PingBarrier(id uuid.UUID) (map[string]interface{}, error) {
	barrier, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if barrier.ControlType == domain.BarrierControlNone || !s.viaAgent(barrier) {
		return barrierctl.Ping(specFromBarrier(barrier)).ToMap(), nil
	}

	if !s.agents.IsOnline(*barrier.AgentID) {
		return map[string]interface{}{"status": "AGENT_OFFLINE", "ping_ms": 0, "is_alive": false, "via_agent": true,
			"message": "Parkovkadagi device-agent ulanmagan (oflayn)"}, nil
	}
	start := time.Now()
	res, err := s.runOnAgent(barrier, agentproto.OpPing)
	if err != nil || res.Ping == nil {
		msg := "agent javob bermadi"
		if err != nil {
			msg = err.Error()
		}
		return map[string]interface{}{"status": "OFFLINE", "ping_ms": time.Since(start).Milliseconds(), "is_alive": false,
			"via_agent": true, "message": msg}, nil
	}
	m := res.Ping.ToMap()
	m["via_agent"] = true
	m["message"] = "Agent orqali: " + res.Ping.Message
	return m, nil
}

func defaultPort(p int) int {
	if p <= 0 {
		return 80
	}
	return p
}

// parseAgentID: nil = not given, "" = unbind.
func parseAgentID(v *string) (id *uuid.UUID, given bool, err error) {
	if v == nil {
		return nil, false, nil
	}
	if strings.TrimSpace(*v) == "" {
		return nil, true, nil
	}
	u, err := uuid.Parse(strings.TrimSpace(*v))
	if err != nil {
		return nil, true, errors.New("agent_id noto'g'ri")
	}
	return &u, true, nil
}
