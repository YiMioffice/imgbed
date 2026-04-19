package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"machring/internal/app"
	"machring/internal/auth"
	"machring/internal/config"
	"machring/internal/persist"
	"machring/internal/policy"
	"machring/internal/resource"
	"machring/internal/storage"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
	0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

var tinySVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`)

func testJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*3) % 256),
				G: uint8((x*5 + y*11) % 256),
				B: uint8((x*13 + y*2) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPolicyTestEndpoint(t *testing.T) {
	api := testAPI(t, true)

	payload := map[string]any{
		"action":      "upload",
		"group":       policy.GroupGuest,
		"filename":    "release.zip",
		"contentType": "application/zip",
		"size":        policy.MB,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/test", bytes.NewReader(body))
	addAdminCookie(t, api, req)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response struct {
		Metadata resource.Metadata `json:"metadata"`
		Decision policy.Decision   `json:"decision"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Metadata.Type != resource.TypeArchive {
		t.Fatalf("metadata type = %q, want %q", response.Metadata.Type, resource.TypeArchive)
	}
	if response.Decision.Allowed {
		t.Fatalf("decision allowed = true, want false")
	}
}

func TestPolicyTestEndpointRejectsNegativeSize(t *testing.T) {
	api := testAPI(t, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/test", bytes.NewBufferString(`{"size":-1}`))
	addAdminCookie(t, api, req)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestPolicyTestEndpointRequiresAdmin(t *testing.T) {
	api := testAPI(t, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/test", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestPolicyGroupsRequireAdmin(t *testing.T) {
	api := testAPI(t, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-groups", nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestResourcesRequireAdmin(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
	listRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want %d; body: %s", listRec.Code, http.StatusUnauthorized, listRec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusUnauthorized {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusUnauthorized, detailRec.Body.String())
	}
}

func TestLoginAndMe(t *testing.T) {
	api := testAPI(t, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("session cookie was not set: %#v", cookies)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body: %s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookies[0])
	logoutRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d; body: %s", logoutRec.Code, http.StatusOK, logoutRec.Body.String())
	}
}

func TestLoginFailureRateLimit(t *testing.T) {
	api := testAPI(t, true)
	api.loginFailureLimiter = newFixedWindowRateLimiter(2, time.Hour)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	firstRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusUnauthorized {
		t.Fatalf("first login status = %d, want %d; body: %s", firstRec.Code, http.StatusUnauthorized, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	secondRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second login status = %d, want %d; body: %s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}
}

func TestLoginRateLimitIgnoresSpoofedForwardedForFromUntrustedClient(t *testing.T) {
	api := testAPI(t, true)
	api.loginFailureLimiter = newFixedWindowRateLimiter(2, time.Hour)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	firstReq.RemoteAddr = "203.0.113.10:1234"
	firstReq.Header.Set("X-Forwarded-For", "198.51.100.1")
	firstRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusUnauthorized {
		t.Fatalf("first login status = %d, want %d; body: %s", firstRec.Code, http.StatusUnauthorized, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	secondReq.RemoteAddr = "203.0.113.10:1234"
	secondReq.Header.Set("X-Forwarded-For", "198.51.100.2")
	secondRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second login status = %d, want %d; body: %s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}
}

func TestSessionCookieIsSecureBehindTrustedHTTPSProxy(t *testing.T) {
	api := testAPI(t, true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"secret"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookie was not set: %#v", cookies)
	}
	if !cookies[0].Secure {
		t.Fatalf("secure cookie = false, want true")
	}
}

func TestUploadCreatesResourceAndServesDirectLink(t *testing.T) {
	api := testAPI(t, true)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("group", policy.GroupGuest); err != nil {
		t.Fatal(err)
	}
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

	var uploadResponse struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&uploadResponse); err != nil {
		t.Fatal(err)
	}
	if uploadResponse.Resource.ID == "" {
		t.Fatal("resource id is empty")
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+uploadResponse.Resource.ID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/r/"+uploadResponse.Resource.ID, nil)
	fileRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", fileRec.Code, http.StatusOK, fileRec.Body.String())
	}
	if !bytes.Equal(fileRec.Body.Bytes(), tinyPNG) {
		t.Fatalf("served body mismatch: %v", fileRec.Body.Bytes())
	}
}

func TestUploadWithCustomDeliveryRouteReturnsRouteURL(t *testing.T) {
	api := testAPI(t, true)

	routeReq := httptest.NewRequest(http.MethodPut, "/api/v1/delivery-routes/fast", bytes.NewBufferString(`{
		"name":"高速下载",
		"description":"下载专线",
		"publicBaseUrl":"https://fast.example.test",
		"isDefault":false,
		"isEnabled":true
	}`))
	addAdminCookie(t, api, routeReq)
	routeReq.Header.Set("Content-Type", "application/json")
	routeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(routeRec, routeReq)
	if routeRec.Code != http.StatusOK {
		t.Fatalf("route save status = %d, want %d; body: %s", routeRec.Code, http.StatusOK, routeRec.Body.String())
	}

	groupReq := httptest.NewRequest(http.MethodPatch, "/api/v1/policy-groups/"+policy.DefaultGroupID, bytes.NewBufferString(`{
		"name":"默认策略组",
		"description":"系统默认策略组",
		"defaultDeliveryRouteId":"fast",
		"allowedDeliveryRouteIds":["fast"],
		"allowDeliveryRouteSelection":true
	}`))
	addAdminCookie(t, api, groupReq)
	groupReq.Header.Set("Content-Type", "application/json")
	groupRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(groupRec, groupReq)
	if groupRec.Code != http.StatusOK {
		t.Fatalf("group save status = %d, want %d; body: %s", groupRec.Code, http.StatusOK, groupRec.Body.String())
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("deliveryRouteId", "fast"); err != nil {
		t.Fatal(err)
	}
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

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", uploadRec.Code, http.StatusCreated, uploadRec.Body.String())
	}
	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Resource.DeliveryRouteID != "fast" {
		t.Fatalf("delivery route id = %q, want fast", payload.Resource.DeliveryRouteID)
	}
	if !strings.HasPrefix(payload.Resource.PublicURL, "https://fast.example.test/r/") {
		t.Fatalf("public url = %q, want fast route", payload.Resource.PublicURL)
	}

	originReq := httptest.NewRequest(http.MethodGet, "/r/"+payload.Resource.ID, nil)
	originRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(originRec, originReq)
	if originRec.Code != http.StatusFound {
		t.Fatalf("origin serve status = %d, want %d; body: %s", originRec.Code, http.StatusFound, originRec.Body.String())
	}
	if got := originRec.Header().Get("Location"); got != payload.Resource.PublicURL {
		t.Fatalf("origin redirect = %q, want %q", got, payload.Resource.PublicURL)
	}

	routeReq2 := httptest.NewRequest(http.MethodGet, payload.Resource.PublicURL, nil)
	routeRec2 := httptest.NewRecorder()
	api.Routes().ServeHTTP(routeRec2, routeReq2)
	if routeRec2.Code != http.StatusOK {
		t.Fatalf("route serve status = %d, want %d; body: %s", routeRec2.Code, http.StatusOK, routeRec2.Body.String())
	}
}

func TestJPEGUploadAppliesGroupCompression(t *testing.T) {
	api := testAPI(t, true)
	guestGroup := lookupGroup(t, api, policy.GroupGuest)
	guestGroup.ImageCompressionEnabled = true
	guestGroup.ImageCompressionQuality = 50
	if _, err := api.app.Data.UpdateUserGroup(context.Background(), guestGroup); err != nil {
		t.Fatal(err)
	}

	original := testJPEG(t)
	rec := uploadTestFile(t, api, "photo.jpg", original, "", "image/jpeg")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Items    []uploadItemResponse `json:"items"`
		Resource resource.Record      `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(payload.Items))
	}
	compression := payload.Items[0].Compression
	if compression == nil || !compression.Applied {
		t.Fatalf("compression was not applied: %#v", payload.Items[0].Compression)
	}
	if compression.OriginalBytes != int64(len(original)) {
		t.Fatalf("original bytes = %d, want %d", compression.OriginalBytes, len(original))
	}
	if compression.CompressedBytes >= int64(len(original)) {
		t.Fatalf("compressed bytes = %d, want smaller than %d", compression.CompressedBytes, len(original))
	}
	if payload.Resource.Size != compression.CompressedBytes {
		t.Fatalf("resource size = %d, want %d", payload.Resource.Size, compression.CompressedBytes)
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/r/"+payload.Resource.ID, nil)
	fileRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", fileRec.Code, http.StatusOK, fileRec.Body.String())
	}
	if int64(fileRec.Body.Len()) != compression.CompressedBytes {
		t.Fatalf("served bytes = %d, want %d", fileRec.Body.Len(), compression.CompressedBytes)
	}
}

func TestDeletingResourceRemovesFeaturedResource(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/featured-resources", bytes.NewBufferString(fmt.Sprintf(`{"resourceId":%q,"sortOrder":1}`, resourceID)))
	addAdminCookie(t, api, addReq)
	addReq.Header.Set("Content-Type", "application/json")
	addRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add featured status = %d, want %d; body: %s", addRec.Code, http.StatusCreated, addRec.Body.String())
	}

	beforeReq := httptest.NewRequest(http.MethodGet, "/api/v1/featured-resources", nil)
	beforeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(beforeRec, beforeReq)
	if beforeRec.Code != http.StatusOK {
		t.Fatalf("featured before status = %d, want %d; body: %s", beforeRec.Code, http.StatusOK, beforeRec.Body.String())
	}
	var beforePayload struct {
		Items []persist.FeaturedResource `json:"items"`
	}
	if err := json.NewDecoder(beforeRec.Body).Decode(&beforePayload); err != nil {
		t.Fatal(err)
	}
	if len(beforePayload.Items) != 1 {
		t.Fatalf("featured before len = %d, want 1", len(beforePayload.Items))
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, deleteReq)
	deleteRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body: %s", deleteRec.Code, http.StatusOK, deleteRec.Body.String())
	}

	afterReq := httptest.NewRequest(http.MethodGet, "/api/v1/featured-resources", nil)
	afterRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(afterRec, afterReq)
	if afterRec.Code != http.StatusOK {
		t.Fatalf("featured after status = %d, want %d; body: %s", afterRec.Code, http.StatusOK, afterRec.Body.String())
	}
	var afterPayload struct {
		Items []persist.FeaturedResource `json:"items"`
	}
	if err := json.NewDecoder(afterRec.Body).Decode(&afterPayload); err != nil {
		t.Fatal(err)
	}
	if len(afterPayload.Items) != 0 {
		t.Fatalf("featured after len = %d, want 0", len(afterPayload.Items))
	}
}

func TestRangeRequestsCountBytesWithoutInflatingAccessCount(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)
	ranges := []struct {
		header string
		start  int
		end    int
	}{
		{header: "bytes=0-9", start: 0, end: 9},
		{header: "bytes=10-19", start: 10, end: 19},
		{header: fmt.Sprintf("bytes=20-%d", len(tinyPNG)-1), start: 20, end: len(tinyPNG) - 1},
	}

	for _, item := range ranges {
		req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
		req.Header.Set("Range", item.header)
		rec := httptest.NewRecorder()
		api.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusPartialContent {
			t.Fatalf("%s status = %d, want %d; body: %s", item.header, rec.Code, http.StatusPartialContent, rec.Body.String())
		}
		expectedContentRange := fmt.Sprintf("bytes %d-%d/%d", item.start, item.end, len(tinyPNG))
		if got := rec.Header().Get("Content-Range"); got != expectedContentRange {
			t.Fatalf("content range = %q, want %q", got, expectedContentRange)
		}
		if !bytes.Equal(rec.Body.Bytes(), tinyPNG[item.start:item.end+1]) {
			t.Fatalf("served range mismatch for %s", item.header)
		}
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}
	var payload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Detail.Record.TrafficBytes != int64(len(tinyPNG)) {
		t.Fatalf("traffic bytes = %d, want %d", payload.Detail.Record.TrafficBytes, len(tinyPNG))
	}
	if payload.Detail.Record.AccessCount != 1 {
		t.Fatalf("access count = %d, want 1", payload.Detail.Record.AccessCount)
	}
	for _, window := range payload.Detail.TrafficWindows {
		if window.WindowType == "day" && window.UserID == "" {
			if window.RequestCount != 1 {
				t.Fatalf("day request count = %d, want 1", window.RequestCount)
			}
			if window.TrafficBytes != int64(len(tinyPNG)) {
				t.Fatalf("day traffic bytes = %d, want %d", window.TrafficBytes, len(tinyPNG))
			}
			return
		}
	}
	t.Fatalf("anonymous day traffic window not found: %#v", payload.Detail.TrafficWindows)
}

func TestUnsatisfiableRangeDoesNotCountTraffic(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(tinyPNG)))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusRequestedRangeNotSatisfiable, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != fmt.Sprintf("bytes */%d", len(tinyPNG)) {
		t.Fatalf("content range = %q", got)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}
	var payload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Detail.Record.TrafficBytes != 0 || payload.Detail.Record.AccessCount != 0 {
		t.Fatalf("traffic/access = %d/%d, want 0/0", payload.Detail.Record.TrafficBytes, payload.Detail.Record.AccessCount)
	}
}

func TestRawAssetRouteDoesNotBypassResourcePolicy(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}

	var payload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}

	rawReq := httptest.NewRequest(http.MethodGet, "/assets/"+payload.Detail.Record.ObjectKey, nil)
	rawRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusNotFound {
		t.Fatalf("raw asset status = %d, want %d; body: %s", rawRec.Code, http.StatusNotFound, rawRec.Body.String())
	}
}

func TestPrivateResourceRejectsAnonymousAccess(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNGAsUser(t, api, createTestUser(t, api, "private-user", "secret123", policy.GroupUser, "active").Username, "secret123")

	updateReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resourceID+"/visibility", bytes.NewBufferString(`{"isPrivate":true}`))
	addAdminCookie(t, api, updateReq)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("visibility status = %d, want %d; body: %s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestSignedResourceLinkAllowsPrivateAccess(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNGAsUser(t, api, createTestUser(t, api, "signed-user", "secret123", policy.GroupUser, "active").Username, "secret123")

	visibilityReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resourceID+"/visibility", bytes.NewBufferString(`{"isPrivate":true}`))
	addAdminCookie(t, api, visibilityReq)
	visibilityReq.Header.Set("Content-Type", "application/json")
	visibilityRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(visibilityRec, visibilityReq)
	if visibilityRec.Code != http.StatusOK {
		t.Fatalf("visibility status = %d, want %d; body: %s", visibilityRec.Code, http.StatusOK, visibilityRec.Body.String())
	}

	linkReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resourceID+"/signed-link", bytes.NewBufferString(`{"expiresInSeconds":3600}`))
	addAdminCookie(t, api, linkReq)
	linkReq.Header.Set("Content-Type", "application/json")
	linkRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(linkRec, linkReq)
	if linkRec.Code != http.StatusOK {
		t.Fatalf("signed link status = %d, want %d; body: %s", linkRec.Code, http.StatusOK, linkRec.Body.String())
	}

	var linkPayload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(linkRec.Body).Decode(&linkPayload); err != nil {
		t.Fatal(err)
	}
	if linkPayload.URL == "" {
		t.Fatal("signed link is empty")
	}

	req := httptest.NewRequest(http.MethodGet, linkPayload.URL, nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestExpiredSignedResourceLinkIsRejected(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNGAsUser(t, api, createTestUser(t, api, "expired-user", "secret123", policy.GroupUser, "active").Username, "secret123")

	visibilityReq := httptest.NewRequest(http.MethodPost, "/api/v1/resources/"+resourceID+"/visibility", bytes.NewBufferString(`{"isPrivate":true}`))
	addAdminCookie(t, api, visibilityReq)
	visibilityReq.Header.Set("Content-Type", "application/json")
	visibilityRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(visibilityRec, visibilityReq)
	if visibilityRec.Code != http.StatusOK {
		t.Fatalf("visibility status = %d, want %d; body: %s", visibilityRec.Code, http.StatusOK, visibilityRec.Body.String())
	}

	expiredUnix := time.Now().Add(-time.Minute).Unix()
	signature, err := api.resourceSignature(context.Background(), resourceID, expiredUnix)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/r/%s?exp=%d&sig=%s", resourceID, expiredUnix, signature), nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestUploadSanitizesFilename(t *testing.T) {
	api := testAPI(t, true)

	rec := uploadTestFile(t, api, `..\..\avatar.png`, tinyPNG, "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Resource.OriginalName != "avatar.png" {
		t.Fatalf("original name = %q, want %q", payload.Resource.OriginalName, "avatar.png")
	}
}

func TestUploadRejectsContentTypeMismatch(t *testing.T) {
	api := testAPI(t, true)

	rec := uploadTestFile(t, api, "photo.png", []byte("<html><body>bad</body></html>"), "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var payload struct {
		Error uploadError `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "content_type_mismatch" {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, "content_type_mismatch")
	}
}

func TestVideoUploadCreatesVideoResource(t *testing.T) {
	api := testAPI(t, true)

	rec := uploadTestFile(t, api, "clip.mp4", []byte("not a full mp4 but enough for policy coverage"), "", "video/mp4")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Resource.Type != resource.TypeVideo {
		t.Fatalf("resource type = %q, want %q", payload.Resource.Type, resource.TypeVideo)
	}
}

func TestGuestSVGUploadDeniedByPolicy(t *testing.T) {
	api := testAPI(t, true)

	rec := uploadTestFile(t, api, "vector.svg", tinySVG, "", "image/svg+xml")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	var payload struct {
		Error    uploadError       `json:"error"`
		Metadata resource.Metadata `json:"metadata"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "policy_rejected" {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, "policy_rejected")
	}
	if payload.Metadata.Type != resource.TypeOther {
		t.Fatalf("metadata type = %q, want %q", payload.Metadata.Type, resource.TypeOther)
	}
}

func TestDangerousResourceIsForcedToAttachment(t *testing.T) {
	api := testAPI(t, true)

	uploadRec := uploadTestFile(t, api, "admin.exe", []byte{0x4d, 0x5a, 0x00, 0x01, 0x02, 0x03}, "admin", "application/octet-stream")
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", uploadRec.Code, http.StatusCreated, uploadRec.Body.String())
	}

	var uploadPayload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploadPayload); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/r/"+uploadPayload.Resource.ID, nil)
	addAdminCookie(t, api, req)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content disposition = %q, want attachment", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("content security policy = %q, want %q", got, "sandbox")
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("x-frame-options = %q, want %q", got, "DENY")
	}
}

func TestResourceDetailIncludesImageMetadata(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, req)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Detail.Metadata.ImageWidth != 1 || payload.Detail.Metadata.ImageHeight != 1 {
		t.Fatalf("image metadata = %#v", payload.Detail.Metadata)
	}
	if payload.Detail.Links.Direct == "" || len(payload.Detail.Variants) == 0 {
		t.Fatalf("detail links/variants missing: %#v", payload.Detail)
	}
	if payload.Detail.TrafficWindows == nil {
		t.Fatal("traffic windows should be an empty array, got null")
	}
}

func TestAuthenticatedTrafficIsAggregatedByUser(t *testing.T) {
	api := testAPI(t, true)
	user := createTestUser(t, api, "alice", "secret123", policy.GroupUser, "active")
	resourceID := uploadTestPNGAsUser(t, api, user.Username, "secret123")

	req := httptest.NewRequest(http.MethodGet, "/r/"+resourceID, nil)
	req.AddCookie(loginCookie(t, api, user.Username, "secret123"))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+resourceID, nil)
	addAdminCookie(t, api, detailReq)
	detailRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", detailRec.Code, http.StatusOK, detailRec.Body.String())
	}

	var payload struct {
		Detail resource.Detail `json:"detail"`
	}
	if err := json.NewDecoder(detailRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, window := range payload.Detail.TrafficWindows {
		if window.UserID == user.ID && window.WindowType == "day" && window.RequestCount > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected authenticated traffic window for user %q, got %#v", user.ID, payload.Detail.TrafficWindows)
	}
}

func TestResourcesListAndStatsOverview(t *testing.T) {
	api := testAPI(t, true)
	_ = uploadTestPNG(t, api)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/resources?page=1&pageSize=1&type=image&status=active", nil)
	addAdminCookie(t, api, listReq)
	listRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body: %s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var listPayload struct {
		Items      []resource.Record `json:"items"`
		Total      int               `json:"total"`
		Page       int               `json:"page"`
		TotalPages int               `json:"totalPages"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if listPayload.Total != 1 || len(listPayload.Items) != 1 || listPayload.Page != 1 || listPayload.TotalPages != 1 {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	statsRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want %d; body: %s", statsRec.Code, http.StatusOK, statsRec.Body.String())
	}

	var statsPayload struct {
		Stats resource.Stats `json:"stats"`
	}
	if err := json.NewDecoder(statsRec.Body).Decode(&statsPayload); err != nil {
		t.Fatal(err)
	}
	if statsPayload.Stats.TotalResources != 1 || statsPayload.Stats.ActiveResources != 1 {
		t.Fatalf("unexpected stats payload: %#v", statsPayload.Stats)
	}
}

func TestUserGroupQuotaRejectsAuthenticatedUpload(t *testing.T) {
	api := testAPI(t, true)
	userGroup := lookupGroup(t, api, policy.GroupUser)
	userGroup.TotalCapacityBytes = 32
	if _, err := api.app.Data.UpdateUserGroup(context.Background(), userGroup); err != nil {
		t.Fatal(err)
	}
	_ = createTestUser(t, api, "quota-user", "secret123", policy.GroupUser, "active")

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
	req.AddCookie(loginCookie(t, api, "quota-user", "secret123"))
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	var payload struct {
		Error uploadError `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "storage_quota_exceeded" {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, "storage_quota_exceeded")
	}
}

func TestUpdateUserGroupPreservesCompressionWhenOmitted(t *testing.T) {
	api := testAPI(t, true)
	guestGroup := lookupGroup(t, api, policy.GroupGuest)
	guestGroup.ImageCompressionEnabled = false
	guestGroup.ImageCompressionQuality = 80
	if _, err := api.app.Data.UpdateUserGroup(context.Background(), guestGroup); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{
		"name":"游客",
		"description":"兼容旧客户端",
		"totalCapacityBytes":0,
		"defaultMonthlyTrafficBytes":0,
		"maxFileSizeBytes":0,
		"dailyUploadLimit":0,
		"allowHotlink":true
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/user-groups/"+policy.GroupGuest, body)
	addAdminCookie(t, api, req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := lookupGroup(t, api, policy.GroupGuest)
	if updated.ImageCompressionEnabled || updated.ImageCompressionQuality != 80 {
		t.Fatalf("compression = %v/%d, want false/80", updated.ImageCompressionEnabled, updated.ImageCompressionQuality)
	}
}

func TestGuestQuotaRejectsUpload(t *testing.T) {
	api := testAPI(t, true)
	guestGroup := lookupGroup(t, api, policy.GroupGuest)
	guestGroup.TotalCapacityBytes = 32
	if _, err := api.app.Data.UpdateUserGroup(context.Background(), guestGroup); err != nil {
		t.Fatal(err)
	}

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
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestGuestDailyIPUploadLimitRejectsSameIP(t *testing.T) {
	api := testAPI(t, true)
	guestGroup := lookupGroup(t, api, policy.GroupGuest)
	guestGroup.DailyIPUploadLimit = 1
	if _, err := api.app.Data.UpdateUserGroup(context.Background(), guestGroup); err != nil {
		t.Fatal(err)
	}

	firstRec := uploadTestFile(t, api, "sample.png", tinyPNG, "", "")
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d, want %d; body: %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}

	secondRec := uploadTestFile(t, api, "sample2.png", tinyPNG, "", "")
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload status = %d, want %d; body: %s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}
	var payload struct {
		Error uploadError `json:"error"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "daily_ip_upload_limit_exceeded" {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, "daily_ip_upload_limit_exceeded")
	}
}

func TestUploadRateLimit(t *testing.T) {
	api := testAPI(t, true)
	api.uploadLimiter = newFixedWindowRateLimiter(1, time.Hour)

	firstRec := uploadTestFile(t, api, "sample.png", tinyPNG, "", "")
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d, want %d; body: %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}

	secondRec := uploadTestFile(t, api, "sample2.png", tinyPNG, "", "")
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload status = %d, want %d; body: %s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}

	var payload struct {
		Error uploadError `json:"error"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "upload_rate_limited" {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, "upload_rate_limited")
	}
}

func TestAdminCanManageUsersAndResetPasswords(t *testing.T) {
	api := testAPI(t, true)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(`{
		"username":"managed-user",
		"displayName":"被管理用户",
		"password":"secret123",
		"groupId":"user",
		"status":"active"
	}`))
	addAdminCookie(t, api, createReq)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	var createPayload struct {
		User auth.User `json:"user"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.User.ID == "" {
		t.Fatal("created user id is empty")
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"managed-user","password":"secret123"}`))
	loginRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("initial login status = %d, want %d; body: %s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}

	banReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+createPayload.User.ID, bytes.NewBufferString(`{
		"displayName":"被管理用户",
		"groupId":"user",
		"status":"banned"
	}`))
	addAdminCookie(t, api, banReq)
	banReq.Header.Set("Content-Type", "application/json")
	banRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(banRec, banReq)
	if banRec.Code != http.StatusOK {
		t.Fatalf("ban status = %d, want %d; body: %s", banRec.Code, http.StatusOK, banRec.Body.String())
	}

	loginBlockedReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"managed-user","password":"secret123"}`))
	loginBlockedRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(loginBlockedRec, loginBlockedReq)
	if loginBlockedRec.Code != http.StatusUnauthorized {
		t.Fatalf("blocked login status = %d, want %d; body: %s", loginBlockedRec.Code, http.StatusUnauthorized, loginBlockedRec.Body.String())
	}

	restoreReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+createPayload.User.ID, bytes.NewBufferString(`{
		"displayName":"被管理用户",
		"groupId":"user",
		"status":"active"
	}`))
	addAdminCookie(t, api, restoreReq)
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want %d; body: %s", restoreRec.Code, http.StatusOK, restoreRec.Body.String())
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+createPayload.User.ID+"/reset-password", bytes.NewBufferString(`{"password":"secret456"}`))
	addAdminCookie(t, api, resetReq)
	resetReq.Header.Set("Content-Type", "application/json")
	resetRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset password status = %d, want %d; body: %s", resetRec.Code, http.StatusOK, resetRec.Body.String())
	}

	oldLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"managed-user","password":"secret123"}`))
	oldLoginRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(oldLoginRec, oldLoginReq)
	if oldLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d, want %d; body: %s", oldLoginRec.Code, http.StatusUnauthorized, oldLoginRec.Body.String())
	}

	newLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"managed-user","password":"secret456"}`))
	newLoginRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(newLoginRec, newLoginReq)
	if newLoginRec.Code != http.StatusOK {
		t.Fatalf("new password login status = %d, want %d; body: %s", newLoginRec.Code, http.StatusOK, newLoginRec.Body.String())
	}
}

