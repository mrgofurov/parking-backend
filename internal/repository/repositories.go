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
	Client         ClientRepository
	Tariff         TariffRepository
	ParkingSession ParkingSessionRepository
	Payment        PaymentRepository
	AuditLog       AuditLogRepository
}

func NewRepositoryContainer(db *gorm.DB) *RepositoryContainer {
	return &RepositoryContainer{
		User:           NewUserRepository(db),
		Branch:         NewBranchRepository(db),
		Camera:         NewCameraRepository(db),
		Barrier:        NewBarrierRepository(db),
		Client:         NewClientRepository(db),
		Tariff:         NewTariffRepository(db),
		ParkingSession: NewParkingSessionRepository(db),
		Payment:        NewPaymentRepository(db),
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
	Update(branch *domain.Branch) error
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

func (r *branchRepo) Update(branch *domain.Branch) error {
	return r.db.Save(branch).Error
}

func (r *branchRepo) UpdateOccupiedSpaces(id uuid.UUID, occupied int) error {
	return r.db.Model(&domain.Branch{}).Where("id = ?", id).Update("occupied_spaces", occupied).Error
}

// ---------------- CAMERA REPO ----------------
type CameraRepository interface {
	List() ([]domain.Camera, error)
	GetByID(id uuid.UUID) (*domain.Camera, error)
	Create(cam *domain.Camera) error
	Update(cam *domain.Camera) error
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

func (r *cameraRepo) GetByID(id uuid.UUID) (*domain.Camera, error) {
	var cam domain.Camera
	err := r.db.First(&cam, "id = ?", id).Error
	return &cam, err
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
	Update(barrier *domain.Barrier) error
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

func (r *barrierRepo) Update(barrier *domain.Barrier) error {
	return r.db.Save(barrier).Error
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
	List(filter SessionFilter) ([]domain.ParkingSession, int64, error)
	CountActive() (int64, error)
	CountToday() (int64, error)
	GetAverageDurationMinutes() (float64, error)
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

func (r *parkingSessionRepo) List(filter SessionFilter) ([]domain.ParkingSession, int64, error) {
	query := r.db.Model(&domain.ParkingSession{}).Preload("Tariff").Preload("Client").Preload("Payments")

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

func (r *parkingSessionRepo) CountToday() (int64, error) {
	startOfDay := time.Now().Truncate(24 * time.Hour)
	var count int64
	err := r.db.Model(&domain.ParkingSession{}).Where("entry_time >= ?", startOfDay).Count(&count).Error
	return count, err
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
	GetRecentPayments(limit int) ([]domain.Payment, error)
	GetRevenueByDays(days int) ([]map[string]interface{}, error)
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

func (r *paymentRepo) GetRecentPayments(limit int) ([]domain.Payment, error) {
	var payments []domain.Payment
	err := r.db.Order("paid_at desc").Limit(limit).Find(&payments).Error
	return payments, err
}

func (r *paymentRepo) GetRevenueByDays(days int) ([]map[string]interface{}, error) {
	since := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	
	type DailyRev struct {
		Date  string  `json:"date"`
		Total float64 `json:"total"`
		Count int64   `json:"count"`
	}

	dateExpr := "TO_CHAR(paid_at, 'YYYY-MM-DD')"
	if r.db.Dialector.Name() == "sqlite" {
		dateExpr = "strftime('%Y-%m-%d', paid_at)"
	}

	var results []DailyRev
	err := r.db.Model(&domain.Payment{}).
		Where("paid_at >= ?", since).
		Select(dateExpr + " as date, SUM(amount) as total, COUNT(id) as count").
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
