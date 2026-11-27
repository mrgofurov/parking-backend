package repository

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/smart-parking/parking-backend/internal/domain"
	"gorm.io/gorm"
)

type RepositoryContainer struct {
	User           UserRepository
	Branch         BranchRepository
	Camera         CameraRepository
	Barrier        BarrierRepository
	Agent          AgentRepository
	Client         ClientRepository
	Tariff         TariffRepository
	ParkingSession ParkingSessionRepository
	Payment        PaymentRepository
	PaymentRequest PaymentRequestRepository
	ClickTx        ClickTransactionRepository
	AuditLog       AuditLogRepository
}

func NewRepositoryContainer(db *gorm.DB) *RepositoryContainer {
	return &RepositoryContainer{
		User:           NewUserRepository(db),
		Branch:         NewBranchRepository(db),
		Camera:         NewCameraRepository(db),
		Barrier:        NewBarrierRepository(db),
		Agent:          NewAgentRepository(db),
		Client:         NewClientRepository(db),
		Tariff:         NewTariffRepository(db),
		ParkingSession: NewParkingSessionRepository(db),
		Payment:        NewPaymentRepository(db),
		PaymentRequest: NewPaymentRequestRepository(db),
		ClickTx:        NewClickTransactionRepository(db),
		AuditLog:       NewAuditLogRepository(db),
	}
}

// ---------------- USER REPO ----------------
type UserRepository interface {
	GetByID(id uuid.UUID) (*domain.User, error)
	GetByUsername(username string) (*domain.User, error)
	List() ([]domain.User, error)
	Create(user *domain.User) error
	Update(user *domain.User) error
	Delete(id uuid.UUID) error
}

type userRepo struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) GetByID(id uuid.UUID) (*domain.User, error) {
	var user domain.User
	err := r.db.First(&user, "id = ?", id).Error
	return &user, err
}

func (r *userRepo) GetByUsername(username string) (*domain.User, error) {
	var user domain.User
	err := r.db.First(&user, "username = ?", username).Error
	return &user, err
}

func (r *userRepo) List() ([]domain.User, error) {
	var users []domain.User
	err := r.db.Order("created_at desc").Find(&users).Error
	return users, err
}

func (r *userRepo) Create(user *domain.User) error {
	return r.db.Create(user).Error
}

func (r *userRepo) Update(user *domain.User) error {
	return r.db.Save(user).Error
}

func (r *userRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.User{}, "id = ?", id).Error
}

// ---------------- BRANCH REPO ----------------
type BranchRepository interface {
	GetFirst() (*domain.Branch, error)
	GetByID(id uuid.UUID) (*domain.Branch, error)
	List() ([]domain.Branch, error)
	Create(branch *domain.Branch) error
	Update(branch *domain.Branch) error
	Delete(id uuid.UUID) error
	UpdateOccupiedSpaces(id uuid.UUID, occupied int) error
}

type branchRepo struct {
	db *gorm.DB
}

func NewBranchRepository(db *gorm.DB) BranchRepository {
	return &branchRepo{db: db}
}

func (r *branchRepo) GetFirst() (*domain.Branch, error) {
	var branch domain.Branch
	err := r.db.First(&branch).Error
	return &branch, err
}

func (r *branchRepo) GetByID(id uuid.UUID) (*domain.Branch, error) {
	var branch domain.Branch
	err := r.db.First(&branch, "id = ?", id).Error
	return &branch, err
}

func (r *branchRepo) List() ([]domain.Branch, error) {
	var branches []domain.Branch
	err := r.db.Order("created_at asc").Find(&branches).Error
	return branches, err
}

func (r *branchRepo) Create(branch *domain.Branch) error {
	return r.db.Create(branch).Error
}

func (r *branchRepo) Update(branch *domain.Branch) error {
	return r.db.Save(branch).Error
}

func (r *branchRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Branch{}, "id = ?", id).Error
}

func (r *branchRepo) UpdateOccupiedSpaces(id uuid.UUID, occupied int) error {
	return r.db.Model(&domain.Branch{}).Where("id = ?", id).Update("occupied_spaces", occupied).Error
}

