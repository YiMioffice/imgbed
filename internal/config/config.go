package config

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	HTTPAddr         string
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	DataDir          string
	DatabasePath     string
	UploadDir        string
	TempDir          string
	PublicBaseURL    string
	SiteName         string
}

func LoadFromEnv() Config {
	dataDir := getenv("MACHRING_DATA_DIR", "./data")
	uploadDir := getenv("MACHRING_UPLOAD_DIR", filepath.Join(dataDir, "uploads"))

	return Config{
		HTTPAddr:         getenv("MACHRING_HTTP_ADDR", ":8080"),
		HTTPReadTimeout:  getenvDuration("MACHRING_HTTP_READ_TIMEOUT", 30*time.Minute),
		HTTPWriteTimeout: getenvDuration("MACHRING_HTTP_WRITE_TIMEOUT", 30*time.Minute),
		DataDir:          dataDir,
		DatabasePath:     getenv("MACHRING_DATABASE_PATH", filepath.Join(dataDir, "machring.db")),
		UploadDir:        uploadDir,
		TempDir:          getenv("MACHRING_TEMP_DIR", filepath.Join(dataDir, "tmp")),
		PublicBaseURL:    getenv("MACHRING_PUBLIC_BASE_URL", "http://localhost:8080"),
		SiteName:         getenv("MACHRING_SITE_NAME", "马赫环"),
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return fallback
	}
	return duration
}
