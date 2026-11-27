package agent_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/agent"
	"github.com/smart-parking/parking-backend/internal/delivery/http/handler"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/agentproto"
	"github.com/smart-parking/parking-backend/internal/repository"
	"github.com/smart-parking/parking-backend/internal/service"
	"gorm.io/gorm"
)

// fakeRelay stands in for a relay on the parking LAN.
type fakeRelay struct {
	mu   sync.Mutex
	hits []string
}

func (f *fakeRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (f *fakeRelay) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

type backend struct {
	url      string
	hub      *service.AgentHub
	agents   repository.AgentRepository
	barriers repository.BarrierRepository
	svc      service.BarrierService
}

// startBackend runs the real agent WebSocket endpoint on a random port.
func startBackend(t *testing.T) *backend {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Barrier{}, &domain.Agent{}); err != nil {
		t.Fatal(err)
	}
	agents := repository.NewAgentRepository(db)
	barriers := repository.NewBarrierRepository(db)
	hub := service.NewAgentHub(agents)

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := handler.NewAgentHandler(agents, hub, nil)
	app.Get(agentproto.Path, h.Upgrade(), h.HandleWS())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	return &backend{
		url:      "http://" + ln.Addr().String(),
		hub:      hub,
		agents:   agents,
		barriers: barriers,
		svc:      service.NewBarrierService(barriers, service.NewWebSocketHub(), hub),
	}
}

func (b *backend) newAgent(t *testing.T) (*domain.Agent, string) {
	t.Helper()
	token, err := service.NewAgentToken()
	if err != nil {
		t.Fatal(err)
	}
	a := &domain.Agent{ID: uuid.New(), Name: "Parkovka #1", TokenHash: service.HashAgentToken(token)}
	if err := b.agents.Create(a); err != nil {
		t.Fatal(err)
	}
	return a, token
}

func runAgent(t *testing.T, serverURL, token string) *agent.Client {
	t.Helper()
	c := agent.NewClient(agent.Config{ServerURL: serverURL, Token: token}, "test")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)
	return c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBarrierCommandsGoThroughAgent(t *testing.T) {
	be := startBackend(t)
	ag, token := be.newAgent(t)
	relay := &fakeRelay{}
	relaySrv := httptest.NewServer(relay)
	defer relaySrv.Close()

	b := &domain.Barrier{ID: uuid.New(), Name: "Chiqish", Direction: domain.DirectionExit, Status: domain.BarrierClosed,
		ControlType: domain.BarrierControlHTTPRelay, RelayURL: relaySrv.URL + "/relay/1", CloseURL: relaySrv.URL + "/relay/2",
		AutoCloseSec: 60, AgentID: &ag.ID}
	if err := be.barriers.Create(b); err != nil {
		t.Fatal(err)
	}

	// Agent offline: the command fails and the barrier stays CLOSED.
	if err := be.svc.OpenBarrier(b.ID, "test"); err == nil || !strings.Contains(err.Error(), "oflayn") {
		t.Fatalf("offline agent: want an offline error, got %v", err)
	}
	if ping, _ := be.svc.PingBarrier(b.ID); ping["status"] != "AGENT_OFFLINE" {
		t.Fatalf("offline ping: %v", ping)
	}

	client := runAgent(t, be.url, token)
	waitFor(t, "agent online", func() bool { return be.hub.IsOnline(ag.ID) })
	if !client.Status().Connected {
		t.Fatal("client should report connected")
	}

	if err := be.svc.OpenBarrier(b.ID, "test"); err != nil {
		t.Fatalf("open via agent: %v", err)
	}
	res, err := be.svc.CloseBarrier(b.ID, "test")
	if err != nil || !res.HardwareSent {
		t.Fatalf("close via agent: res=%+v err=%v", res, err)
	}
	ping, _ := be.svc.PingBarrier(b.ID)
	if ping["status"] != "ONLINE" || ping["via_agent"] != true {
		t.Fatalf("ping via agent: %v", ping)
	}

	hits := relay.list()
	if len(hits) < 3 || hits[0] != "POST /relay/1" || hits[1] != "POST /relay/2" || hits[2] != "HEAD /relay/1" {
		t.Fatalf("relay hits: %v", hits)
	}

	// The agent reported itself in hello.
	got, _ := be.agents.GetByID(ag.ID)
	if got.Version != "test" || got.LastSeenAt == nil {
		t.Fatalf("agent row not updated: %+v", got)
	}
}

func TestAgentWithWrongTokenIsRejected(t *testing.T) {
	be := startBackend(t)
	ag, _ := be.newAgent(t)
	client := runAgent(t, be.url, "pa_wrong")
	waitFor(t, "401 reported", func() bool { return strings.Contains(client.Status().LastError, "401") })
	if be.hub.IsOnline(ag.ID) {
		t.Fatal("agent with a wrong token must not be online")
	}
}

func TestDisconnectAfterTokenRotationDropsAgent(t *testing.T) {
	be := startBackend(t)
	ag, token := be.newAgent(t)
	runAgent(t, be.url, token)
	waitFor(t, "agent online", func() bool { return be.hub.IsOnline(ag.ID) })

	// Rotate: new hash, drop the live connection. The old token must not
	// get back in.
	newToken, _ := service.NewAgentToken()
	ag.TokenHash = service.HashAgentToken(newToken)
	if err := be.agents.Update(ag); err != nil {
		t.Fatal(err)
	}
	be.hub.Disconnect(ag.ID)
	waitFor(t, "agent offline", func() bool { return !be.hub.IsOnline(ag.ID) })
	time.Sleep(1500 * time.Millisecond) // one reconnect attempt with the old token
	if be.hub.IsOnline(ag.ID) {
		t.Fatal("old token reconnected after rotation")
	}
}

// A dropped connection (backend restart, network blip) comes back by itself.
func TestAgentReconnectsAfterDrop(t *testing.T) {
	be := startBackend(t)
	ag, token := be.newAgent(t)
	runAgent(t, be.url, token)
	waitFor(t, "agent online", func() bool { return be.hub.IsOnline(ag.ID) })

	be.hub.Disconnect(ag.ID)
	waitFor(t, "agent back online", func() bool { return be.hub.IsOnline(ag.ID) })
}
