package service

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"github.com/smart-parking/parking-backend/internal/pkg/ledscreen"
)

// Display states shown on the customer-facing lane monitor.
const (
	DisplayIdle            = "IDLE"
	DisplayWelcome         = "WELCOME"          // entry: barrier opened
	DisplayPaymentRequired = "PAYMENT_REQUIRED" // exit: amount + QR
	DisplayPaid            = "PAID"             // exit: payment received, barrier opened
	DisplayFree            = "FREE"             // exit: nothing to pay, barrier opened
	DisplayNoSession       = "NO_SESSION"       // exit: entry not found
	DisplayError           = "ERROR"
)

const WSEventDisplay WSEventType = "LANE_DISPLAY"

// DisplayState is everything a lane monitor needs to render itself.
type DisplayState struct {
	CameraID         uuid.UUID            `json:"camera_id"`
	CameraCode       string               `json:"camera_code"`
	BranchID         uuid.UUID            `json:"branch_id"`
	LaneName         string               `json:"lane_name"`
	Direction        domain.DirectionType `json:"direction"`
	State            string               `json:"state"`
	PlateNumber      string               `json:"plate_number,omitempty"`
	EntryTime        *time.Time           `json:"entry_time,omitempty"`
	ExitTime         *time.Time           `json:"exit_time,omitempty"`
	DurationMin      int                  `json:"duration_min,omitempty"`
	CalculatedFee    float64              `json:"calculated_fee,omitempty"`
	DiscountAmount   float64              `json:"discount_amount,omitempty"`
	Amount           float64              `json:"amount,omitempty"`
	Description      string               `json:"description,omitempty"`
	QRPayload        string               `json:"qr_payload,omitempty"`
	PaymentRequestID *uuid.UUID           `json:"payment_request_id,omitempty"`
	ExpiresAt        *time.Time           `json:"expires_at,omitempty"`
	Message          string               `json:"message,omitempty"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

// DisplayService keeps the latest state per lane (camera) in memory and pushes
// changes over WebSocket. Terminal states fall back to IDLE automatically.
// Lanes whose camera has DisplayIP also get the state on their LED screen.
type DisplayService struct {
	hub    *WebSocketHub
	led    *ledSink
	mu     sync.Mutex
	states map[uuid.UUID]*DisplayState
	timers map[uuid.UUID]*time.Timer
}

// NewDisplayService: newLEDDriver builds the driver for a lane's LED screen
// (normally ledscreen.NewDriver); nil disables LED output.
func NewDisplayService(hub *WebSocketHub, newLEDDriver func(addr string) ledscreen.Driver) *DisplayService {
	d := &DisplayService{
		hub:    hub,
		states: map[uuid.UUID]*DisplayState{},
		timers: map[uuid.UUID]*time.Timer{},
	}
	if newLEDDriver != nil {
		d.led = newLEDSink(newLEDDriver)
	}
	return d
}

func idleAfter(state string) time.Duration {
	switch state {
	case DisplayWelcome, DisplayFree, DisplayPaid:
		return 15 * time.Second
	case DisplayNoSession, DisplayError:
		return 45 * time.Second
	case DisplayPaymentRequired:
		return 20 * time.Minute // safety net; normally replaced by PAID
	}
	return 0
}

// Set replaces the lane state and broadcasts it.
func (d *DisplayService) Set(cam *domain.Camera, st DisplayState) {
	if cam == nil {
		return
	}
	st.CameraID = cam.ID
	st.CameraCode = cam.Code
	st.BranchID = cam.BranchID
	st.LaneName = cam.Name
	st.Direction = cam.Direction
	st.UpdatedAt = time.Now()

	d.mu.Lock()
	d.states[cam.ID] = &st
	if t, ok := d.timers[cam.ID]; ok {
		t.Stop()
		delete(d.timers, cam.ID)
	}
	if after := idleAfter(st.State); after > 0 {
		camCopy := *cam
		stamp := st.UpdatedAt
		d.timers[cam.ID] = time.AfterFunc(after, func() {
			d.mu.Lock()
			cur, ok := d.states[camCopy.ID]
			stale := ok && cur.UpdatedAt.Equal(stamp)
			d.mu.Unlock()
			if stale {
				d.Set(&camCopy, DisplayState{State: DisplayIdle})
			}
		})
	}
	d.mu.Unlock()

	d.led.push(cam, st)
	if d.hub != nil {
		d.hub.Broadcast(WSEventDisplay, st)
	}
}

// Get returns the current state of a lane (IDLE if nothing happened yet).
func (d *DisplayService) Get(cam *domain.Camera) DisplayState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st, ok := d.states[cam.ID]; ok {
		return *st
	}
	return DisplayState{
		CameraID:   cam.ID,
		CameraCode: cam.Code,
		BranchID:   cam.BranchID,
		LaneName:   cam.Name,
		Direction:  cam.Direction,
		State:      DisplayIdle,
		UpdatedAt:  time.Now(),
	}
}

// FindByPaymentRequest returns the camera id currently showing a request.
func (d *DisplayService) FindByPaymentRequest(prID uuid.UUID) (uuid.UUID, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for camID, st := range d.states {
		if st.PaymentRequestID != nil && *st.PaymentRequestID == prID {
			return camID, true
		}
	}
	return uuid.Nil, false
}