// ---------------- CAMERA REPO ----------------
type CameraRepository interface {
	List() ([]domain.Camera, error)
	ListByBranch(branchID uuid.UUID) ([]domain.Camera, error)
	GetByID(id uuid.UUID) (*domain.Camera, error)
	Create(cam *domain.Camera) error
	Update(cam *domain.Camera) error
	GetByCode(code string) (*domain.Camera, error)
	GetByIP(ip string) (*domain.Camera, error)
	UpdateHeartbeat(id uuid.UUID) error
	Delete(id uuid.UUID) error
}

type cameraRepo struct {
	db *gorm.DB
}

func NewCameraRepository(db *gorm.DB) CameraRepository {
	return &cameraRepo{db: db}
}

func (r *cameraRepo) List() ([]domain.Camera, error) {
	var cams []domain.Camera
	err := r.db.Order("name asc").Find(&cams).Error
	return cams, err
}

func (r *cameraRepo) ListByBranch(branchID uuid.UUID) ([]domain.Camera, error) {
	var cams []domain.Camera
	err := r.db.Where("branch_id = ?", branchID).Order("name asc").Find(&cams).Error
	return cams, err
}

func (r *cameraRepo) GetByID(id uuid.UUID) (*domain.Camera, error) {
	var cam domain.Camera
	err := r.db.First(&cam, "id = ?", id).Error
	return &cam, err
}

func (r *cameraRepo) GetByCode(code string) (*domain.Camera, error) {
	var cam domain.Camera
	err := r.db.First(&cam, "LOWER(code) = LOWER(?)", code).Error
	if err != nil {
		return nil, err
	}
	return &cam, nil
}

func (r *cameraRepo) GetByIP(ip string) (*domain.Camera, error) {
	var cam domain.Camera
	err := r.db.First(&cam, "ip_address = ?", ip).Error
	if err != nil {
		return nil, err
	}
	return &cam, nil
}

func (r *cameraRepo) Create(cam *domain.Camera) error {
	return r.db.Create(cam).Error
}

func (r *cameraRepo) Update(cam *domain.Camera) error {
	return r.db.Save(cam).Error
}

func (r *cameraRepo) UpdateHeartbeat(id uuid.UUID) error {
	return r.db.Model(&domain.Camera{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":         domain.CameraStatusOnline,
		"last_heartbeat": time.Now(),
	}).Error
}

func (r *cameraRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Camera{}, "id = ?", id).Error
}

// ---------------- BARRIER REPO ----------------
type BarrierRepository interface {
	List() ([]domain.Barrier, error)
	GetByID(id uuid.UUID) (*domain.Barrier, error)
	GetByDirection(direction domain.DirectionType) (*domain.Barrier, error)
	GetByBranchDirection(branchID uuid.UUID, direction domain.DirectionType) (*domain.Barrier, error)
	Create(barrier *domain.Barrier) error
	Update(barrier *domain.Barrier) error
	Delete(id uuid.UUID) error
	UpdateStatus(id uuid.UUID, status domain.BarrierStatus) error
}

type barrierRepo struct {
	db *gorm.DB
}

func NewBarrierRepository(db *gorm.DB) BarrierRepository {
	return &barrierRepo{db: db}
}

func (r *barrierRepo) List() ([]domain.Barrier, error) {
	var barriers []domain.Barrier
	err := r.db.Order("name asc").Find(&barriers).Error
	return barriers, err
}

func (r *barrierRepo) GetByID(id uuid.UUID) (*domain.Barrier, error) {
	var b domain.Barrier
	err := r.db.First(&b, "id = ?", id).Error
	return &b, err
}

func (r *barrierRepo) GetByDirection(direction domain.DirectionType) (*domain.Barrier, error) {
	var b domain.Barrier
	err := r.db.First(&b, "direction = ?", direction).Error
	return &b, err
}

func (r *barrierRepo) GetByBranchDirection(branchID uuid.UUID, direction domain.DirectionType) (*domain.Barrier, error) {
	var b domain.Barrier
	err := r.db.Order("created_at asc").First(&b, "branch_id = ? AND direction = ?", branchID, direction).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *barrierRepo) Create(barrier *domain.Barrier) error {
	return r.db.Create(barrier).Error
}

func (r *barrierRepo) Update(barrier *domain.Barrier) error {
	return r.db.Save(barrier).Error
}

