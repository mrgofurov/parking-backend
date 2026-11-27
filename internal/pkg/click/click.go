// Package click implements the merchant side of Click (click.uz):
//
//   - the payment link the driver opens (https://my.click.uz/services/pay?...);
//     the Click app intercepts it on phones, otherwise it opens in the browser;
//   - SHOP API: Click POSTs Prepare (action=0) and Complete (action=1) to the
//     merchant as application/x-www-form-urlencoded, signed with
//     md5(click_trans_id + service_id + SECRET_KEY + merchant_trans_id +
//     [merchant_prepare_id] + amount + action + sign_time). The merchant
//     answers with JSON.
//
// Reference: https://docs.click.uz/en/shop-api/requests,
// https://docs.click.uz/en/shop-api/errors, https://docs.click.uz/en/click-button/
package click

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const PayBaseURL = "https://my.click.uz/services/pay"

const (
	ActionPrepare  = 0
	ActionComplete = 1
)

// Error codes the merchant returns to Click.
const (
	ErrSuccess             = 0
	ErrSignCheckFailed     = -1
	ErrIncorrectAmount     = -2
	ErrActionNotFound      = -3
	ErrAlreadyPaid         = -4
	ErrUserNotFound        = -5
	ErrTransactionNotFound = -6
	ErrFailedToUpdate      = -7
	ErrBadRequest          = -8
	ErrTransactionCanceled = -9
)

var errorNotes = map[int]string{
	ErrSuccess:             "Success",
	ErrSignCheckFailed:     "SIGN CHECK FAILED!",
	ErrIncorrectAmount:     "Incorrect parameter amount",
	ErrActionNotFound:      "Action not found",
	ErrAlreadyPaid:         "Already paid",
	ErrUserNotFound:        "User does not exist",
	ErrTransactionNotFound: "Transaction does not exist",
	ErrFailedToUpdate:      "Failed to update user",
	ErrBadRequest:          "Error in request from click",
	ErrTransactionCanceled: "Transaction cancelled",
}

// ErrorNote is the documented error_note for code.
func ErrorNote(code int) string { return errorNotes[code] }

// Config holds what Click issues to the merchant on sign-up.
type Config struct {
	ServiceID      string
	MerchantID     string
	MerchantUserID string // Merchant API auth (not needed for SHOP API)
	SecretKey      string
}

// Configured reports whether payment links and SHOP API can work.
func (c Config) Configured() bool {
	return c.ServiceID != "" && c.MerchantID != "" && c.SecretKey != ""
}

// PayURL builds the payment link. transactionParam comes back to us as
// merchant_trans_id in Prepare/Complete. Amount is in sums, format N.NN.
func (c Config) PayURL(amount float64, transactionParam, returnURL string) string {
	q := url.Values{}
	q.Set("service_id", c.ServiceID)
	q.Set("merchant_id", c.MerchantID)
	q.Set("amount", FormatAmount(amount))
	q.Set("transaction_param", transactionParam)
	if returnURL != "" {
		q.Set("return_url", returnURL)
	}
	return PayBaseURL + "?" + q.Encode()
}

// FormatAmount renders sums as N.NN.
func FormatAmount(amount float64) string { return strconv.FormatFloat(amount, 'f', 2, 64) }

// Request is one Prepare or Complete call. Fields stay as the raw strings
// Click sent: the signature is computed over them verbatim (e.g. "1000" vs
// "1000.00" give different hashes).
type Request struct {
	ClickTransID      string
	ServiceID         string
	ClickPaydocID     string
	MerchantTransID   string
	MerchantPrepareID string // Complete only
	Amount            string
	Action            string
	Error             string
	ErrorNote         string
	SignTime          string
	SignString        string
}

// FormGetter returns one form field (e.g. url.Values.Get).
type FormGetter func(key string) string

// ParseRequest reads a SHOP API request from form fields.
func ParseRequest(get FormGetter) Request {
	return Request{
		ClickTransID:      strings.TrimSpace(get("click_trans_id")),
		ServiceID:         strings.TrimSpace(get("service_id")),
		ClickPaydocID:     strings.TrimSpace(get("click_paydoc_id")),
		MerchantTransID:   strings.TrimSpace(get("merchant_trans_id")),
		MerchantPrepareID: strings.TrimSpace(get("merchant_prepare_id")),
		Amount:            strings.TrimSpace(get("amount")),
		Action:            strings.TrimSpace(get("action")),
		Error:             strings.TrimSpace(get("error")),
		ErrorNote:         get("error_note"),
		SignTime:          strings.TrimSpace(get("sign_time")),
		SignString:        strings.TrimSpace(get("sign_string")),
	}
}

// Form encodes the request the way Click sends it (used by mock mode / tests).
func (r Request) Form() url.Values {
	v := url.Values{}
	v.Set("click_trans_id", r.ClickTransID)
	v.Set("service_id", r.ServiceID)
	v.Set("click_paydoc_id", r.ClickPaydocID)
	v.Set("merchant_trans_id", r.MerchantTransID)
	if r.MerchantPrepareID != "" {
		v.Set("merchant_prepare_id", r.MerchantPrepareID)
	}
	v.Set("amount", r.Amount)
	v.Set("action", r.Action)
	v.Set("error", r.Error)
	v.Set("error_note", r.ErrorNote)
	v.Set("sign_time", r.SignTime)
	v.Set("sign_string", r.SignString)
	return v
}

// ExpectedSign is the md5 Click puts in sign_string. merchant_prepare_id
// takes part only in Complete.
func (r Request) ExpectedSign(secretKey string) string {
	var b strings.Builder
	b.WriteString(r.ClickTransID)
	b.WriteString(r.ServiceID)
	b.WriteString(secretKey)
	b.WriteString(r.MerchantTransID)
	if r.Action == strconv.Itoa(ActionComplete) {
		b.WriteString(r.MerchantPrepareID)
	}
	b.WriteString(r.Amount)
	b.WriteString(r.Action)
	b.WriteString(r.SignTime)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// SignValid checks sign_string in constant time.
func (r Request) SignValid(secretKey string) bool {
	if secretKey == "" || r.SignString == "" {
		return false
	}
	want := r.ExpectedSign(secretKey)
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(r.SignString)), []byte(want)) == 1
}

func (r Request) ActionInt() (int, error) { return strconv.Atoi(r.Action) }

// ErrorInt is Click's own status for this payment; negative means Click
// failed/cancelled it.
func (r Request) ErrorInt() int {
	n, err := strconv.Atoi(r.Error)
	if err != nil {
		return 0
	}
	return n
}

func (r Request) AmountFloat() (float64, error) {
	f, err := strconv.ParseFloat(r.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("amount: %w", err)
	}
	return f, nil
}

// Response is the JSON the merchant answers Prepare/Complete with.
type Response struct {
	ClickTransID      int64  `json:"click_trans_id"`
	MerchantTransID   string `json:"merchant_trans_id"`
	MerchantPrepareID *int64 `json:"merchant_prepare_id,omitempty"`
	MerchantConfirmID *int64 `json:"merchant_confirm_id,omitempty"`
	Error             int    `json:"error"`
	ErrorNote         string `json:"error_note"`
}