func TestPolicyGroupsCanBeCopiedAndActivated(t *testing.T) {
	api := testAPI(t, true)

	copyReq := httptest.NewRequest(http.MethodPost, "/api/v1/policy-groups/default/copy", bytes.NewBufferString(`{"name":"实验策略组"}`))
	addAdminCookie(t, api, copyReq)
	copyRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(copyRec, copyReq)
	if copyRec.Code != http.StatusCreated {
		t.Fatalf("copy status = %d, want %d; body: %s", copyRec.Code, http.StatusCreated, copyRec.Body.String())
	}

	var copyPayload struct {
		Group policy.Group `json:"group"`
	}
	if err := json.NewDecoder(copyRec.Body).Decode(&copyPayload); err != nil {
		t.Fatal(err)
	}

	rulesPayload := bytes.NewBufferString(`{"rules":[{"userGroup":"guest","resourceType":"image","allowUpload":false,"allowAccess":true,"maxFileSizeBytes":10485760,"monthlyTrafficPerResourceBytes":1073741824,"monthlyTrafficPerUserAndTypeBytes":0,"requireAuth":false,"requireReview":false,"forcePrivate":false,"cacheControl":"public, max-age=31536000, immutable","downloadDisposition":""}]}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/policies?groupId="+copyPayload.Group.ID, rulesPayload)
	addAdminCookie(t, api, updateReq)
	updateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update rules status = %d, want %d; body: %s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	activateReq := httptest.NewRequest(http.MethodPost, "/api/v1/policy-groups/"+copyPayload.Group.ID+"/activate", nil)
	addAdminCookie(t, api, activateReq)
	activateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(activateRec, activateReq)
	if activateRec.Code != http.StatusOK {
		t.Fatalf("activate status = %d, want %d; body: %s", activateRec.Code, http.StatusOK, activateRec.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/v1/policies/test", bytes.NewBufferString(`{"group":"guest","filename":"demo.jpg","contentType":"image/jpeg","size":1024}`))
	addAdminCookie(t, api, testReq)
	testRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status = %d, want %d; body: %s", testRec.Code, http.StatusOK, testRec.Body.String())
	}

	var testPayload struct {
		Decision    policy.Decision `json:"decision"`
		PolicyGroup policy.Group    `json:"policyGroup"`
	}
	if err := json.NewDecoder(testRec.Body).Decode(&testPayload); err != nil {
		t.Fatal(err)
	}
	if testPayload.PolicyGroup.ID != copyPayload.Group.ID {
		t.Fatalf("policy group id = %q, want %q", testPayload.PolicyGroup.ID, copyPayload.Group.ID)
	}
	if testPayload.Decision.Allowed {
		t.Fatalf("decision allowed = true, want false")
	}
}

func TestReplacePoliciesReturnsValidationErrors(t *testing.T) {
	api := testAPI(t, true)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/policies", bytes.NewBufferString(`{"rules":[{"userGroup":"bad","resourceType":"image","allowUpload":true,"allowAccess":true,"maxFileSizeBytes":-1,"monthlyTrafficPerResourceBytes":0,"monthlyTrafficPerUserAndTypeBytes":0,"requireAuth":false,"requireReview":false,"forcePrivate":false}]}`))
	addAdminCookie(t, api, req)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestEffectivePolicySupportsExplicitResourceType(t *testing.T) {
	api := testAPI(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/policies/effective?group=guest&action=upload&resourceType=image&extension=jpg&size=1024", nil)
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Decision    policy.Decision `json:"decision"`
		PolicyGroup policy.Group    `json:"policyGroup"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Decision.Allowed {
		t.Fatalf("decision allowed = false; reason: %s", payload.Decision.Reason)
	}
	if payload.PolicyGroup.ID == "" {
		t.Fatal("policy group id is empty")
	}
}

func TestInstallStateAndSetup(t *testing.T) {
	api := testAPI(t, false)

	stateReq := httptest.NewRequest(http.MethodGet, "/api/v1/install/state", nil)
	stateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("state status = %d, want %d; body: %s", stateRec.Code, http.StatusOK, stateRec.Body.String())
	}

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/install/setup", bytes.NewBufferString(`{
		"siteName":"测试站点",
		"defaultStorage":"local",
		"adminUsername":"root",
		"displayName":"超级管理员",
		"password":"secret123"
	}`))
	setupRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d; body: %s", setupRec.Code, http.StatusCreated, setupRec.Body.String())
	}

	repeatReq := httptest.NewRequest(http.MethodPost, "/api/v1/install/setup", bytes.NewBufferString(`{
		"siteName":"测试站点",
		"defaultStorage":"local",
		"adminUsername":"root",
		"displayName":"超级管理员",
		"password":"secret123"
	}`))
	repeatRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(repeatRec, repeatReq)
	if repeatRec.Code != http.StatusConflict {
		t.Fatalf("repeat setup status = %d, want %d; body: %s", repeatRec.Code, http.StatusConflict, repeatRec.Body.String())
	}
}

func TestInstallSetupBlockedWhenExistingDataHasNoAdmin(t *testing.T) {
	api := testAPI(t, false)
	now := time.Now()
	err := api.app.Data.CreateResource(context.Background(), persist.CreateResourceBundle{
		Record: resource.Record{
			ID:            "res_existing",
			UserGroup:     policy.GroupGuest,
			StorageDriver: "local",
			ObjectKey:     "legacy/sample.png",
			PublicURL:     "http://example.test/r/res_existing",
			OriginalName:  "sample.png",
			Extension:     "png",
			Type:          resource.TypeImage,
			Size:          1,
			ContentType:   "image/png",
			Hash:          "legacy",
			Status:        resource.StatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/v1/install/state", nil)
	stateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("state status = %d, want %d; body: %s", stateRec.Code, http.StatusOK, stateRec.Body.String())
	}
	var state persist.InstallState
	if err := json.NewDecoder(stateRec.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if !state.Initialized {
		t.Fatal("state initialized = false, want true when existing resource data is present")
	}

	setupReq := httptest.NewRequest(http.MethodPost, "/api/v1/install/setup", bytes.NewBufferString(`{
		"siteName":"恶意初始化",
		"defaultStorage":"local",
		"adminUsername":"attacker",
		"displayName":"Attacker",
		"password":"secret123"
	}`))
	setupRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusConflict {
		t.Fatalf("setup status = %d, want %d; body: %s", setupRec.Code, http.StatusConflict, setupRec.Body.String())
	}
}

func testAPI(t *testing.T, initialized bool) *API {
	t.Helper()
	dataDir := t.TempDir()
	dataStore, err := persist.NewSQLite(filepath.Join(dataDir, "machring.db"), policy.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	if initialized {
		passwordHash, err := auth.HashPassword("secret")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dataStore.Initialize(context.Background(), persist.InitializeParams{
			SiteName:       "测试站点",
			DefaultStorage: "local",
			AdminUsername:  "admin",
			DisplayName:    "管理员",
			PasswordHash:   passwordHash,
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := dataStore.Close(); err != nil {
			t.Fatal(err)
		}
	})

	localStore := storage.NewLocal(filepath.Join(dataDir, "uploads"), "http://example.test")
	return New(&app.App{
		Config:      config.Config{PublicBaseURL: "http://example.test"},
		Storage:     localStore,
		Storages:    storage.NewManager(localStore),
		PolicyStore: dataStore,
		Data:        dataStore,
		Detector:    resource.Detector{},
		Auth:        auth.NewService(dataStore, time.Hour),
	})
}

func addAdminCookie(t *testing.T, api *API, req *http.Request) {
	t.Helper()

	session, ok, err := api.app.Auth.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("admin login failed")
	}

	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session.Token})
}

func loginCookie(t *testing.T, api *API, username, password string) *http.Cookie {
	t.Helper()

	session, ok, err := api.app.Auth.Login(context.Background(), username, password)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("login failed for %s", username)
	}
	return &http.Cookie{Name: sessionCookieName, Value: session.Token}
}

func createTestUser(t *testing.T, api *API, username, password, groupID, status string) auth.User {
	t.Helper()

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	user, err := api.app.Data.CreateUser(context.Background(), persist.CreateUserParams{
		Username:     username,
		DisplayName:  username,
		PasswordHash: passwordHash,
		Role:         "user",
		GroupID:      groupID,
		Status:       status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func lookupGroup(t *testing.T, api *API, id string) persist.UserGroup {
	t.Helper()

	groups, err := api.app.Data.UserGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("group %q not found", id)
	return persist.UserGroup{}
}

func uploadTestPNG(t *testing.T, api *API) string {
	t.Helper()

	rec := uploadTestFile(t, api, "sample.png", tinyPNG, "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Resource.ID
}

func uploadTestPNGAsUser(t *testing.T, api *API, username, password string) string {
	t.Helper()

	rec := uploadTestFile(t, api, "sample.png", tinyPNG, username, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var payload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Resource.OwnerUserID == "" {
		t.Fatal("owner user id is empty")
	}
	return payload.Resource.ID
}

func uploadTestFile(t *testing.T, api *API, filename string, content []byte, username string, contentType string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if username == "" {
		if err := writer.WriteField("group", policy.GroupGuest); err != nil {
			t.Fatal(err)
		}
	}
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	if strings.TrimSpace(contentType) != "" {
		partHeader.Set("Content-Type", contentType)
	}
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/resources/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if username != "" {
		password := "secret123"
		if username == "admin" {
			password = "secret"
		}
		req.AddCookie(loginCookie(t, api, username, password))
	}
	rec := httptest.NewRecorder()
	api.Routes().ServeHTTP(rec, req)
	return rec
}
