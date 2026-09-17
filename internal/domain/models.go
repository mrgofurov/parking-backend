package domain

import (
	"time"

	"github.com/google/uuid"
)

type UserRole string

const (
	RoleAdmin    UserRole = "ADMIN"
	RoleOperator UserRole = "OPERATOR"
)

type User struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;not null" json:"username"`
	Password  string    `gorm:"not null" json:"-"`
	FullName  string    `json:"full_name"`
	Role      UserRole  `gorm:"default:'OPERATOR'" json:"role"`
	BranchID  *uuid.UUID `json:"branch_id,omitempty"`
	Active    bool      `gorm:"default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

type Camera struct {
	ID            uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	Name          string        `gorm:"not null" json:"name"`
	BranchID      uuid.UUID     `json:"branch_id"`
	Direction     DirectionType `gorm:"not null" json:"direction"` // ENTRY or EXIT
	IPAddress     string        `json:"ip_address"`
	RTSPUrl       string        `json:"rtsp_url"`
	Status        CameraStatus  `gorm:"default:'ONLINE'" json:"status"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type BarrierStatus string

const (
	BarrierOpen   BarrierStatus = "OPEN"
	BarrierClosed BarrierStatus = "CLOSED"
)

type Barrier struct {
	ID              uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	Name            string        `gorm:"not null" json:"name"`
	BranchID        uuid.UUID     `json:"branch_id"`
	Direction       DirectionType `gorm:"not null" json:"direction"` // ENTRY or EXIT
	Status          BarrierStatus `gorm:"default:'CLOSED'" json:"status"`
	RelayURL        string        `json:"relay_url"` // HTTP endpoint for hardware relay
	AutoCloseSec    int           `gorm:"default:10" json:"auto_close_sec"`
	LastTriggeredAt *time.Time    `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type DiscountType string

const (
	DiscountNone     DiscountType = "NONE"
	DiscountFree1H   DiscountType = "FREE_1H"
	DiscountFree3H   DiscountType = "FREE_3H"
	DiscountFree100  DiscountType = "FREE_100"
	DiscountPercent  DiscountType = "PERCENT"
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
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name          string    `gorm:"not null" json:"name"`
	BranchID      *uuid.UUID `json:"branch_id,omitempty"`
	FreeMinutes   int       `gorm:"default:15" json:"free_minutes"`
	HourlyPrice   float64   `gorm:"default:5000" json:"hourly_price"` // In UZS
	DailyMaxPrice float64   `gorm:"default:50000" json:"daily_max_price"` // In UZS
	IsDefault     bool      `gorm:"default:false" json:"is_default"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SessionStatus string

const (
	SessionActive    SessionStatus = "ACTIVE"
	SessionPaid      SessionStatus = "PAID"
	SessionCompleted SessionStatus = "COMPLETED"
	SessionCancelled SessionStatus = "CANCELLED"
)

type ParkingSession struct {
	ID             uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	SessionCode    string         `gorm:"uniqueIndex;not null" json:"session_code"`
	BranchID       uuid.UUID      `json:"branch_id"`
	PlateNumber    string         `gorm:"index;not null" json:"plate_number"`
	EntryTime      time.Time      `gorm:"index;not null" json:"entry_time"`
	ExitTime       *time.Time     `json:"exit_time,omitempty"`
	DurationMin    int            `gorm:"default:0" json:"duration_min"`
	EntryCameraID  *uuid.UUID     `json:"entry_camera_id,omitempty"`
	ExitCameraID   *uuid.UUID     `json:"exit_camera_id,omitempty"`
	EntrySnapshot  string         `json:"entry_snapshot"`
	ExitSnapshot   string         `json:"exit_snapshot"`
	TariffID       uuid.UUID      `json:"tariff_id"`
	Tariff         *Tariff        `gorm:"foreignKey:TariffID" json:"tariff,omitempty"`
	ClientID       *uuid.UUID     `json:"client_id,omitempty"`
	Client         *Client        `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	CalculatedFee  float64        `gorm:"default:0" json:"calculated_fee"`  // In UZS
	DiscountAmount float64        `gorm:"default:0" json:"discount_amount"` // In UZS
	FinalFee       float64        `gorm:"default:0" json:"final_fee"`       // In UZS
	Status         SessionStatus  `gorm:"default:'ACTIVE';index" json:"status"`
	Notes          string         `json:"notes"`
	Payments       []Payment      `gorm:"foreignKey:SessionID" json:"payments,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type PaymentMethod string

const (
	PaymentCash PaymentMethod = "CASH"
	PaymentCard PaymentMethod = "CARD"
	PaymentQR   PaymentMethod = "QR"
)

type Payment struct {
	ID            uuid.UUID     `gorm:"type:uuid;primaryKey" json:"id"`
	SessionID     uuid.UUID     `gorm:"index;not null" json:"session_id"`
	Amount        float64       `gorm:"not null" json:"amount"` // In UZS
	PaymentMethod PaymentMethod `gorm:"not null" json:"payment_method"`
	TransactionRef string       `json:"transaction_ref"`
	OperatorID    *uuid.UUID    `json:"operator_id,omitempty"`
	OperatorName  string        `json:"operator_name"`
	ReceiptNumber string        `gorm:"uniqueIndex;not null" json:"receipt_number"`
	PaidAt        time.Time     `json:"paid_at"`
	CreatedAt     time.Time     `json:"created_at"`
}

type AuditLog struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Username  string    `json:"username"`
	Action    string    `gorm:"not null" json:"action"`
	Entity    string    `json:"entity"`
	EntityID  string    `json:"entity_id"`
	Details   string    `json:"details"`
	IPAddress string    `json:"ip_address"`
	CreatedAt time.Time `json:"created_at"`
}
