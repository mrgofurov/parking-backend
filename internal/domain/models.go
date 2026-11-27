package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserRole string

const (
	RoleAdmin    UserRole = "ADMIN"
	RoleOperator UserRole = "OPERATOR"
)

type User struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Username  string     `gorm:"uniqueIndex;not null" json:"username"`
	Password  string     `gorm:"not null" json:"-"`
	FullName  string     `json:"full_name"`
	Role      UserRole   `gorm:"default:'OPERATOR'" json:"role"`
	BranchID  *uuid.UUID `json:"branch_id,omitempty"`
	Active    bool       `gorm:"default:true" json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Branch struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name           string    `gorm:"not null" json:"name"`
	Address        string    `json:"address"`
	TotalSpaces    int       `gorm:"default:100" json:"total_spaces"`
	OccupiedSpaces int       `gorm:"default:0" json:"occupied_spaces"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DirectionType string

const (
	DirectionEntry DirectionType = "ENTRY"
	DirectionExit  DirectionType = "EXIT"
)

type CameraStatus string

const (
	CameraStatusOnline  CameraStatus = "ONLINE"
	CameraStatusOffline CameraStatus = "OFFLINE"
)

// CameraSource tells who reads the plate for this camera.
type CameraSource string

const (
	// Hikvision ANPR/LPR camera recognises plates itself and pushes events to
	// POST /api/v1/anpr/hikvision (HTTP Listening).
	CameraSourceHikvision CameraSource = "HIKVISION_ANPR"
	// Plain IP camera: parking-capture-service reads RTSP and pushes events to
	// POST /api/v1/anpr/event.
	CameraSourceCapture CameraSource = "CAPTURE_SERVICE"
)

