package storage

import (
	"context"
	"io"
	"time"
)

type Object struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

type Stat struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	ContentType  string `json:"contentType"`
	LastModified string `json:"lastModified"`
}

type Store interface {
	Put(ctx context.Context, key string, reader io.Reader) (Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (Stat, error)
	Exists(ctx context.Context, key string) (bool, error)
	PublicURL(key string) string
	HealthCheck(ctx context.Context) error
}

type RedirectOptions struct {
	Method             string
	Expires            time.Duration
	ContentType        string
	CacheControl       string
	ContentDisposition string
}

type Redirector interface {
	RedirectURL(ctx context.Context, key string, options RedirectOptions) (string, error)
}