func (r *barrierRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Barrier{}, "id = ?", id).Error
}

func (r *barrierRepo) UpdateStatus(id uuid.UUID, status domain.BarrierStatus) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status": status,
	}
	if status == domain.BarrierOpen {
		updates["last_triggered_at"] = &now
	}
	return r.db.Model(&domain.Barrier{}).Where("id = ?", id).Updates(updates).Error
}

// ---------------- AGENT REPO ----------------
type AgentRepository interface {
	List() ([]domain.Agent, error)
	GetByID(id uuid.UUID) (*domain.Agent, error)
	GetByTokenHash(hash string) (*domain.Agent, error)
	Create(agent *domain.Agent) error
	Update(agent *domain.Agent) error
	// Delete removes the agent and unbinds its barriers (they fall back to
	// being driven by the backend directly).
	Delete(id uuid.UUID) error
	// Touch records a connection: last seen time and what the agent reported.
	Touch(id uuid.UUID, version, hostname, remoteAddr string) error
}

type agentRepo struct {
	db *gorm.DB
}

func NewAgentRepository(db *gorm.DB) AgentRepository {
	return &agentRepo{db: db}
}

func (r *agentRepo) List() ([]domain.Agent, error) {
	var agents []domain.Agent
	err := r.db.Order("name asc").Find(&agents).Error
	return agents, err
}

func (r *agentRepo) GetByID(id uuid.UUID) (*domain.Agent, error) {
	var a domain.Agent
	if err := r.db.First(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *agentRepo) GetByTokenHash(hash string) (*domain.Agent, error) {
	var a domain.Agent
	if err := r.db.First(&a, "token_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *agentRepo) Create(agent *domain.Agent) error {
	return r.db.Create(agent).Error
}

func (r *agentRepo) Update(agent *domain.Agent) error {
	return r.db.Save(agent).Error
}

func (r *agentRepo) Delete(id uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Barrier{}).Where("agent_id = ?", id).Update("agent_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&domain.Agent{}, "id = ?", id).Error
	})
}

func (r *agentRepo) Touch(id uuid.UUID, version, hostname, remoteAddr string) error {
	now := time.Now()
	return r.db.Model(&domain.Agent{}).Where("id = ?", id).Updates(map[string]any{
		"last_seen_at": &now, "version": version, "hostname": hostname, "remote_addr": remoteAddr,
	}).Error
}

// ---------------- CLIENT REPO ----------------
type ClientRepository interface {
	List() ([]domain.Client, error)
	GetByID(id uuid.UUID) (*domain.Client, error)
	GetByPlate(plate string) (*domain.Client, error)
	Create(client *domain.Client) error
	Update(client *domain.Client) error
	Delete(id uuid.UUID) error
}

type clientRepo struct {
	db *gorm.DB
}

func NewClientRepository(db *gorm.DB) ClientRepository {
	return &clientRepo{db: db}
}

func (r *clientRepo) List() ([]domain.Client, error) {
	var clients []domain.Client
	err := r.db.Order("created_at desc").Find(&clients).Error
	return clients, err
}

func (r *clientRepo) GetByID(id uuid.UUID) (*domain.Client, error) {
	var client domain.Client
	err := r.db.First(&client, "id = ?", id).Error
	return &client, err
}

func (r *clientRepo) GetByPlate(plate string) (*domain.Client, error) {
	cleanPlate := strings.ToUpper(strings.ReplaceAll(plate, " ", ""))
	var client domain.Client
	err := r.db.Where("REPLACE(UPPER(plate_number), ' ', '') = ? AND active = ?", cleanPlate, true).First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (r *clientRepo) Create(client *domain.Client) error {
	return r.db.Create(client).Error
}

func (r *clientRepo) Update(client *domain.Client) error {
	return r.db.Save(client).Error
}

func (r *clientRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Client{}, "id = ?", id).Error
}

// ---------------- TARIFF REPO ----------------
type TariffRepository interface {
	List() ([]domain.Tariff, error)
	GetByID(id uuid.UUID) (*domain.Tariff, error)
	GetDefault() (*domain.Tariff, error)
	Create(tariff *domain.Tariff) error
	Update(tariff *domain.Tariff) error
	Delete(id uuid.UUID) error
}

