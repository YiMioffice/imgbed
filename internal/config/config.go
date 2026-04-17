package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	HTTPAddr      string
	DataDir       string
	DatabasePath  string
	UploadDir     string
	TempDir       string
	PublicBaseURL string
	SiteName      string
}

func LoadFromEnv() Config {
	dataDir := getenv("MACHRING_DATA_DIR", "./data")
	uploadDir := getenv("MACHRING_UPLOAD_DIR", filepath.Join(dataDir, "uploads"))

	return Config{
		HTTPAddr:      getenv("MACHRING_HTTP_ADDR", ":8080"),
		DataDir:       dataDir,
		DatabasePath:  getenv("MACHRING_DATABASE_PATH", filepath.Join(dataDir, "machring.db")),
		UploadDir:     uploadDir,
		TempDir:       getenv("MACHRING_TEMP_DIR", filepath.Join(dataDir, "tmp")),
		PublicBaseURL: getenv("MACHRING_PUBLIC_BASE_URL", "http://localhost:8080"),
		SiteName:      getenv("MACHRING_SITE_NAME", "马赫环"),
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
