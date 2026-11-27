package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/smart-parking/parking-backend/internal/pkg/agentproto"
	"github.com/smart-parking/parking-backend/internal/pkg/barrierctl"
)

// readWait: the backend pings every 20 s; a connection silent for longer
// than this is considered dead and re-dialled.
const readWait = 75 * time.Second

// Client keeps the WebSocket to the backend and executes its commands.
type Client struct {
	cfg     Config
	version string
	// Exec runs one command; replaceable in tests.
	Exec func(agentproto.Message) agentproto.Message

	mu          sync.Mutex
	connected   bool
	connectedAt time.Time
	lastErr     string
}

func NewClient(cfg Config, version string) *Client {
	return &Client{cfg: cfg, version: version, Exec: ExecuteCommand}
}

// Status is what /health reports about the backend connection.
type Status struct {
	Connected   bool      `json:"connected"`
	ConnectedAt time.Time `json:"connected_at,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Connected: c.connected, LastError: c.lastErr}
	if c.connected {
		s.ConnectedAt = c.connectedAt
	}
	return s
}

func (c *Client) setState(connected bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = connected
	if connected {
		c.connectedAt = time.Now()
		c.lastErr = ""
	}
	if err != nil {
		c.lastErr = err.Error()
	}
}

// Run connects and reconnects until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.runOnce(ctx)
		c.setState(false, err)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second // it was a working connection: retry fast
		}
		log.Printf("[agent] backend bilan aloqa yo'q: %v (%s dan keyin qayta urinaman)\n", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, Proxy: http.ProxyFromEnvironment}
	header := http.Header{"Authorization": {"Bearer " + c.cfg.Token}}
	conn, resp, err := dialer.DialContext(ctx, c.cfg.WSURL(), header)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return errors.New("token noto'g'ri yoki agent o'chirilgan (401)")
		}
		if resp != nil {
			return fmt.Errorf("ulanib bo'lmadi: HTTP %d", resp.StatusCode)
		}
		return err
	}
	defer conn.Close()
	c.setState(true, nil)
	log.Printf("[agent] backendga ulandi: %s\n", c.cfg.WSURL())

	var writeMu sync.Mutex
	write := func(msg agentproto.Message) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	hostname, _ := os.Hostname()
	if err := write(agentproto.Message{Type: agentproto.TypeHello, Version: c.version, Hostname: hostname}); err != nil {
		return err
	}

	_ = conn.SetReadDeadline(time.Now().Add(readWait))
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(readWait))
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})

	// Close the socket when the agent shuts down, unblocking ReadMessage.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(readWait))
		var msg agentproto.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[agent] noto'g'ri xabar: %v\n", err)
			continue
		}
		if msg.Type != agentproto.TypeCommand {
			continue
		}
		go func(cmd agentproto.Message) {
			if err := write(c.Exec(cmd)); err != nil {
				log.Printf("[agent] natijani yuborib bo'lmadi: %v\n", err)
			}
		}(msg)
	}
}

// deviceLocks serialises commands to the same device, so two pulses on one
// relay never overlap.
var deviceLocks sync.Map

func deviceKey(s barrierctl.Spec) string {
	if s.Host != "" {
		return s.Host
	}
	return s.RelayURL
}

// ExecuteCommand runs a backend command on the local hardware.
func ExecuteCommand(cmd agentproto.Message) agentproto.Message {
	res := agentproto.Message{Type: agentproto.TypeResult, ID: cmd.ID}
	if cmd.Spec == nil {
		res.Error = "spec yo'q"
		return res
	}
	spec := *cmd.Spec

	lock, _ := deviceLocks.LoadOrStore(deviceKey(spec), &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	start := time.Now()
	var err error
	switch cmd.Op {
	case agentproto.OpOpen:
		err = barrierctl.Open(spec)
	case agentproto.OpClose:
		res.Sent, err = barrierctl.Close(spec)
	case agentproto.OpPing:
		p := barrierctl.Ping(spec)
		res.Ping = &p
	default:
		err = fmt.Errorf("noma'lum buyruq: %s", cmd.Op)
	}
	if err != nil {
		res.Error = err.Error()
	} else {
		res.OK = true
	}
	log.Printf("[agent] %s %q (%s): ok=%v %s %s\n", cmd.Op, spec.Name, spec.Type, res.OK,
		time.Since(start).Round(time.Millisecond), res.Error)
	return res
}
