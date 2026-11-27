package agent_test

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/smart-parking/parking-backend/internal/agent"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/delivery/http/handler"
	"github.com/smart-parking/parking-backend/internal/pkg/agentsetup"
)

// The downloaded setup .exe carries settings that connect the agent at once,
// and the agent's previous token stops working.
func TestWindowsInstallerSettingsConnect(t *testing.T) {
	b := startBackend(t)
	a, oldToken := b.newAgent(t)

	exePath := filepath.Join(t.TempDir(), "parking-agent.exe")
	if err := os.WriteFile(exePath, []byte("MZ fake windows agent"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AgentWindowsExe: exePath, ANPRWebhookToken: "anpr-secret"}
	app := fiber.New()
	h := handler.NewAgentHandler(b.agents, b.hub, cfg)
	app.Post("/agents/:id/windows-installer", h.WindowsInstaller)

	req := httptest.NewRequest("POST", "/agents/"+a.ID.String()+"/windows-installer",
		strings.NewReader(`{"server_url":"`+b.url+`/"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "parking-agent-setup.exe") {
		t.Fatalf("Content-Disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	if !strings.HasPrefix(string(body), "MZ fake windows agent") {
		t.Fatal("executable bytes are not at the start of the download")
	}

	setup := filepath.Join(t.TempDir(), "parking-agent-setup.exe")
	if err := os.WriteFile(setup, body, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := agentsetup.Read(setup)
	if err != nil || text == "" {
		t.Fatalf("embedded settings: %q, %v", text, err)
	}
	vals, err := godotenv.Unmarshal(text)
	if err != nil {
		t.Fatal(err)
	}
	if vals["AGENT_QUEUE_DIR"] != `C:\ProgramData\parking-agent\queue` || vals["AGENT_ANPR_TOKEN"] != "anpr-secret" {
		t.Fatalf("settings: %v", vals)
	}
	if vals["AGENT_TOKEN"] == oldToken || vals["AGENT_SERVER_URL"] != b.url {
		t.Fatalf("token not rotated or wrong URL: %v", vals)
	}
	for k, v := range vals {
		t.Setenv(k, v)
	}
	t.Setenv("AGENT_QUEUE_DIR", t.TempDir())
	ac, err := agent.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}

	runAgent(t, ac.ServerURL, ac.Token)
	waitFor(t, "agent with the downloaded token online", func() bool { return b.hub.IsOnline(a.ID) })

	if _, err := b.hub.Authenticate(oldToken); err == nil {
		t.Fatal("old token still accepted")
	}
}

func TestWindowsInstallerWithoutExe(t *testing.T) {
	b := startBackend(t)
	a, oldToken := b.newAgent(t)
	app := fiber.New()
	h := handler.NewAgentHandler(b.agents, b.hub, &config.Config{AgentWindowsExe: filepath.Join(t.TempDir(), "missing.exe")})
	app.Post("/agents/:id/windows-installer", h.WindowsInstaller)

	resp, err := app.Test(httptest.NewRequest("POST", "/agents/"+a.ID.String()+"/windows-installer", nil), 5000)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.StatusCode)
	}
	// A failed download must not cost the agent its working token.
	if _, err := b.hub.Authenticate(oldToken); err != nil {
		t.Fatalf("token rotated although no installer was built: %v", err)
	}
	resp, _ = app.Test(httptest.NewRequest("POST", "/agents/"+uuid.NewString()+"/windows-installer", nil), 5000)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("unknown agent: status %d", resp.StatusCode)
	}
}
