package storage

import (
	"context"
	"errors"
	"path"
	"strings"

	"machring/internal/persist"
)

type Manager struct {
	local Store
}

func NewManager(local Store) *Manager {
	return &Manager{local: local}
}

func (m *Manager) Resolve(ctx context.Context, cfg persist.StorageConfig) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "", "local":
		return m.local, nil
	case "s3":
		return NewS3(ctx, cfg)
	case "webdav":
		return NewWebDAV(cfg)
	default:
		return nil, errors.New("unsupported storage type")
	}
}

func JoinBasePath(basePath, key string) string {
	basePath = strings.Trim(strings.ReplaceAll(basePath, "\\", "/"), "/")
	key = strings.TrimLeft(key, "/")
	if basePath == "" {
		return key
	}
	return path.Join(basePath, key)
}