type Camera struct {
	ID        uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string        `gorm:"not null" json:"name"`
	BranchID  uuid.UUID     `json:"branch_id"`
	Direction DirectionType `gorm:"not null" json:"direction"` // ENTRY or EXIT
	// Code is a short stable identifier (e.g. "cam-entry") used by the capture
	// service and webhook URLs instead of the UUID.
	Code   string       `gorm:"index" json:"code"`
	Source CameraSource `gorm:"default:'HIKVISION_ANPR'" json:"source"`
	// BarrierID binds the camera to the barrier of its lane. When empty, the
	// branch's first barrier with the same direction is used.
	BarrierID     *uuid.UUID `json:"barrier_id,omitempty"`
	IPAddress     string     `json:"ip_address"`
	RTSPUrl       string     `json:"rtsp_url"`
	MinConfidence float64    `gorm:"default:0" json:"min_confidence"` // 0-100, events below are ignored
	// DisplayIP is the lane's Hikvision DS-TVL224 LED screen ("ip" or
	// "ip:port"); empty = no LED screen on this lane.
	DisplayIP string `json:"display_ip"`
	// DisplayRows: 4 (DS-TVL224-4-5Y) or 2 (the "2 Rows" variant).
	DisplayRows   int          `gorm:"default:4" json:"display_rows"`
	Status        CameraStatus `gorm:"default:'ONLINE'" json:"status"`
	LastHeartbeat time.Time    `json:"last_heartbeat"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type BarrierStatus string

const (
	BarrierOpen   BarrierStatus = "OPEN"
	BarrierClosed BarrierStatus = "CLOSED"
)

type Barrier struct {
	ID        uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string        `gorm:"not null" json:"name"`
	BranchID  uuid.UUID     `json:"branch_id"`
	Direction DirectionType `gorm:"not null" json:"direction"` // ENTRY or EXIT
	Status    BarrierStatus `gorm:"default:'CLOSED'" json:"status"`
	// ControlType selects how the physical barrier is opened.
	ControlType BarrierControlType `gorm:"default:'HTTP_RELAY'" json:"control_type"`
	RelayURL    string             `json:"relay_url"` // HTTP_RELAY: endpoint POSTed to open
	// HIKVISION_IO / HIKVISION_GATE: the device (usually the lane's ANPR camera)
	DeviceHost        string `json:"device_host"`
	DeviceUsername    string `json:"device_username"`
	DevicePassword    string `json:"-"`
	HasDevicePassword bool   `gorm:"-" json:"has_device_password"`
	OutputID          int    `gorm:"default:1" json:"output_id"` // relay output no. / gate channel
	// Closing on command (optional). HIKVISION_GATE closes via ctrlMode=close;
	// HIKVISION_IO needs a second relay output wired to the controller's CLOSE
	// input; HTTP_RELAY needs a close URL. 0 / empty = the barrier closes by
	// itself (loop detector / its own timer) and only the status is updated.
	CloseOutputID int    `gorm:"default:0" json:"close_output_id"`
	CloseURL      string `json:"close_url"`
	AutoCloseSec  int    `gorm:"default:10" json:"auto_close_sec"`
	// AgentID: when set, the hardware commands above are executed by that
	// device-agent on the parking's local network (the backend cannot reach
	// the device itself, e.g. when it runs on a cloud server).
	AgentID         *uuid.UUID `gorm:"type:uuid;index" json:"agent_id"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Agent is a device-agent installed on a parking's local network. It keeps
// an outbound WebSocket to the backend and executes barrier commands there.
type Agent struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Name       string     `gorm:"not null" json:"name"`
	BranchID   uuid.UUID  `gorm:"type:uuid;index" json:"branch_id"`
	TokenHash  string     `gorm:"uniqueIndex;not null" json:"-"` // sha256 of the token; the token itself is shown once
	Version    string     `json:"version"`
	Hostname   string     `json:"hostname"`
	RemoteAddr string     `json:"remote_addr"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Online     bool       `gorm:"-" json:"online"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (b *Barrier) AfterFind(_ *gorm.DB) error {
	b.HasDevicePassword = b.DevicePassword != ""
	return nil
}

type BarrierControlType string

const (
	BarrierControlHTTPRelay     BarrierControlType = "HTTP_RELAY"
	BarrierControlHikvisionIO   BarrierControlType = "HIKVISION_IO"   // PUT /ISAPI/System/IO/outputs/{n}/trigger
	BarrierControlHikvisionGate BarrierControlType = "HIKVISION_GATE" // PUT /ISAPI/Parking/channels/{n}/barrierGate
	BarrierControlNone          BarrierControlType = "NONE"           // software-only (testing)
)

type DiscountType string

const (
	DiscountNone    DiscountType = "NONE"
	DiscountFree1H  DiscountType = "FREE_1H"
	DiscountFree3H  DiscountType = "FREE_3H"
	DiscountFree100 DiscountType = "FREE_100"
	DiscountPercent DiscountType = "PERCENT"
)

type Client struct {
	ID            uuid.UUID    `gorm:"type:uuid;primaryKey" json:"id"`
	FullName      string       `gorm:"not null" json:"full_name"`
	Phone         string       `json:"phone"`
	PlateNumber   string       `gorm:"uniqueIndex;not null" json:"plate_number"`
	DiscountType  DiscountType `gorm:"default:'NONE'" json:"discount_type"`
	DiscountValue float64      `gorm:"default:0" json:"discount_value"` // e.g. 20 for 20%
	Notes         string       `json:"notes"`
	Active        bool         `gorm:"default:true" json:"active"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type Tariff struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Name          string     `gorm:"not null" json:"name"`
	BranchID      *uuid.UUID `json:"branch_id,omitempty"`
	FreeMinutes   int        `gorm:"default:15" json:"free_minutes"`
	HourlyPrice   float64    `gorm:"default:5000" json:"hourly_price"`     // In UZS
	DailyMaxPrice float64    `gorm:"default:50000" json:"daily_max_price"` // In UZS
	IsDefault     bool       `gorm:"default:false" json:"is_default"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type SessionStatus string

const (
	SessionActive    SessionStatus = "ACTIVE"
	SessionPaid      SessionStatus = "PAID"
	SessionCompleted SessionStatus = "COMPLETED"
	SessionCancelled SessionStatus = "CANCELLED"
)

type ParkingSession struct {
	ID             uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	SessionCode    string        `gorm:"uniqueIndex;not null" json:"session_code"`
	BranchID       uuid.UUID     `json:"branch_id"`
	PlateNumber    string        `gorm:"index;not null" json:"plate_number"`
	EntryTime      time.Time     `gorm:"index;not null" json:"entry_time"`
	ExitTime       *time.Time    `json:"exit_time,omitempty"`
	DurationMin    int           `gorm:"default:0" json:"duration_min"`
	EntryCameraID  *uuid.UUID    `json:"entry_camera_id,omitempty"`
	ExitCameraID   *uuid.UUID    `json:"exit_camera_id,omitempty"`
	EntrySnapshot  string        `json:"entry_snapshot"`
	ExitSnapshot   string        `json:"exit_snapshot"`
	TariffID       uuid.UUID     `json:"tariff_id"`
	Tariff         *Tariff       `gorm:"foreignKey:TariffID" json:"tariff,omitempty"`
	ClientID       *uuid.UUID    `json:"client_id,omitempty"`
	Client         *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	CalculatedFee  float64       `gorm:"default:0" json:"calculated_fee"`  // In UZS
	DiscountAmount float64       `gorm:"default:0" json:"discount_amount"` // In UZS
	FinalFee       float64       `gorm:"default:0" json:"final_fee"`       // In UZS
	Status         SessionStatus `gorm:"default:'ACTIVE';index" json:"status"`
	Notes          string        `json:"notes"`
	Payments       []Payment     `gorm:"foreignKey:SessionID" json:"payments,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type PaymentMethod string

const (
	PaymentCash PaymentMethod = "CASH"
	PaymentCard PaymentMethod = "CARD"
	PaymentQR   PaymentMethod = "QR"
)

type Payment struct {
	ID             uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	SessionID      uuid.UUID     `gorm:"index;not null" json:"session_id"`
	Amount         float64       `gorm:"not null" json:"amount"` // In UZS
	PaymentMethod  PaymentMethod `gorm:"not null" json:"payment_method"`
	TransactionRef string        `json:"transaction_ref"`
	OperatorID     *uuid.UUID    `json:"operator_id,omitempty"`
	OperatorName   string        `json:"operator_name"`
	ReceiptNumber  string        `gorm:"uniqueIndex;not null" json:"receipt_number"`
	PaidAt         time.Time     `json:"paid_at"`
	CreatedAt      time.Time     `json:"created_at"`
}

type PaymentRequestStatus string

const (
	PaymentRequestPending   PaymentRequestStatus = "PENDING"
	PaymentRequestPaid      PaymentRequestStatus = "PAID"
	PaymentRequestExpired   PaymentRequestStatus = "EXPIRED"
	PaymentRequestCancelled PaymentRequestStatus = "CANCELLED"
	PaymentRequestFailed    PaymentRequestStatus = "FAILED"
)

// PaymentRequest is one QR (Munis / UzQR) shown to a driver at the exit.
// BillNumber is the Munis receiptNumber the payment is looked up by.
type PaymentRequest struct {
	ID                uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	SessionID         uuid.UUID            `gorm:"index;not null" json:"session_id"`
	BranchID          uuid.UUID            `json:"branch_id"`
	CameraID          *uuid.UUID           `json:"camera_id,omitempty"`
	PlateNumber       string               `json:"plate_number"`
	Provider          string               `json:"provider"` // MUNIS | MOCK
	BillNumber        string               `gorm:"uniqueIndex;not null" json:"bill_number"`
	Amount            float64              `gorm:"not null" json:"amount"`
	QRPayload         string               `gorm:"type:text" json:"qr_payload"`
	Status            PaymentRequestStatus `gorm:"index;default:'PENDING'" json:"status"`
	ExternalPaymentID string               `json:"external_payment_id"`
	PaidAmount        float64              `json:"paid_amount"`
	PaymentID         *uuid.UUID           `json:"payment_id,omitempty"`
	LastError         string               `json:"last_error"`
	ExpiresAt         time.Time            `json:"expires_at"`
	PaidAt            *time.Time           `json:"paid_at,omitempty"`
	LastCheckedAt     *time.Time           `json:"last_checked_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type ClickTransactionStatus string

const (
	ClickTxPrepared  ClickTransactionStatus = "PREPARED"
	ClickTxConfirmed ClickTransactionStatus = "CONFIRMED"
	ClickTxCancelled ClickTransactionStatus = "CANCELLED"
)

// ClickTransaction is one Click payment attempt for a PaymentRequest. Its ID
// is the merchant_prepare_id / merchant_confirm_id returned to Click, so
// Click's retries of the same click_trans_id get the same answer.
type ClickTransaction struct {
	ID               int64                  `gorm:"primaryKey;autoIncrement" json:"id"`
	ClickTransID     int64                  `gorm:"uniqueIndex;not null" json:"click_trans_id"`
	ClickPaydocID    int64                  `json:"click_paydoc_id"`
	PaymentRequestID uuid.UUID              `gorm:"index" json:"payment_request_id"`
	BillNumber       string                 `gorm:"index" json:"bill_number"`
	Amount           float64                `json:"amount"`
	Status           ClickTransactionStatus `gorm:"index" json:"status"`
	Note             string                 `json:"note"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type AuditLog struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Username  string     `json:"username"`
	Action    string     `gorm:"not null" json:"action"`
	Entity    string     `json:"entity"`
	EntityID  string     `json:"entity_id"`
	Details   string     `json:"details"`
	IPAddress string     `json:"ip_address"`
	CreatedAt time.Time  `json:"created_at"`
}