type tariffRepo struct {
	db *gorm.DB
}

func NewTariffRepository(db *gorm.DB) TariffRepository {
	return &tariffRepo{db: db}
}

func (r *tariffRepo) List() ([]domain.Tariff, error) {
	var tariffs []domain.Tariff
	err := r.db.Order("is_default desc, name asc").Find(&tariffs).Error
	return tariffs, err
}

func (r *tariffRepo) GetByID(id uuid.UUID) (*domain.Tariff, error) {
	var tariff domain.Tariff
	err := r.db.First(&tariff, "id = ?", id).Error
	return &tariff, err
}

func (r *tariffRepo) GetDefault() (*domain.Tariff, error) {
	var tariff domain.Tariff
	err := r.db.Where("is_default = ?", true).First(&tariff).Error
	if err != nil {
		err = r.db.First(&tariff).Error
	}
	return &tariff, err
}

func (r *tariffRepo) Create(tariff *domain.Tariff) error {
	if tariff.IsDefault {
		r.db.Model(&domain.Tariff{}).Where("id != ?", tariff.ID).Update("is_default", false)
	}
	return r.db.Create(tariff).Error
}

func (r *tariffRepo) Update(tariff *domain.Tariff) error {
	if tariff.IsDefault {
		r.db.Model(&domain.Tariff{}).Where("id != ?", tariff.ID).Update("is_default", false)
	}
	return r.db.Save(tariff).Error
}

func (r *tariffRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Tariff{}, "id = ?", id).Error
}

// ---------------- PARKING SESSION REPO ----------------
type SessionFilter struct {
	BranchID    *uuid.UUID
	PlateNumber string
	Status      domain.SessionStatus
	StartDate   *time.Time
	EndDate     *time.Time
	Page        int
	Limit       int
}

type ParkingSessionRepository interface {
	Create(session *domain.ParkingSession) error
	Update(session *domain.ParkingSession) error
	GetByID(id uuid.UUID) (*domain.ParkingSession, error)
	GetBySessionCode(code string) (*domain.ParkingSession, error)
	FindActiveByPlate(plate string) (*domain.ParkingSession, error)
	FindRecentlyClosedByPlate(plate string, since time.Time) (*domain.ParkingSession, error)
	List(filter SessionFilter) ([]domain.ParkingSession, int64, error)
	CountActive() (int64, error)
	CountActiveByBranch(branchID *uuid.UUID) (int64, error)
	CountToday() (int64, error)
	CountTodayByBranch(branchID *uuid.UUID) (int64, error)
	GetAverageDurationMinutes() (float64, error)
	GetAverageDurationByBranch(branchID *uuid.UUID) (float64, error)
	GetVehicleHistory(plate string) ([]domain.ParkingSession, error)
}

type parkingSessionRepo struct {
	db *gorm.DB
}

func NewParkingSessionRepository(db *gorm.DB) ParkingSessionRepository {
	return &parkingSessionRepo{db: db}
}

func (r *parkingSessionRepo) Create(session *domain.ParkingSession) error {
	return r.db.Create(session).Error
}

func (r *parkingSessionRepo) Update(session *domain.ParkingSession) error {
	return r.db.Save(session).Error
}

func (r *parkingSessionRepo) GetByID(id uuid.UUID) (*domain.ParkingSession, error) {
	var session domain.ParkingSession
	err := r.db.Preload("Tariff").Preload("Client").Preload("Payments").First(&session, "id = ?", id).Error
	return &session, err
}

func (r *parkingSessionRepo) GetBySessionCode(code string) (*domain.ParkingSession, error) {
	var session domain.ParkingSession
	err := r.db.Preload("Tariff").Preload("Client").Preload("Payments").First(&session, "session_code = ?", code).Error
	return &session, err
}

