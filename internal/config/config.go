package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DBDriver      string // "postgres" or "sqlite"
	DBSource      string
	JWTSecret     string
	UploadDir     string
	FrontendURL   string
	DefaultBranch string

	// ANPR webhook protection (optional). When set, cameras / capture service
	// must send it as ?token=… or X-ANPR-Token header.
	ANPRWebhookToken string

	// Exit flow
	ExitGraceMinutes    int // a session paid this recently opens the exit barrier without new charge
	PaymentQRTTLMinutes int // how long a generated QR stays payable
	PaymentPollSeconds  int // how often pending Munis payments are checked

	// Munis (UzQR). Mode: "live" | "mock" | "off". Empty = live when configured, else off.
	MunisMode        string
	MunisApiURL      string
	MunisApiUsername string
	MunisApiPassword string
	MunisQRApiURL    string
	MunisQRAuthToken string
	MunisOriginalQR  string
}

func LoadConfig() *Config {
	_ = godotenv.Load()

	port := getEnv("PORT", "8080")
	dbDriver := getEnv("DB_DRIVER", "sqlite")
	dbSource := getEnv("DB_SOURCE", "parking.db")
	jwtSecret := getEnv("JWT_SECRET", "super-secret-smart-parking-jwt-key-2026")
	uploadDir := getEnv("UPLOAD_DIR", "./uploads")
	frontendURL := getEnv("FRONTEND_URL", "http://localhost:5173")
	defaultBranch := getEnv("DEFAULT_BRANCH", "Markaziy Filial")

	// Ensure upload dir exists
	_ = os.MkdirAll(uploadDir, 0755)

	return &Config{
		Port:          port,
		DBDriver:      dbDriver,
		DBSource:      dbSource,
		JWTSecret:     jwtSecret,
		UploadDir:     uploadDir,
		FrontendURL:   frontendURL,
		DefaultBranch: defaultBranch,

		ANPRWebhookToken: getEnv("ANPR_WEBHOOK_TOKEN", ""),

		ExitGraceMinutes:    getEnvAsInt("EXIT_GRACE_MINUTES", 15),
		PaymentQRTTLMinutes: getEnvAsInt("PAYMENT_QR_TTL_MINUTES", 15),
		PaymentPollSeconds:  getEnvAsInt("PAYMENT_POLL_SECONDS", 3),

		MunisMode:        getEnv("MUNIS_MODE", ""),
		MunisApiURL:      getEnv("MUNIS_API_URL", ""),
		MunisApiUsername: getEnv("MUNIS_API_USERNAME", ""),
		MunisApiPassword: getEnv("MUNIS_API_PASSWORD", ""),
		MunisQRApiURL:    getEnv("MUNIS_QR_API_URL", ""),
		MunisQRAuthToken: getEnv("MUNIS_QR_AUTH_TOKEN", ""),
		MunisOriginalQR:  getEnv("MUNIS_ORIGINAL_QR", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return defaultVal
}
