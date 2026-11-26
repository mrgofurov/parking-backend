// Package munis is a client for Munis "QR Online" (UzQR) — the same provider
// magnitgo-crm uses. Two separate APIs with separate auth:
//
//   - POST {QRApiURL}/qr/generate — public internet, static "Auth-Token" header.
//     Turns the merchant's static QR into a dynamic one carrying amount +
//     receiptNumber (the bill number the payment is later looked up by).
//   - GET  {ApiURL}/1.0.0/api/payments?bill_number=… — bank network (reached
//     through the IPsec tunnel), Bearer token from POST /service/auth/token.
package munis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	pathQRGenerate = "/qr/generate"
	pathPayments   = "/1.0.0/api/payments"
	pathAuthToken  = "/service/auth/token"

	CodeSuccess  = 0
	CodeNotFound = 2

	sumsToTiyin = 100
)

var (
	ErrNotConfigured = errors.New("munis sozlanmagan")
	ErrUnavailable   = errors.New("munis xizmati javob bermadi")
	ErrUnauthorized  = errors.New("munis Auth-Token yaroqsiz")
)

type Config struct {
	ApiURL      string // payments API (bank network)
	Username    string
	Password    string
	QRApiURL    string // qr/generate API (public)
	QRAuthToken string
	OriginalQR  string // optional: merchant's static QR
}

type Client struct {
	cfg  Config
	http *http.Client

	tokenMu   sync.Mutex
	token     string
	expiresAt time.Time
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

// CanGenerate reports whether QR generation is configured.
func (c *Client) CanGenerate() bool { return c.cfg.QRApiURL != "" && c.cfg.QRAuthToken != "" }

// CanVerify reports whether payment lookup is configured.
func (c *Client) CanVerify() bool {
	return c.cfg.ApiURL != "" && c.cfg.Username != "" && c.cfg.Password != ""
}

type generateRequest struct {
	OriginalQr    string `json:"originalQr,omitempty"`
	Amount        string `json:"amount"`
	ReceiptNumber string `json:"receiptNumber,omitempty"`
}

type generateResponse struct {
	Success bool   `json:"success"`
	Qr      string `json:"qr"`
	Message string `json:"message"`
}

// ChargedSum is what a QR for amount actually charges: Munis object 54 is whole
// sums, so kopecks are rounded away. Generation and verification both go
// through it so they never disagree.
func ChargedSum(amount float64) float64 { return math.Round(amount) }

// AmountMatches reports whether paidSum settles expectedSum (tolerance under
// one sum, mirroring magnitgo-crm).
func AmountMatches(paidSum, expectedSum float64) bool {
	return math.Abs(paidSum-expectedSum) < 1 || math.Abs(paidSum-ChargedSum(expectedSum)) < 0.01
}

// GenerateQR returns the dynamic QR payload for amount (sums). receiptNumber
// is capped at 25 chars by Munis and is the bill_number to verify with.
func (c *Client) GenerateQR(ctx context.Context, amount float64, receiptNumber string) (string, error) {
	if !c.CanGenerate() {
		return "", ErrNotConfigured
	}
	if len(receiptNumber) > 25 {
		return "", fmt.Errorf("receiptNumber 25 belgidan uzun: %q", receiptNumber)
	}
	body, _ := json.Marshal(generateRequest{
		OriginalQr:    c.cfg.OriginalQR,
		Amount:        strconv.FormatInt(int64(ChargedSum(amount)), 10),
		ReceiptNumber: receiptNumber,
	})

	status, respBody, err := c.do(ctx, http.MethodPost, c.cfg.QRApiURL+pathQRGenerate, body,
		map[string]string{"Auth-Token": c.cfg.QRAuthToken})
	if err != nil {
		log.Printf("[Munis] qr/generate request failed: %v", err)
		return "", ErrUnavailable
	}
	if status == http.StatusUnauthorized {
		log.Printf("[Munis] qr/generate 401: static Auth-Token invalid/revoked")
		return "", ErrUnauthorized
	}

	var res generateResponse
	if err := json.Unmarshal(respBody, &res); err != nil {
		log.Printf("[Munis] qr/generate decode error: %v body=%s", err, string(respBody))
		return "", ErrUnavailable
	}
	if status < 200 || status >= 300 || !res.Success || res.Qr == "" {
		log.Printf("[Munis] qr/generate failed status=%d success=%v message=%s", status, res.Success, res.Message)
		return "", ErrUnavailable
	}
	return res.Qr, nil
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int64  `json:"expiresIn"`
}

func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	if !c.CanVerify() {
		return "", ErrNotConfigured
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if force {
		c.token = ""
	}
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}

	body, _ := json.Marshal(authRequest{Username: c.cfg.Username, Password: c.cfg.Password})
	status, respBody, err := c.do(ctx, http.MethodPost, c.cfg.ApiURL+pathAuthToken, body, nil)
	if err != nil {
		log.Printf("[Munis] auth request failed: %v", err)
		return "", ErrUnavailable
	}
	if status < 200 || status >= 300 {
		log.Printf("[Munis] auth failed status=%d", status)
		return "", ErrUnavailable
	}
	var res authResponse
	if err := json.Unmarshal(respBody, &res); err != nil || res.AccessToken == "" {
		log.Printf("[Munis] auth decode error: %v", err)
		return "", ErrUnavailable
	}
	c.token = res.AccessToken
	ttl := time.Duration(res.ExpiresIn)*time.Second - time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	c.expiresAt = time.Now().Add(ttl)
	return c.token, nil
}

type PaymentBody struct {
	BillNumber     string  `json:"bill_number"`
	TerminalLabel  string  `json:"terminal_label"`
	AmountCurrency string  `json:"amount_currency"`
	AmountValue    float64 `json:"amount_value"` // tiyin, AFTER commission
	PayerName      string  `json:"payer_name"`
	PaymentID      int64   `json:"payment_id"`
	DocumentID     int64   `json:"document_id"`
	CreatedOn      string  `json:"created_on"`
}

// PaidSum returns the paid amount in sums.
func (b PaymentBody) PaidSum() float64 { return b.AmountValue / sumsToTiyin }

type PaymentResponse struct {
	Code         int         `json:"code"`
	Msg          string      `json:"msg"`
	ResponseBody PaymentBody `json:"response_body"` // live API uses snake_case
}

// Found reports whether the response describes an actual received payment.
func (r *PaymentResponse) Found() bool {
	return r.Code == CodeSuccess && (r.ResponseBody.PaymentID != 0 || r.ResponseBody.BillNumber != "")
}

// GetPayment looks up a payment by bill number. A response with Found()==false
// means "not paid yet".
func (c *Client) GetPayment(ctx context.Context, billNumber string) (*PaymentResponse, error) {
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	endpoint := c.cfg.ApiURL + pathPayments + "?bill_number=" + url.QueryEscape(billNumber)

	status, body, err := c.do(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return nil, ErrUnavailable
	}
	if status == http.StatusUnauthorized {
		if token, err = c.accessToken(ctx, true); err != nil {
			return nil, err
		}
		status, body, err = c.do(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return nil, ErrUnavailable
		}
	}
	if status < 200 || status >= 300 {
		log.Printf("[Munis] payments unexpected status=%d body=%s", status, string(body))
		return nil, ErrUnavailable
	}
	var res PaymentResponse
	if err := json.Unmarshal(body, &res); err != nil {
		log.Printf("[Munis] payments decode error: %v body=%s", err, string(body))
		return nil, ErrUnavailable
	}
	return &res, nil
}

func (c *Client) do(ctx context.Context, method, fullURL string, body []byte, headers map[string]string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, err
}