func (r *parkingSessionRepo) FindActiveByPlate(plate string) (*domain.ParkingSession, error) {
	cleanPlate := strings.ToUpper(strings.ReplaceAll(plate, " ", ""))
	var session domain.ParkingSession
	err := r.db.Preload("Tariff").Preload("Client").
		Where("REPLACE(UPPER(plate_number), ' ', '') = ? AND status = ?", cleanPlate, domain.SessionActive).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// FindRecentlyClosedByPlate returns the latest session of plate that was paid /
// completed after since (used to let a car that already paid leave).
func (r *parkingSessionRepo) FindRecentlyClosedByPlate(plate string, since time.Time) (*domain.ParkingSession, error) {
	cleanPlate := strings.ToUpper(strings.ReplaceAll(plate, " ", ""))
	var session domain.ParkingSession
	err := r.db.Preload("Tariff").Preload("Client").
		Where("REPLACE(UPPER(plate_number), ' ', '') = ? AND status IN ? AND updated_at >= ?",
			cleanPlate, []domain.SessionStatus{domain.SessionPaid, domain.SessionCompleted}, since).
		Order("updated_at desc").
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *parkingSessionRepo) List(filter SessionFilter) ([]domain.ParkingSession, int64, error) {
	query := r.db.Model(&domain.ParkingSession{}).Preload("Tariff").Preload("Client").Preload("Payments")

	if filter.BranchID != nil {
		query = query.Where("branch_id = ?", *filter.BranchID)
	}
	if filter.PlateNumber != "" {
		clean := strings.ToUpper(strings.ReplaceAll(filter.PlateNumber, " ", ""))
		query = query.Where("REPLACE(UPPER(plate_number), ' ', '') LIKE ?", "%"+clean+"%")
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartDate != nil {
		query = query.Where("entry_time >= ?", filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("entry_time <= ?", filter.EndDate)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var sessions []domain.ParkingSession
	err := query.Order("entry_time desc").Offset(offset).Limit(limit).Find(&sessions).Error
	return sessions, total, err
}

func (r *parkingSessionRepo) CountActive() (int64, error) {
	var count int64
	err := r.db.Model(&domain.ParkingSession{}).Where("status = ?", domain.SessionActive).Count(&count).Error
	return count, err
}

func (r *parkingSessionRepo) CountActiveByBranch(branchID *uuid.UUID) (int64, error) {
	var count int64
	q := r.db.Model(&domain.ParkingSession{}).Where("status = ?", domain.SessionActive)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	err := q.Count(&count).Error
	return count, err
}

func (r *parkingSessionRepo) CountToday() (int64, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour)
	var count int64
	err := r.db.Model(&domain.ParkingSession{}).Where("entry_time >= ?", startOfDay).Count(&count).Error
	return count, err
}

func (r *parkingSessionRepo) CountTodayByBranch(branchID *uuid.UUID) (int64, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour)
	var count int64
	q := r.db.Model(&domain.ParkingSession{}).Where("entry_time >= ?", startOfDay)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	err := q.Count(&count).Error
	return count, err
}

func (r *parkingSessionRepo) GetAverageDurationByBranch(branchID *uuid.UUID) (float64, error) {
	var avg float64
	q := r.db.Model(&domain.ParkingSession{}).
		Where("status IN ? AND duration_min > 0", []domain.SessionStatus{domain.SessionCompleted, domain.SessionPaid})
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	err := q.Select("COALESCE(AVG(duration_min), 0)").Scan(&avg).Error
	return avg, err
}

func (r *parkingSessionRepo) GetAverageDurationMinutes() (float64, error) {
	var avg float64
	err := r.db.Model(&domain.ParkingSession{}).
		Where("status IN ? AND duration_min > 0", []domain.SessionStatus{domain.SessionCompleted, domain.SessionPaid}).
		Select("COALESCE(AVG(duration_min), 0)").
		Scan(&avg).Error
	return avg, err
}

func (r *parkingSessionRepo) GetVehicleHistory(plate string) ([]domain.ParkingSession, error) {
	clean := strings.ToUpper(strings.ReplaceAll(plate, " ", ""))
	var sessions []domain.ParkingSession
	err := r.db.Preload("Tariff").Preload("Payments").
		Where("REPLACE(UPPER(plate_number), ' ', '') = ?", clean).
		Order("entry_time desc").
		Find(&sessions).Error
	return sessions, err
}

// ---------------- PAYMENT REPO ----------------
type PaymentRepository interface {
	Create(payment *domain.Payment) error
	GetByID(id uuid.UUID) (*domain.Payment, error)
	List(page, limit int) ([]domain.Payment, int64, error)
	GetTodayRevenue() (float64, error)
	GetTodayRevenueByBranch(branchID *uuid.UUID) (float64, error)
	GetRecentPayments(limit int) ([]domain.Payment, error)
	GetRevenueByDays(days int) ([]map[string]interface{}, error)
	GetRevenueByDaysByBranch(branchID *uuid.UUID, days int) ([]map[string]interface{}, error)
}

type paymentRepo struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) PaymentRepository {
	return &paymentRepo{db: db}
}

func (r *paymentRepo) Create(payment *domain.Payment) error {
	return r.db.Create(payment).Error
}

func (r *paymentRepo) GetByID(id uuid.UUID) (*domain.Payment, error) {
	var p domain.Payment
	err := r.db.First(&p, "id = ?", id).Error
	return &p, err
}

func (r *paymentRepo) List(page, limit int) ([]domain.Payment, int64, error) {
	var total int64
	r.db.Model(&domain.Payment{}).Count(&total)

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var payments []domain.Payment
	err := r.db.Order("paid_at desc").Offset(offset).Limit(limit).Find(&payments).Error
	return payments, total, err
}

func (r *paymentRepo) GetTodayRevenue() (float64, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour)
	var total float64
	err := r.db.Model(&domain.Payment{}).
		Where("paid_at >= ?", startOfDay).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&total).Error
	return total, err
}

