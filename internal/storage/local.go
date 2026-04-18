package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Local struct {
	root          string
	publicBaseURL string
}

func NewLocal(root, publicBaseURL string) *Local {
	return &Local{
		root:          root,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func (s *Local) Put(_ context.Context, key string, reader io.Reader) (Object, error) {
	target, err := s.localPath(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Object{}, err
	}

	file, err := os.Create(target)
	if err != nil {
		return Object{}, err
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		return Object{}, err
	}

	return Object{
		Key: key,
		URL: s.PublicURL(key),
	}, nil
}

func (s *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	target, err := s.localPath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

func (s *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.Open(ctx, key)
}

func (s *Local) Delete(_ context.Context, key string) error {
	target, err := s.localPath(key)
	if err != nil {
		return err
	}
	return os.Remove(target)
}

func (s *Local) Stat(_ context.Context, key string) (Stat, error) {
	target, err := s.localPath(key)
	if err != nil {
		return Stat{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Stat{}, err
	}
	return Stat{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Local) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.Stat(ctx, key)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *Local) PublicURL(key string) string {
	return s.publicBaseURL + "/assets/" + key
}

func (s *Local) HealthCheck(_ context.Context) error {
	return os.MkdirAll(s.root, 0o755)
}

func (s *Local) localPath(key string) (string, error) {
	key = strings.TrimLeft(strings.ReplaceAll(strings.TrimSpace(key), "\\", "/"), "/")
	if key == "" {
		return "", errors.New("storage key is required")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("invalid storage key")
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("invalid storage key")
	}
	return target, nil
}
