package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"machring/internal/resource"
)

func TestWebDAVStorageConfigUploadAndServe(t *testing.T) {
	api := testAPI(t, true)
	server := newMemoryObjectServer("webdav")
	defer server.Close()

	putStorageConfig(t, api, "webdav-default", `{
		"type":"webdav",
		"name":"WebDAV 存储",
		"endpoint":"`+server.URL()+`",
		"basePath":"uploads",
		"isDefault":true
	}`)

	healthReq := httptest.NewRequest(http.MethodPost, "/api/v1/storage-configs/health-check", bytes.NewBufferString(`{
		"id":"webdav-default",
		"type":"webdav",
		"name":"WebDAV 存储",
		"endpoint":"`+server.URL()+`",
		"basePath":"uploads",
		"isDefault":true
	}`))
	addAdminCookie(t, api, healthReq)
	healthReq.Header.Set("Content-Type", "application/json")
	healthRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d; body: %s", healthRec.Code, http.StatusOK, healthRec.Body.String())
	}

	resourceID, record := uploadTestPNGRecord(t, api)
	if record.StorageDriver != "webdav-default" {
		t.Fatalf("storage driver = %q, want %q", record.StorageDriver, "webdav-default")
	}
	if got := server.objectCount(); got == 0 {
		t.Fatalf("expected uploaded object in webdav server, got %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
		t.Fatalf("served body mismatch: %v", rec.Body.Bytes())
	}
}

func TestS3CompatibleStorageUploadAndServe(t *testing.T) {
	api := testAPI(t, true)
	server := newMemoryObjectServer("s3")
	defer server.Close()

	putStorageConfig(t, api, "s3-default", `{
		"type":"s3",
		"name":"S3 存储",
		"endpoint":"`+server.URL()+`",
		"region":"auto",
		"bucket":"assets",
		"accessKeyId":"test-access",
		"secretAccessKey":"test-secret",
		"usePathStyle":true,
		"basePath":"uploads",
		"isDefault":true
	}`)

	resourceID, record := uploadTestPNGRecord(t, api)
	if record.StorageDriver != "s3-default" {
		t.Fatalf("storage driver = %q, want %q", record.StorageDriver, "s3-default")
	}
	if got := server.objectCount(); got == 0 {
		t.Fatalf("expected uploaded object in s3 server, got %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
		t.Fatalf("served body mismatch: %v", rec.Body.Bytes())
	}
}

func putStorageConfig(t *testing.T, api *API, id, body string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/storage-configs/"+id, bytes.NewBufferString(body))
	addAdminCookie(t, api, req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save storage config status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func uploadTestPNGRecord(t *testing.T, api *API) (string, resource.Record) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(tinyPNG); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Resource.ID, payload.Resource
}

type memoryObjectServer struct {
	kind   string
	server *httptest.Server
	mu     sync.RWMutex
	data   map[string][]byte
}

func newMemoryObjectServer(kind string) *memoryObjectServer {
	s := &memoryObjectServer{
		kind: kind,
		data: make(map[string][]byte),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

func (s *memoryObjectServer) URL() string {
	return s.server.URL
}

func (s *memoryObjectServer) Close() {
	s.server.Close()
}

func (s *memoryObjectServer) objectCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *memoryObjectServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	key, isBucketRequest := s.normalizePath(r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		if isBucketRequest {
			w.WriteHeader(http.StatusOK)
			return
		}
		s.mu.RLock()
		body, ok := s.data[key]
		s.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.data[key] = body
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		s.mu.RLock()
		body, ok := s.data[key]
		s.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.data, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *memoryObjectServer) normalizePath(r *http.Request) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(r.URL.Path), "/")
	if trimmed == "" {
		return "", true
	}
	parts := strings.Split(trimmed, "/")
	if s.kind == "s3" {
		if len(parts) == 1 {
			return "", true
		}
		return strings.Join(parts[1:], "/"), false
	}
	return trimmed, false
}