func (r *paymentRepo) GetTodayRevenueByBranch(branchID *uuid.UUID) (float64, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour)
	var total float64
	q := r.db.Model(&domain.Payment{}).Where("paid_at >= ?", startOfDay)
	if branchID != nil {
		// Payment ga branch_id yo'q, session orqali filtrlash
		q = q.Joins("JOIN parking_sessions ON parking_sessions.id = payments.session_id").
			Where("parking_sessions.branch_id = ?", *branchID)
	}
	err := q.Select("COALESCE(SUM(payments.amount), 0)").Scan(&total).Error
	return total, err
}

func (r *paymentRepo) GetRecentPayments(limit int) ([]domain.Payment, error) {
	var payments []domain.Payment
	err := r.db.Order("paid_at desc").Limit(limit).Find(&payments).Error
	return payments, err
}

func (r *paymentRepo) GetRevenueByDays(days int) ([]map[string]interface{}, error) {
	return r.GetRevenueByDaysByBranch(nil, days)
}

func (r *paymentRepo) GetRevenueByDaysByBranch(branchID *uuid.UUID, days int) ([]map[string]interface{}, error) {
	since := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)

	type DailyRev struct {
		Date  string  `json:"date"`
		Total float64 `json:"total"`
		Count int64   `json:"count"`
	}

	dateExpr := "TO_CHAR(payments.paid_at, 'YYYY-MM-DD')"
	if r.db.Dialector.Name() == "sqlite" {
		dateExpr = "strftime('%Y-%m-%d', payments.paid_at)"
	}

	q := r.db.Model(&domain.Payment{}).Where("payments.paid_at >= ?", since)
	if branchID != nil {
		q = q.Joins("JOIN parking_sessions ON parking_sessions.id = payments.session_id").
			Where("parking_sessions.branch_id = ?", *branchID)
	}

	var results []DailyRev
	err := q.Select(dateExpr + " as date, SUM(payments.amount) as total, COUNT(payments.id) as count").
		Group(dateExpr).
		Order("date asc").
		Scan(&results).Error

	var output []map[string]interface{}
	for _, r := range results {
		output = append(output, map[string]interface{}{
			"date":  r.Date,
			"total": r.Total,
			"count": r.Count,
		})
	}
	return output, err
}

// ---------------- AUDIT LOG REPO ----------------
type AuditLogRepository interface {
	Create(log *domain.AuditLog) error
	List(limit int) ([]domain.AuditLog, error)
}

type auditLogRepo struct {
	db *gorm.DB
}

func NewAuditLogRepository(db *gorm.DB) AuditLogRepository {
	return &auditLogRepo{db: db}
}

func (r *auditLogRepo) Create(log *domain.AuditLog) error {
	return r.db.Create(log).Error
}

