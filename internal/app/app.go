package app

import (
	"context"
	"os"
	"time"

	"machring/internal/auth"
	"machring/internal/config"
	"machring/internal/persist"
	"machring/internal/policy"
	"machring/internal/resource"
	"machring/internal/storage"
)

type App struct {
	Config      config.Config
	Storage     storage.Store
	Storages    *storage.Manager
	PolicyStore policy.Store
	Data        persist.DataStore
	Detector    resource.Detector
	Auth        *auth.Service
}

func New(cfg config.Config) (*App, error) {
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return nil, err
	}
	dataStore, err := persist.NewSQLite(cfg.DatabasePath, policy.DefaultRules())
	if err != nil {
		return nil, err
	}
	if state, err := dataStore.InstallState(context.Background()); err == nil && state.SiteName != "" {
		cfg.SiteName = state.SiteName
	}

	return &App{
		Config:      cfg,
		Storage:     storage.NewLocal(cfg.UploadDir, cfg.PublicBaseURL),
		Storages:    storage.NewManager(storage.NewLocal(cfg.UploadDir, cfg.PublicBaseURL)),
		PolicyStore: dataStore,
		Data:        dataStore,
		Detector:    resource.Detector{},
		Auth:        auth.NewService(dataStore, 24*time.Hour),
	}, nil
}
