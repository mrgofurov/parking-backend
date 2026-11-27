package service

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/config"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/click"
	"github.com/smart-parking/parking-backend/internal/repository"
	"gorm.io/gorm"
)

const testClickSecret = "s3cret"

type clickEnv struct {
	db      *gorm.DB
	repos   *repository.RepositoryContainer
	pay     PaymentService
	qr      QRPaymentService
	click   ClickService
	session *domain.ParkingSession
	pr      *domain.PaymentRequest
}

// newClickEnv: an active session owing 12 000 with a pending PaymentRequest.
func newClickEnv(t *testing.T, cfg *config.Config) *clickEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Branch{}, &domain.Camera{}, &domain.Barrier{}, &domain.Client{}, &domain.Tariff{},
		&domain.ParkingSession{}, &domain.Payment{}, &domain.PaymentRequest{}, &domain.ClickTransaction{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewRepositoryContainer(db)
	hub := NewWebSocketHub()
	display := NewDisplayService(hub, nil)
	pay := NewPaymentService(repos, NewBarrierService(repos.Barrier, hub, nil), display, hub)
	cfg.PaymentQRTTLMinutes, cfg.PaymentPollSeconds = 15, 3
	qr := NewQRPaymentService(cfg, repos, pay, display)
	env := &clickEnv{db: db, repos: repos, pay: pay, qr: qr, click: NewClickService(cfg, repos, qr)}

	env.session = &domain.ParkingSession{
		ID: uuid.New(), SessionCode: "S-" + uuid.NewString()[:8], BranchID: uuid.New(), PlateNumber: "01A123BC",
		EntryTime: time.Now().Add(-2 * time.Hour), Status: domain.SessionActive, FinalFee: 12000,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := repos.ParkingSession.Create(env.session); err != nil {
		t.Fatal(err)
	}
	if env.click.Enabled() || cfg.MunisMode != QRModeOff {
		pr, err := qr.RequestForSession(env.session, nil)
		if err != nil {
			t.Fatal(err)
		}
		env.pr = pr
	}
	return env
}

func liveClickCfg() *config.Config {
	return &config.Config{MunisMode: QRModeOff, ClickServiceID: "77", ClickMerchantID: "88", ClickSecretKey: testClickSecret}
}

var transSeq int64 = 1000

// prepareReq is a correctly signed Prepare for the env's request.
func (e *clickEnv) prepareReq() click.Request {
	transSeq++
	r := click.Request{
		ClickTransID: strconv.FormatInt(transSeq, 10), ServiceID: "77", ClickPaydocID: "555",
		MerchantTransID: e.pr.BillNumber, Amount: "12000", Action: "0", Error: "0", ErrorNote: "Success",
		SignTime: "2026-11-26 12:00:00",
	}
	r.SignString = r.ExpectedSign(testClickSecret)
	return r
}

func completeReq(prep click.Request, prepareID int64) click.Request {
	r := prep
	r.Action = "1"
	r.MerchantPrepareID = strconv.FormatInt(prepareID, 10)
	r.SignString = r.ExpectedSign(testClickSecret)
	return r
}

func resign(r click.Request) click.Request {
	r.SignString = r.ExpectedSign(testClickSecret)
	return r
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestClickSignMatchesDocumentedFormula(t *testing.T) {
	r := click.Request{ClickTransID: "123", ServiceID: "77", MerchantTransID: "bill1", Amount: "1000.00", Action: "0", SignTime: "2026-11-26 12:00:00"}
	// md5(click_trans_id + service_id + SECRET_KEY + merchant_trans_id + amount + action + sign_time)
	if got, want := r.ExpectedSign("KEY"), md5hex("12377KEYbill11000.0002026-11-26 12:00:00"); got != want {
		t.Errorf("prepare sign = %s, want %s", got, want)
	}
	r.Action, r.MerchantPrepareID = "1", "9"
	// Complete adds merchant_prepare_id after merchant_trans_id.
	if got, want := r.ExpectedSign("KEY"), md5hex("12377KEYbill191000.0012026-11-26 12:00:00"); got != want {
		t.Errorf("complete sign = %s, want %s", got, want)
	}
	r.SignString = r.ExpectedSign("KEY")
	if !r.SignValid("KEY") {
		t.Error("valid signature rejected")
	}
	r.Amount = "1.00"
	if r.SignValid("KEY") {
		t.Error("tampered amount accepted")
	}
}

func TestClickPayURL(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	raw := env.click.PayURL(env.pr, "https://parking.example.uz/pay/cam-exit")
	u, err := url.Parse(raw)
	if err != nil || u.Host != "my.click.uz" || u.Path != "/services/pay" {
		t.Fatalf("bad url %q", raw)
	}
	q := u.Query()
	for k, want := range map[string]string{"service_id": "77", "merchant_id": "88", "amount": "12000.00",
		"transaction_param": env.pr.BillNumber, "return_url": "https://parking.example.uz/pay/cam-exit"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
}

func TestClickRequestCreatedWhenMunisOff(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	if env.pr.Provider != ProviderClick || env.pr.QRPayload != "" || env.pr.Amount != 12000 {
		t.Errorf("request = provider %s payload %q amount %.0f", env.pr.Provider, env.pr.QRPayload, env.pr.Amount)
	}

	off := newClickEnv(t, &config.Config{MunisMode: QRModeOff, ClickMode: ClickModeOff})
	if _, err := off.qr.RequestForSession(off.session, nil); !errors.Is(err, ErrQRDisabled) {
		t.Errorf("both off: err = %v, want ErrQRDisabled", err)
	}
}

func TestClickPrepareCompleteSettlesSession(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	prep := env.prepareReq()

	p := env.click.Prepare(prep)
	if p.Error != click.ErrSuccess || p.MerchantPrepareID == nil {
		t.Fatalf("prepare = %+v", p)
	}
	if again := env.click.Prepare(prep); again.Error != 0 || *again.MerchantPrepareID != *p.MerchantPrepareID {
		t.Errorf("retried prepare = %+v, want same prepare id", again)
	}

	comp := completeReq(prep, *p.MerchantPrepareID)
	c := env.click.Complete(comp)
	if c.Error != click.ErrSuccess || c.MerchantConfirmID == nil {
		t.Fatalf("complete = %+v", c)
	}

	pr, _ := env.repos.PaymentRequest.GetByID(env.pr.ID)
	if pr.Status != domain.PaymentRequestPaid || pr.ExternalPaymentID != "CLICK:"+prep.ClickTransID || pr.PaymentID == nil {
		t.Errorf("request after complete = %s ref=%s payment=%v", pr.Status, pr.ExternalPaymentID, pr.PaymentID)
	}
	s, _ := env.repos.ParkingSession.GetByID(env.session.ID)
	if s.Status != domain.SessionCompleted {
		t.Errorf("session = %s, want COMPLETED", s.Status)
	}
	var payments []domain.Payment
	env.db.Where("session_id = ?", env.session.ID).Find(&payments)
	if len(payments) != 1 || payments[0].PaymentMethod != domain.PaymentQR || payments[0].OperatorName != "Click" || payments[0].Amount != 12000 {
		t.Errorf("payments = %+v", payments)
	}

	if again := env.click.Complete(comp); again.Error != click.ErrAlreadyPaid {
		t.Errorf("retried complete error = %d, want -4", again.Error)
	}
	if again := env.click.Prepare(prep); again.Error != click.ErrAlreadyPaid {
		t.Errorf("prepare after confirm error = %d, want -4", again.Error)
	}
	if other := env.click.Prepare(env.prepareReq()); other.Error != click.ErrAlreadyPaid {
		t.Errorf("new prepare for paid bill error = %d, want -4", other.Error)
	}
}

func TestClickPrepareRejections(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	cases := map[string]struct {
		mutate func(r click.Request) click.Request
		want   int
	}{
		"bad sign":      {func(r click.Request) click.Request { r.SignString = "deadbeef"; return r }, click.ErrSignCheckFailed},
		"wrong action":  {func(r click.Request) click.Request { r.Action = "7"; return resign(r) }, click.ErrActionNotFound},
		"wrong service": {func(r click.Request) click.Request { r.ServiceID = "1"; return resign(r) }, click.ErrBadRequest},
		"wrong amount":  {func(r click.Request) click.Request { r.Amount = "11000"; return resign(r) }, click.ErrIncorrectAmount},
		"unknown bill":  {func(r click.Request) click.Request { r.MerchantTransID = "nope"; return resign(r) }, click.ErrUserNotFound},
		"click error":   {func(r click.Request) click.Request { r.Error = "-5017"; return resign(r) }, click.ErrTransactionCanceled},
	}
	for name, c := range cases {
		if got := env.click.Prepare(c.mutate(env.prepareReq())); got.Error != c.want || got.ErrorNote != click.ErrorNote(c.want) {
			t.Errorf("%s: error = %d %q, want %d", name, got.Error, got.ErrorNote, c.want)
		}
	}
	var n int64
	env.db.Model(&domain.ClickTransaction{}).Count(&n)
	if n != 0 {
		t.Errorf("%d transactions stored for rejected prepares", n)
	}
}

func TestClickCompleteAfterCashierPaymentIsCancelled(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	prep := env.prepareReq()
	p := env.click.Prepare(prep)
	if p.Error != 0 {
		t.Fatalf("prepare = %+v", p)
	}
	// Driver pays cash at the cashier while Click is still processing.
	if _, err := env.pay.CompleteSessionPayment(env.session.ID, CompletePaymentInput{Amount: 12000, Method: domain.PaymentCash}); err != nil {
		t.Fatal(err)
	}

	if c := env.click.Complete(completeReq(prep, *p.MerchantPrepareID)); c.Error != click.ErrTransactionCanceled {
		t.Errorf("complete error = %d, want -9 (Click refunds)", c.Error)
	}
	tx, _ := env.repos.ClickTx.GetByID(*p.MerchantPrepareID)
	if tx.Status != domain.ClickTxCancelled {
		t.Errorf("tx = %s, want CANCELLED", tx.Status)
	}
	var n int64
	env.db.Model(&domain.Payment{}).Where("session_id = ?", env.session.ID).Count(&n)
	if n != 1 {
		t.Errorf("%d payments, want only the cash one", n)
	}
}

func TestClickCompleteValidation(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	prep := env.prepareReq()
	p := env.click.Prepare(prep)

	wrongPrep := completeReq(prep, *p.MerchantPrepareID+100)
	if c := env.click.Complete(wrongPrep); c.Error != click.ErrTransactionNotFound {
		t.Errorf("unknown prepare id error = %d, want -6", c.Error)
	}
	wrongAmount := completeReq(prep, *p.MerchantPrepareID)
	wrongAmount.Amount = "100"
	if c := env.click.Complete(resign(wrongAmount)); c.Error != click.ErrIncorrectAmount {
		t.Errorf("wrong amount error = %d, want -2", c.Error)
	}

	failed := completeReq(prep, *p.MerchantPrepareID)
	failed.Error, failed.ErrorNote = "-5017", "Insufficient funds"
	if c := env.click.Complete(resign(failed)); c.Error != click.ErrTransactionCanceled {
		t.Errorf("click-failed complete error = %d, want -9", c.Error)
	}
	pr, _ := env.repos.PaymentRequest.GetByID(env.pr.ID)
	if pr.Status != domain.PaymentRequestPending {
		t.Errorf("request = %s, want still PENDING after Click failure", pr.Status)
	}
	if c := env.click.Complete(completeReq(prep, *p.MerchantPrepareID)); c.Error != click.ErrTransactionCanceled {
		t.Errorf("complete after cancel error = %d, want -9", c.Error)
	}
}

func TestClickCompletesExpiredRequest(t *testing.T) {
	env := newClickEnv(t, liveClickCfg())
	prep := env.prepareReq()
	p := env.click.Prepare(prep)
	// The amount-lock window ran out while the driver was in the Click app.
	if won, _ := env.repos.PaymentRequest.MarkStatus(env.pr.ID, domain.PaymentRequestPending, domain.PaymentRequestExpired); !won {
		t.Fatal("could not expire request")
	}
	if c := env.click.Complete(completeReq(prep, *p.MerchantPrepareID)); c.Error != click.ErrSuccess {
		t.Fatalf("complete = %+v", c)
	}
	if pr, _ := env.repos.PaymentRequest.GetByID(env.pr.ID); pr.Status != domain.PaymentRequestPaid {
		t.Errorf("request = %s, want PAID", pr.Status)
	}
}

func TestClickMockPay(t *testing.T) {
	env := newClickEnv(t, &config.Config{MunisMode: QRModeOff, ClickMode: ClickModeMock})
	if env.click.Mode() != ClickModeMock {
		t.Fatalf("mode = %s", env.click.Mode())
	}
	pr, err := env.click.MockPay(env.pr.BillNumber)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != domain.PaymentRequestPaid {
		t.Errorf("request = %s, want PAID", pr.Status)
	}
	if _, err := env.click.MockPay(env.pr.BillNumber); err == nil {
		t.Error("second mock payment of the same bill succeeded")
	}

	live := newClickEnv(t, liveClickCfg())
	if _, err := live.click.MockPay(live.pr.BillNumber); !errors.Is(err, ErrClickNotMock) {
		t.Errorf("live mock pay err = %v, want ErrClickNotMock", err)
	}
}

func TestResolveClickMode(t *testing.T) {
	keys := func(mode string) *config.Config {
		return &config.Config{ClickMode: mode, ClickServiceID: "1", ClickMerchantID: "2", ClickSecretKey: "3"}
	}
	cases := []struct {
		cfg  *config.Config
		want string
	}{
		{&config.Config{}, ClickModeOff},
		{keys(""), ClickModeLive},
		{&config.Config{ClickMode: "live"}, ClickModeOff}, // live without keys
		{&config.Config{ClickMode: "MOCK"}, ClickModeMock},
		{keys("off"), ClickModeOff},
	}
	for i, c := range cases {
		if got := ResolveClickMode(c.cfg); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
}