func (r *auditLogRepo) List(limit int) ([]domain.AuditLog, error) {
	var logs []domain.AuditLog
	err := r.db.Order("created_at desc").Limit(limit).Find(&logs).Error
	return logs, err
}

// ---------------- PAYMENT REQUEST REPO ----------------
type PaymentRequestRepository interface {
	Create(pr *domain.PaymentRequest) error
	Update(pr *domain.PaymentRequest) error
	GetByID(id uuid.UUID) (*domain.PaymentRequest, error)
	ListPending() ([]domain.PaymentRequest, error)
	List(status string, limit int) ([]domain.PaymentRequest, error)
	GetPendingBySession(sessionID uuid.UUID) (*domain.PaymentRequest, error)
	GetByBillNumber(bill string) (*domain.PaymentRequest, error)
	// MarkStatus atomically moves a request from one status to another and
	// reports whether this call won (protects against double-completion).
	MarkStatus(id uuid.UUID, from, to domain.PaymentRequestStatus) (bool, error)
}

type paymentRequestRepo struct {
	db *gorm.DB
}

func NewPaymentRequestRepository(db *gorm.DB) PaymentRequestRepository {
	return &paymentRequestRepo{db: db}
}

func (r *paymentRequestRepo) Create(pr *domain.PaymentRequest) error {
	return r.db.Create(pr).Error
}

func (r *paymentRequestRepo) Update(pr *domain.PaymentRequest) error {
	return r.db.Save(pr).Error
}

func (r *paymentRequestRepo) GetByID(id uuid.UUID) (*domain.PaymentRequest, error) {
	var pr domain.PaymentRequest
	if err := r.db.First(&pr, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &pr, nil
}

func (r *paymentRequestRepo) ListPending() ([]domain.PaymentRequest, error) {
	var list []domain.PaymentRequest
	err := r.db.Where("status = ?", domain.PaymentRequestPending).Order("created_at asc").Find(&list).Error
	return list, err
}

func (r *paymentRequestRepo) List(status string, limit int) ([]domain.PaymentRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.db.Order("created_at desc").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var list []domain.PaymentRequest
	err := q.Find(&list).Error
	return list, err
}

func (r *paymentRequestRepo) GetPendingBySession(sessionID uuid.UUID) (*domain.PaymentRequest, error) {
	var pr domain.PaymentRequest
	err := r.db.Where("session_id = ? AND status = ?", sessionID, domain.PaymentRequestPending).
		Order("created_at desc").First(&pr).Error
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

func (r *paymentRequestRepo) GetByBillNumber(bill string) (*domain.PaymentRequest, error) {
	var pr domain.PaymentRequest
	if err := r.db.First(&pr, "bill_number = ?", bill).Error; err != nil {
		return nil, err
	}
	return &pr, nil
}

func (r *paymentRequestRepo) MarkStatus(id uuid.UUID, from, to domain.PaymentRequestStatus) (bool, error) {
	res := r.db.Model(&domain.PaymentRequest{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]interface{}{"status": to, "updated_at": time.Now()})
	return res.RowsAffected == 1, res.Error
}

// ---- Click transactions ----

type ClickTransactionRepository interface {
	Create(tx *domain.ClickTransaction) error
	Update(tx *domain.ClickTransaction) error
	GetByID(id int64) (*domain.ClickTransaction, error)
	GetByClickTransID(clickTransID int64) (*domain.ClickTransaction, error)
}

type clickTxRepo struct {
	db *gorm.DB
}

func NewClickTransactionRepository(db *gorm.DB) ClickTransactionRepository {
	return &clickTxRepo{db: db}
}

func (r *clickTxRepo) Create(tx *domain.ClickTransaction) error {
	return r.db.Create(tx).Error
}

func (r *clickTxRepo) Update(tx *domain.ClickTransaction) error {
	return r.db.Save(tx).Error
}

func (r *clickTxRepo) GetByID(id int64) (*domain.ClickTransaction, error) {
	var tx domain.ClickTransaction
	if err := r.db.First(&tx, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *clickTxRepo) GetByClickTransID(clickTransID int64) (*domain.ClickTransaction, error) {
	var tx domain.ClickTransaction
	if err := r.db.First(&tx, "click_trans_id = ?", clickTransID).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}
