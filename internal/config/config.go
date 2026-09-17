package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         string
	DBDriver     string // "postgres" or "sqlite"
	DBSource     string
	JWTSecret    string
	UploadDir    string
	FrontendURL  string
	DefaultBranch string
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
