package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"machring/internal/persist"
)

const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
const unsignedPayload = "UNSIGNED-PAYLOAD"

type S3 struct {
	cfg      persist.StorageConfig
	client   *http.Client
	endpoint *url.URL
}

func NewS3(_ context.Context, cfg persist.StorageConfig) (*S3, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("s3 endpoint is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, errors.New("s3 credentials are required")
	}
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"))
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, errors.New("s3 endpoint must include scheme and host")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "auto"
	}
	return &S3{
		cfg:      cfg,
		client:   &http.Client{Timeout: 45 * time.Second},
		endpoint: endpoint,
	}, nil
}

func (s *S3) Put(ctx context.Context, key string, reader io.Reader) (Object, error) {
	tempFile, payloadHash, size, err := bufferReader(reader)
	if err != nil {
		return Object{}, err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return Object{}, err
	}

	req, err := s.newObjectRequest(ctx, http.MethodPut, key, tempFile, payloadHash)
	if err != nil {
		return Object{}, err
	}
	req.ContentLength = size

	resp, err := s.client.Do(req)
	if err != nil {
		return Object{}, err
	}
	defer drainAndClose(resp.Body)
	if err := requireStatus(resp, http.StatusOK); err != nil {
		return Object{}, err
	}

	return Object{
		Key: key,
		URL: s.PublicURL(key),
	}, nil
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := s.newObjectRequest(ctx, http.MethodGet, key, nil, emptyPayloadSHA256)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer drainAndClose(resp.Body)
		return nil, fmt.Errorf("s3 get object failed: %s", resp.Status)
	}
	return resp.Body, nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.Open(ctx, key)
}

func (s *S3) Delete(ctx context.Context, key string) error {
	req, err := s.newObjectRequest(ctx, http.MethodDelete, key, nil, emptyPayloadSHA256)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	return requireStatus(resp, http.StatusNoContent, http.StatusOK, http.StatusAccepted, http.StatusNotFound)
}

func (s *S3) Stat(ctx context.Context, key string) (Stat, error) {
	req, err := s.newObjectRequest(ctx, http.MethodHead, key, nil, emptyPayloadSHA256)
	if err != nil {
		return Stat{}, err
	}
	resp, err := s.client.Do(req)
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

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	req, err := s.newObjectRequest(ctx, http.MethodHead, key, nil, emptyPayloadSHA256)
	if err != nil {
		return false, err
	}
	resp, err := s.client.Do(req)
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
		return false, fmt.Errorf("s3 exists check failed: %s", resp.Status)
	}
}

func (s *S3) PublicURL(key string) string {
	key = JoinBasePath(s.cfg.BasePath, key)
	if base := strings.TrimRight(strings.TrimSpace(s.cfg.PublicBaseURL), "/"); base != "" {
		return base + "/" + strings.TrimLeft(key, "/")
	}
	u := s.objectURL(key)
	return u.String()
}

func (s *S3) RedirectURL(ctx context.Context, key string, options RedirectOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	method := strings.ToUpper(strings.TrimSpace(options.Method))
	if method == "" {
		method = http.MethodGet
	}
	expires := options.Expires
	if expires <= 0 {
		expires = 5 * time.Minute
	}
	if expires > 7*24*time.Hour {
		expires = 7 * 24 * time.Hour
	}

	targetURL := s.objectURL(JoinBasePath(s.cfg.BasePath, key))
	query := targetURL.Query()
	now := time.Now().UTC()
	shortDate := now.Format("20060102")
	scope := shortDate + "/" + s.cfg.Region + "/s3/aws4_request"
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", s.cfg.AccessKeyID+"/"+scope)
	query.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(expires/time.Second), 10))
	query.Set("X-Amz-SignedHeaders", "host")
	if strings.TrimSpace(options.ContentType) != "" {
		query.Set("response-content-type", strings.TrimSpace(options.ContentType))
	}
	if strings.TrimSpace(options.CacheControl) != "" {
		query.Set("response-cache-control", strings.TrimSpace(options.CacheControl))
	}
	if strings.TrimSpace(options.ContentDisposition) != "" {
		query.Set("response-content-disposition", strings.TrimSpace(options.ContentDisposition))
	}
	targetURL.RawQuery = query.Encode()
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(&targetURL),
		canonicalQuery(&targetURL),
		"host:" + targetURL.Host + "\n",
		"host",
		unsignedPayload,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		now.Format("20060102T150405Z"),
		scope,
		sha256HexString(canonicalRequest),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(s.cfg.SecretAccessKey, shortDate, s.cfg.Region, "s3"), stringToSign))
	targetURL.RawQuery = canonicalQuery(&targetURL) + "&X-Amz-Signature=" + signature
	return targetURL.String(), nil
}

func (s *S3) HealthCheck(ctx context.Context) error {
	req, err := s.newBucketRequest(ctx, http.MethodHead, nil, emptyPayloadSHA256)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	if err := requireStatus(resp, http.StatusOK, http.StatusNoContent); err == nil {
		return nil
	}
	return s.objectProbe(ctx)
}

