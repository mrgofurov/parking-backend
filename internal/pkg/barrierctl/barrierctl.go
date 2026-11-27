// Package barrierctl drives the physical barrier hardware: Hikvision relay
// outputs, Hikvision barrier gates and HTTP relays. The backend uses it when it
// can reach the device itself; the device-agent uses the same code on the
// parking's local network when the backend runs elsewhere.
//
// It deliberately does not import the domain package, so the agent binary
// stays free of the database layer.
package barrierctl

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/smart-parking/parking-backend/internal/pkg/hikvision"
)

// Control types (same strings as domain.BarrierControlType).
const (
	TypeHTTPRelay     = "HTTP_RELAY"
	TypeHikvisionIO   = "HIKVISION_IO"
	TypeHikvisionGate = "HIKVISION_GATE"
	TypeNone          = "NONE"
)

// Spec is everything needed to drive one barrier.
type Spec struct {
	Name          string `json:"name,omitempty"` // for logs only
	Type          string `json:"type"`
	RelayURL      string `json:"relay_url,omitempty"`
	CloseURL      string `json:"close_url,omitempty"`
	Host          string `json:"host,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	OutputID      int    `json:"output_id,omitempty"`
	CloseOutputID int    `json:"close_output_id,omitempty"`
}

// PingResult is the outcome of a connectivity check.
type PingResult struct {
	Status     string `json:"status"` // SOFTWARE | ONLINE | OFFLINE | NO_URL
	PingMS     int64  `json:"ping_ms"`
	Alive      bool   `json:"is_alive"`
	Message    string `json:"message"`
	RelayURL   string `json:"relay_url,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

var errNoHost = errors.New("qurilma manzili (device_host) kiritilmagan")

// Open sends the physical "open" command.
func Open(s Spec) error {
	switch s.Type {
	case TypeNone:
		return nil
	case TypeHikvisionIO, TypeHikvisionGate:
		if s.Host == "" {
			return errNoHost
		}
		cli := hikvision.NewClient(s.Host, s.Username, s.Password)
		var err error
		for attempt := 1; attempt <= 2; attempt++ {
			if s.Type == TypeHikvisionGate {
				err = cli.OpenBarrierGate(s.OutputID)
			} else {
				err = cli.TriggerOutput(s.OutputID)
			}
			if err == nil {
				return nil
			}
			log.Printf("[Barrier] %s: ISAPI urinish %d muvaffaqiyatsiz: %v\n", s.Name, attempt, err)
		}
		return err
	default: // HTTP_RELAY
		if s.RelayURL == "" {
			return errors.New("relay URL kiritilmagan")
		}
		return postRelay(s.RelayURL)
	}
}

// HasCloseChannel reports whether a dedicated close command is configured.
// HIKVISION_GATE is left out on purpose: those controllers close themselves,
// and an extra timed close could bring the arm down on a car.
func HasCloseChannel(s Spec) bool {
	switch s.Type {
	case TypeHikvisionIO:
		return s.CloseOutputID > 0
	case TypeHTTPRelay:
		return s.CloseURL != ""
	}
	return false
}

// Close sends the physical close command if the barrier has one configured.
// sent=false means the barrier closes by itself.
func Close(s Spec) (bool, error) {
	switch s.Type {
	case TypeHikvisionGate:
		if s.Host == "" {
			return false, errNoHost
		}
		return true, hikvision.NewClient(s.Host, s.Username, s.Password).CloseBarrierGate(s.OutputID)
	case TypeHikvisionIO:
		if s.CloseOutputID <= 0 {
			return false, nil
		}
		if s.Host == "" {
			return false, errNoHost
		}
		return true, hikvision.NewClient(s.Host, s.Username, s.Password).TriggerOutput(s.CloseOutputID)
	case TypeHTTPRelay:
		if s.CloseURL == "" {
			return false, nil
		}
		return true, postRelay(s.CloseURL)
	}
	return false, nil
}

func postRelay(url string) error {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay HTTP %d qaytardi", resp.StatusCode)
	}
	return nil
}

// Ping checks that the device answers (and, for Hikvision, that the login is
// right).
func Ping(s Spec) PingResult {
	switch s.Type {
	case TypeNone:
		return PingResult{Status: "SOFTWARE", Alive: true, Message: "Dasturiy rejim: qurilma ulanmagan"}
	case TypeHikvisionIO, TypeHikvisionGate:
		if s.Host == "" {
			return PingResult{Status: "NO_URL", Message: "Qurilma manzili (device_host) kiritilmagan"}
		}
		start := time.Now()
		_, err := hikvision.NewClient(s.Host, s.Username, s.Password).DeviceInfo()
		ms := time.Since(start).Milliseconds()
		if err != nil {
			return PingResult{Status: "OFFLINE", PingMS: ms, Message: "Hikvision qurilmasiga ulanib bo'lmadi: " + err.Error()}
		}
		return PingResult{Status: "ONLINE", PingMS: ms, Alive: true,
			Message: fmt.Sprintf("Hikvision ISAPI aloqasi va login to'g'ri (%d ms)", ms)}
	}

	if s.RelayURL == "" {
		return PingResult{Status: "NO_URL", Message: "Hardware Relay URL kiritilmagan"}
	}

	// 1. HTTP probe
	start := time.Now()
	client := http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodHead, s.RelayURL, nil)
	var resp *http.Response
	if err == nil {
		resp, err = client.Do(req)
	}
	latency := max(time.Since(start).Milliseconds(), 1)
	if err == nil && resp != nil {
		resp.Body.Close()
		return PingResult{Status: "ONLINE", PingMS: latency, RelayURL: s.RelayURL, StatusCode: resp.StatusCode, Alive: true,
			Message: fmt.Sprintf("Relay aloqasi barqaror (%d ms)", latency)}
	}

	// 2. TCP probe fallback
	target := strings.TrimPrefix(strings.TrimPrefix(s.RelayURL, "http://"), "https://")
	if idx := strings.IndexAny(target, "/?"); idx != -1 {
		target = target[:idx]
	}
	if !strings.Contains(target, ":") {
		target += ":80"
	}
	tcpStart := time.Now()
	conn, tcpErr := net.DialTimeout("tcp", target, 1200*time.Millisecond)
	if tcpErr == nil {
		conn.Close()
		tcpLatency := max(time.Since(tcpStart).Milliseconds(), 1)
		return PingResult{Status: "ONLINE", PingMS: tcpLatency, RelayURL: s.RelayURL, Alive: true,
			Message: fmt.Sprintf("Relay porti ochiq (%d ms)", tcpLatency)}
	}
	return PingResult{Status: "OFFLINE", PingMS: latency, RelayURL: s.RelayURL,
		Message: "Relay kontrolleriga ulanib bo'lmadi (Aloqa yo'q)"}
}

// ToMap renders the result in the JSON shape the admin panel already uses.
func (p PingResult) ToMap() map[string]any {
	m := map[string]any{"status": p.Status, "ping_ms": p.PingMS, "is_alive": p.Alive, "message": p.Message}
	if p.RelayURL != "" {
		m["relay_url"] = p.RelayURL
	}
	if p.StatusCode != 0 {
		m["status_code"] = p.StatusCode
	}
	return m
}
