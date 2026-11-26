package hikvision

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a Hikvision device (camera / barrier controller) over ISAPI.
type Client struct {
	BaseURL  string // e.g. http://192.168.1.64
	Username string
	Password string
	http     *http.Client
}

func NewClient(host, username, password string) *Client {
	host = strings.TrimSpace(host)
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return &Client{
		BaseURL:  strings.TrimRight(host, "/"),
		Username: username,
		Password: password,
		http:     &http.Client{Timeout: 4 * time.Second},
	}
}

func (c *Client) do(method, path string, body []byte) (int, string, error) {
	ct := ""
	if body != nil {
		ct = "application/xml"
	}
	resp, err := doDigest(c.http, method, c.BaseURL+path, c.Username, c.Password, body, ct)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, string(b), nil
}

func checkStatus(status int, body string) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("login yoki parol noto'g'ri (401)")
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("qurilma xatolik qaytardi: HTTP %d %s", status, truncate(body, 200))
	}
	return nil
}

// TriggerOutput fires an alarm/relay output. The output should be configured
// on the camera as a pulse (e.g. 1-5s) so "high" produces one open pulse for
// the barrier controller.
func (c *Client) TriggerOutput(outputID int) error {
	if outputID <= 0 {
		outputID = 1
	}
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<IOPortData version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema"><outputState>high</outputState></IOPortData>`)
	status, resp, err := c.do(http.MethodPut, fmt.Sprintf("/ISAPI/System/IO/outputs/%d/trigger", outputID), body)
	if err != nil {
		return err
	}
	return checkStatus(status, resp)
}

// OpenBarrierGate uses the dedicated ISAPI parking barrier endpoint that
// Hikvision ANPR "entrance/exit" cameras and barrier controllers expose.
func (c *Client) OpenBarrierGate(channel int) error {
	if channel <= 0 {
		channel = 1
	}
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<BarrierGate version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema"><ctrlMode>open</ctrlMode></BarrierGate>`)
	status, resp, err := c.do(http.MethodPut, fmt.Sprintf("/ISAPI/Parking/channels/%d/barrierGate", channel), body)
	if err != nil {
		return err
	}
	return checkStatus(status, resp)
}

// CloseBarrierGate sends ctrlMode=close to the ISAPI parking barrier endpoint.
func (c *Client) CloseBarrierGate(channel int) error {
	if channel <= 0 {
		channel = 1
	}
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<BarrierGate version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema"><ctrlMode>close</ctrlMode></BarrierGate>`)
	status, resp, err := c.do(http.MethodPut, fmt.Sprintf("/ISAPI/Parking/channels/%d/barrierGate", channel), body)
	if err != nil {
		return err
	}
	return checkStatus(status, resp)
}

// DeviceInfo checks connectivity + credentials.
func (c *Client) DeviceInfo() (string, error) {
	status, body, err := c.do(http.MethodGet, "/ISAPI/System/deviceInfo", nil)
	if err != nil {
		return "", err
	}
	if err := checkStatus(status, body); err != nil {
		return "", err
	}
	return body, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
