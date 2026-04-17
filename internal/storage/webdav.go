package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"machring/internal/persist"
)

type WebDAV struct {
	cfg      persist.StorageConfig
	client   *http.Client
	endpoint *url.URL
}

func NewWebDAV(cfg persist.StorageConfig) (*WebDAV, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("webdav endpoint is required")
	}
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"))
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, errors.New("webdav endpoint must include scheme and host")
	}
	return &WebDAV{
		cfg:      cfg,
		client:   &http.Client{Timeout: 45 * time.Second},
		endpoint: endpoint,
	}, nil
}

func (w *WebDAV) Put(ctx context.Context, key string, reader io.Reader) (Object, error) {
	req, err := w.newRequest(ctx, http.MethodPut, key, reader)
	if err != nil {
		return Object{}, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Object{}, err
	}
	defer drainAndClose(resp.Body)
	if err := requireStatus(resp, http.StatusCreated, http.StatusOK, http.StatusNoContent); err != nil {
		return Object{}, err
	}
	return Object{
		Key: key,
		URL: w.PublicURL(key),
	}, nil
}

func (w *WebDAV) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := w.newRequest(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer drainAndClose(resp.Body)
		return nil, fmt.Errorf("webdav get failed: %s", resp.Status)
	}
	return resp.Body, nil
}

func (w *WebDAV) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return w.Open(ctx, key)
}

func (w *WebDAV) Delete(ctx context.Context, key string) error {
	req, err := w.newRequest(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	return requireStatus(resp, http.StatusNoContent, http.StatusOK, http.StatusAccepted, http.StatusNotFound)
}

func (w *WebDAV) Stat(ctx context.Context, key string) (Stat, error) {
	req, err := w.newRequest(ctx, http.MethodHead, key, nil)
	if err != nil {
		return Stat{}, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Stat{}, err
	}
	defer drainAndClose(resp.Body)
	if err := requireStatus(resp, http.StatusOK); err != nil {
		return Stat{}, err
	}
	return Stat{
		Key:          key,
		Size:         headerInt64(resp.Header.Get("Content-Length")),
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

func (w *WebDAV) Exists(ctx context.Context, key string) (bool, error) {
	req, err := w.newRequest(ctx, http.MethodHead, key, nil)
	if err != nil {
		return false, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return false, err
	}
	defer drainAndClose(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("webdav exists check failed: %s", resp.Status)
	}
}

func (w *WebDAV) PublicURL(key string) string {
	key = JoinBasePath(w.cfg.BasePath, key)
	if base := strings.TrimRight(strings.TrimSpace(w.cfg.PublicBaseURL), "/"); base != "" {
		return base + "/" + strings.TrimLeft(key, "/")
	}
	targetURL := *w.endpoint
	targetURL.Path = joinURLPath(targetURL.Path, key)
	return targetURL.String()
}

func (w *WebDAV) HealthCheck(ctx context.Context) error {
	req, err := w.newRequest(ctx, http.MethodOptions, "", nil)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	return requireStatus(resp, http.StatusOK, http.StatusNoContent, http.StatusCreated)
}

func (w *WebDAV) newRequest(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	targetURL := *w.endpoint
	targetURL.Path = joinURLPath(targetURL.Path, JoinBasePath(w.cfg.BasePath, key))
	req, err := http.NewRequestWithContext(ctx, method, targetURL.String(), body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(w.cfg.Username) != "" {
		req.SetBasicAuth(w.cfg.Username, w.cfg.Password)
	}
	return req, nil
}
