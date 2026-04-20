package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	if rec.Code != http.StatusFound {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	redirectLocation := rec.Header().Get("Location")
	if !strings.HasPrefix(redirectLocation, server.URL()+"/assets/uploads/") {
		t.Fatalf("redirect location = %q, want s3 object URL prefix %q", redirectLocation, server.URL()+"/assets/uploads/")
	}
	if !strings.Contains(redirectLocation, "X-Amz-Signature=") {
		t.Fatalf("redirect location is not presigned: %q", redirectLocation)
	}
	if !strings.Contains(redirectLocation, "response-content-type=image%2Fpng") {
		t.Fatalf("redirect location does not preserve content type: %q", redirectLocation)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}
	var detailPayload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&detailPayload); err != nil {
		t.Fatal(err)
	}
	if detailPayload.Detail.Record.TrafficBytes != int64(len(tinyPNG)) {
		t.Fatalf("s3 traffic bytes = %d, want %d", detailPayload.Detail.Record.TrafficBytes, len(tinyPNG))
	}
	if detailPayload.Detail.Record.AccessCount != 1 {
		t.Fatalf("s3 access count = %d, want 1", detailPayload.Detail.Record.AccessCount)
	}
}

func TestS3DirectUploadInitComplete(t *testing.T) {
	api := testAPI(t, true)
	disableGuestImageCompression(t, api)
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

	hash := sha256.Sum256(tinyPNG)
	hashHex := hex.EncodeToString(hash[:])
	initBody, err := json.Marshal(map[string]any{
		"filename":     "sample.png",
		"contentType":  "image/png",
		"size":         len(tinyPNG),
		"sha256":       hashHex,
		"headerBase64": base64.StdEncoding.EncodeToString(tinyPNG),
	})
	if err != nil {
		t.Fatal(err)
	}
	initReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/direct-upload/init", bytes.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("direct init status = %d, want %d; body: %s", initRec.Code, http.StatusOK, initRec.Body.String())
	}
	var initPayload struct {
		Upload struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(initRec.Body).Decode(&initPayload); err != nil {
		t.Fatal(err)
	}
	if initPayload.Token == "" || initPayload.Upload.URL == "" {
		t.Fatalf("direct init payload missing token or upload url: %+v", initPayload)
	}

	putReq, err := http.NewRequest(initPayload.Upload.Method, initPayload.Upload.URL, bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range initPayload.Upload.Headers {
		putReq.Header.Set(name, value)
	}
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, putResp.Body)
	_ = putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("direct put status = %d, want %d", putResp.StatusCode, http.StatusOK)
	}

	completeBody, err := json.Marshal(map[string]string{"token": initPayload.Token})
	if err != nil {
		t.Fatal(err)
	}
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/direct-upload/complete", bytes.NewReader(completeBody))
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusCreated {
		t.Fatalf("direct complete status = %d, want %d; body: %s", completeRec.Code, http.StatusCreated, completeRec.Body.String())
	}
	var completePayload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(completeRec.Body).Decode(&completePayload); err != nil {
		t.Fatal(err)
	}
	if completePayload.Resource.ID != hashHex[:16] {
		t.Fatalf("direct resource id = %q, want %q", completePayload.Resource.ID, hashHex[:16])
	}
	if completePayload.Resource.StorageDriver != "s3-default" {
		t.Fatalf("storage driver = %q, want s3-default", completePayload.Resource.StorageDriver)
	}
	if got := server.objectCount(); got != 1 {
		t.Fatalf("object count = %d, want 1", got)
	}
}

func TestS3DirectUploadRejectsMismatchedObjectHeader(t *testing.T) {
	api := testAPI(t, true)
	disableGuestImageCompression(t, api)
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

	body := []byte("<html><body>bad</body></html>")
	hash := sha256.Sum256(body)
	initBody, err := json.Marshal(map[string]any{
		"filename":     "sample.png",
		"contentType":  "image/png",
		"size":         len(body),
		"sha256":       hex.EncodeToString(hash[:]),
		"headerBase64": base64.StdEncoding.EncodeToString(tinyPNG),
	})
	if err != nil {
		t.Fatal(err)
	}
	initReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/direct-upload/init", bytes.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("direct init status = %d, want %d; body: %s", initRec.Code, http.StatusOK, initRec.Body.String())
	}
	var initPayload struct {
		Upload struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(initRec.Body).Decode(&initPayload); err != nil {
		t.Fatal(err)
	}
	putReq, err := http.NewRequest(initPayload.Upload.Method, initPayload.Upload.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range initPayload.Upload.Headers {
		putReq.Header.Set(name, value)
	}
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, putResp.Body)
	_ = putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("direct put status = %d, want %d", putResp.StatusCode, http.StatusOK)
	}

	completeBody, err := json.Marshal(map[string]string{"token": initPayload.Token})
	if err != nil {
		t.Fatal(err)
	}
	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/direct-upload/complete", bytes.NewReader(completeBody))
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusBadRequest {
		t.Fatalf("direct complete status = %d, want %d; body: %s", completeRec.Code, http.StatusBadRequest, completeRec.Body.String())
	}
	if got := server.objectCount(); got != 0 {
		t.Fatalf("object count after rejected complete = %d, want 0", got)
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

func disableGuestImageCompression(t *testing.T, api *API) {
	t.Helper()

	groups, err := api.app.Data.UserGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.ID != "guest" {
			continue
		}
		group.ImageCompressionEnabled = false
		if _, err := api.app.Data.UpdateUserGroup(context.Background(), group); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("guest group not found")
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
		if rawRange := strings.TrimSpace(r.Header.Get("Range")); strings.HasPrefix(rawRange, "bytes=0-") {
			end, err := strconv.Atoi(strings.TrimPrefix(rawRange, "bytes=0-"))
			if err != nil || end < 0 {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if end >= len(body) {
				end = len(body) - 1
			}
			if end < 0 {
				w.WriteHeader(http.StatusPartialContent)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body[:end+1])
			return
		}
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