func (s *S3) objectProbe(ctx context.Context) error {
	key := ".machring-healthcheck/" + time.Now().UTC().Format("20060102T150405.000000000Z")
	putReq, err := s.newObjectRequest(ctx, http.MethodPut, key, strings.NewReader(""), emptyPayloadSHA256)
	if err != nil {
		return err
	}
	putReq.ContentLength = 0
	putResp, err := s.client.Do(putReq)
	if err != nil {
		return err
	}
	if err := requireStatus(putResp, http.StatusOK, http.StatusCreated, http.StatusNoContent); err != nil {
		drainAndClose(putResp.Body)
		return err
	}
	drainAndClose(putResp.Body)

	if exists, err := s.Exists(ctx, key); err != nil {
		_ = s.Delete(ctx, key)
		return err
	} else if !exists {
		_ = s.Delete(ctx, key)
		return errors.New("s3 health probe object was not visible after upload")
	}
	return s.Delete(ctx, key)
}

func (s *S3) newObjectRequest(ctx context.Context, method, key string, body io.Reader, payloadHash string) (*http.Request, error) {
	targetURL := s.objectURL(JoinBasePath(s.cfg.BasePath, key))
	return s.newSignedRequest(ctx, method, &targetURL, body, payloadHash)
}

func (s *S3) objectURL(key string) url.URL {
	targetURL := *s.endpoint
	if s.cfg.UsePathStyle {
		targetURL.Path = joinURLPath(targetURL.Path, s.cfg.Bucket, key)
		return targetURL
	}
	targetURL.Host = s.cfg.Bucket + "." + targetURL.Host
	targetURL.Path = joinURLPath(targetURL.Path, key)
	return targetURL
}

func (s *S3) newBucketRequest(ctx context.Context, method string, body io.Reader, payloadHash string) (*http.Request, error) {
	targetURL := *s.endpoint
	if s.cfg.UsePathStyle {
		targetURL.Path = joinURLPath(targetURL.Path, s.cfg.Bucket)
	} else {
		targetURL.Host = s.cfg.Bucket + "." + targetURL.Host
	}
	return s.newSignedRequest(ctx, method, &targetURL, body, payloadHash)
}

func (s *S3) newSignedRequest(ctx context.Context, method string, targetURL *url.URL, body io.Reader, payloadHash string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL.String(), body)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	canonicalHeaders := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	signedHeaders := make([]string, 0, len(canonicalHeaders))
	for name := range canonicalHeaders {
		signedHeaders = append(signedHeaders, name)
	}
	sort.Strings(signedHeaders)

	canonicalHeaderBuilder := strings.Builder{}
	for _, name := range signedHeaders {
		canonicalHeaderBuilder.WriteString(name)
		canonicalHeaderBuilder.WriteString(":")
		canonicalHeaderBuilder.WriteString(strings.TrimSpace(canonicalHeaders[name]))
		canonicalHeaderBuilder.WriteString("\n")
	}

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(req.URL),
		canonicalQuery(req.URL),
		canonicalHeaderBuilder.String(),
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")
	scope := shortDate + "/" + s.cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256HexString(canonicalRequest),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(signingKey(s.cfg.SecretAccessKey, shortDate, s.cfg.Region, "s3"), stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKeyID,
		scope,
		strings.Join(signedHeaders, ";"),
		signature,
	))
	return req, nil
}

func canonicalURI(targetURL *url.URL) string {
	if escaped := targetURL.EscapedPath(); escaped != "" {
		return escaped
	}
	return "/"
}

func canonicalQuery(targetURL *url.URL) string {
	values := targetURL.Query()
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		sortedValues := append([]string(nil), values[key]...)
		sort.Strings(sortedValues)
		escapedKey := awsQueryEscape(key)
		if len(sortedValues) == 0 {
			parts = append(parts, escapedKey+"=")
			continue
		}
		for _, value := range sortedValues {
			parts = append(parts, escapedKey+"="+awsQueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsQueryEscape(value string) string {
	replacer := strings.NewReplacer("+", "%20", "*", "%2A", "%7E", "~")
	return replacer.Replace(url.QueryEscape(value))
}

func signingKey(secretKey, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func bufferReader(reader io.Reader) (*os.File, string, int64, error) {
	file, err := os.CreateTemp("", "machring-storage-*")
	if err != nil {
		return nil, "", 0, err
	}
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hasher), reader)
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, "", 0, err
	}
	return file, hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func requireStatus(resp *http.Response, allowed ...int) error {
	for _, code := range allowed {
		if resp.StatusCode == code {
			return nil
		}
	}
	return fmt.Errorf("unexpected status: %s", resp.Status)
}

func drainAndClose(closer io.ReadCloser) {
	if closer == nil {
		return
	}
	_, _ = io.Copy(io.Discard, closer)
	_ = closer.Close()
}

func headerInt64(raw string) int64 {
	if raw == "" {
		return 0
	}
	var value int64
	fmt.Sscanf(raw, "%d", &value)
	return value
}

func joinURLPath(basePath string, parts ...string) string {
	segments := make([]string, 0, len(parts)+1)
	if trimmed := strings.Trim(basePath, "/"); trimmed != "" {
		segments = append(segments, trimmed)
	}
	for _, part := range parts {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return "/" + path.Join(segments...)
}
