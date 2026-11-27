// Package agent is the device-agent: a small service on the parking's local
// network that keeps an outbound WebSocket to the backend (which may run on a
// cloud server), executes barrier commands on local devices, and forwards
// camera events to the backend with a disk queue for internet outages.
package agent

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/smart-parking/parking-backend/internal/pkg/agentproto"
)

type Config struct {
	// ServerURL is the backend base URL, e.g. https://parking.example.uz
	ServerURL string
	// Token is the agent token created in the admin panel.
	Token string
	// Listen is the local address cameras / capture-service send events to
	// (e.g. ":8090"). Empty disables the forwarder.
	Listen string
	// ANPRToken is the backend's ANPR_WEBHOOK_TOKEN, added to forwarded
	// events that do not carry one.
	ANPRToken string
	// QueueDir keeps events that could not be delivered.
	QueueDir string
	// QueueMaxAge: older undelivered events are dropped. A late exit event
	// would show a payment screen for a car that already left, so keep it
	// short.
	QueueMaxAge time.Duration
	// QueueMaxItems caps the queue; the oldest events go first.
	QueueMaxItems int
}

// DataDir is where the agent keeps its queue (and, on Windows, its log).
func DataDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "parking-agent")
	}
	return "/var/lib/parking-agent"
}

func defaultQueueDir() string { return filepath.Join(DataDir(), "queue") }

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ConfigFromEnv reads AGENT_* environment variables.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ServerURL:     strings.TrimRight(getenv("AGENT_SERVER_URL", ""), "/"),
		Token:         getenv("AGENT_TOKEN", ""),
		Listen:        getenv("AGENT_LISTEN", ":8090"),
		ANPRToken:     getenv("AGENT_ANPR_TOKEN", ""),
		QueueDir:      getenv("AGENT_QUEUE_DIR", defaultQueueDir()),
		QueueMaxAge:   30 * time.Minute,
		QueueMaxItems: 5000,
	}
	if v := getenv("AGENT_QUEUE_MAX_AGE", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("AGENT_QUEUE_MAX_AGE noto'g'ri: %w", err)
		}
		cfg.QueueMaxAge = d
	}
	if v := getenv("AGENT_QUEUE_MAX_ITEMS", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, errors.New("AGENT_QUEUE_MAX_ITEMS musbat son bo'lishi kerak")
		}
		cfg.QueueMaxItems = n
	}
	if strings.EqualFold(cfg.Listen, "off") {
		cfg.Listen = ""
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.ServerURL == "" {
		return errors.New("AGENT_SERVER_URL kiritilmagan (masalan https://parking.example.uz)")
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("AGENT_SERVER_URL http:// yoki https:// bilan boshlanishi kerak")
	}
	if c.Token == "" {
		return errors.New("AGENT_TOKEN kiritilmagan (admin panelda agent yaratganda beriladi)")
	}
	return nil
}

// WSURL is the backend's agent WebSocket endpoint.
func (c Config) WSURL() string {
	u, _ := url.Parse(c.ServerURL)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + agentproto.Path
	return u.String()
}
