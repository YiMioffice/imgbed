package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"machring/internal/resource"
)

func TestSiteSettingsControlGuestUploadsAndPublicLinks(t *testing.T) {
	api := testAPI(t, true)

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/site-settings", bytes.NewBufferString(`{
		"siteName":"新站点",
		"externalBaseUrl":"https://cdn.example.test",
		"allowGuestUploads":false,
		"showStatsOnHome":true,
		"showFeaturedOnHome":true
	}`))
	addAdminCookie(t, api, updateReq)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update site settings status = %d, want %d; body: %s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}

	guestUploadReq := newUploadPNGRequest(t)
	guestUploadRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(guestUploadRec, guestUploadReq)
	if guestUploadRec.Code != http.StatusForbidden {
		t.Fatalf("guest upload status = %d, want %d; body: %s", guestUploadRec.Code, http.StatusForbidden, guestUploadRec.Body.String())
	}

	var guestPayload struct {
		Error uploadError `json:"error"`
	}
	if err := json.NewDecoder(guestUploadRec.Body).Decode(&guestPayload); err != nil {
		t.Fatal(err)
	}
	if guestPayload.Error.Code != "guest_uploads_disabled" {
		t.Fatalf("error code = %q, want %q", guestPayload.Error.Code, "guest_uploads_disabled")
	}

	enableReq := httptest.NewRequest(http.MethodPut, "/api/v1/site-settings", bytes.NewBufferString(`{
		"siteName":"新站点",
		"externalBaseUrl":"https://cdn.example.test",
		"allowGuestUploads":true,
		"showStatsOnHome":false,
		"showFeaturedOnHome":true
	}`))
	addAdminCookie(t, api, enableReq)
	enableReq.Header.Set("Content-Type", "application/json")
	enableRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable guest uploads status = %d, want %d; body: %s", enableRec.Code, http.StatusOK, enableRec.Body.String())
	}

	uploadReq := newUploadPNGRequest(t)
	uploadRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body: %s", uploadRec.Code, http.StatusCreated, uploadRec.Body.String())
	}

	var uploadPayload struct {
		Resource resource.Record `json:"resource"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploadPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uploadPayload.Resource.PublicURL, "https://cdn.example.test/r/") {
		t.Fatalf("public url = %q, want prefix %q", uploadPayload.Resource.PublicURL, "https://cdn.example.test/r/")
	}
}

func TestFeaturedResourcesLifecycle(t *testing.T) {
	api := testAPI(t, true)
	resourceID := uploadTestPNG(t, api)

	addFeaturedReq := httptest.NewRequest(http.MethodPost, "/api/v1/featured-resources", bytes.NewBufferString(`{"resourceId":"`+resourceID+`"}`))
	addAdminCookie(t, api, addFeaturedReq)
	addFeaturedReq.Header.Set("Content-Type", "application/json")
	addFeaturedRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(addFeaturedRec, addFeaturedReq)
	if addFeaturedRec.Code != http.StatusCreated {
		t.Fatalf("add featured first status = %d, want %d; body: %s", addFeaturedRec.Code, http.StatusCreated, addFeaturedRec.Body.String())
	}

	reorderReq := httptest.NewRequest(http.MethodPut, "/api/v1/featured-resources/order", bytes.NewBufferString(`{"resourceIds":["`+resourceID+`"]}`))
	addAdminCookie(t, api, reorderReq)
	reorderReq.Header.Set("Content-Type", "application/json")
	reorderRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(reorderRec, reorderReq)
	if reorderRec.Code != http.StatusOK {
		t.Fatalf("reorder featured status = %d, want %d; body: %s", reorderRec.Code, http.StatusOK, reorderRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/featured-resources", nil)
	listRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list featured status = %d, want %d; body: %s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var listPayload struct {
		Items []struct {
			Resource resource.Record `json:"resource"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Items) != 1 {
		t.Fatalf("featured item count = %d, want %d", len(listPayload.Items), 1)
	}
	if listPayload.Items[0].Resource.ID != resourceID {
		t.Fatalf("featured id = %q, want %q", listPayload.Items[0].Resource.ID, resourceID)
	}

	removeReq := httptest.NewRequest(http.MethodDelete, "/api/v1/featured-resources/"+resourceID, nil)
	addAdminCookie(t, api, removeReq)
	removeRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(removeRec, removeReq)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove featured status = %d, want %d; body: %s", removeRec.Code, http.StatusOK, removeRec.Body.String())
	}

	finalListReq := httptest.NewRequest(http.MethodGet, "/api/v1/featured-resources", nil)
	finalListRec := httptest.NewRecorder()
	api.Routes().ServeHTTP(finalListRec, finalListReq)
	if finalListRec.Code != http.StatusOK {
		t.Fatalf("final list featured status = %d, want %d; body: %s", finalListRec.Code, http.StatusOK, finalListRec.Body.String())
	}

	var finalListPayload struct {
		Items []struct {
			Resource resource.Record `json:"resource"`
		} `json:"items"`
	}
	if err := json.NewDecoder(finalListRec.Body).Decode(&finalListPayload); err != nil {
		t.Fatal(err)
	}
	if len(finalListPayload.Items) != 0 {
		t.Fatalf("final featured item count = %d, want %d", len(finalListPayload.Items), 0)
	}
}

func newUploadPNGRequest(t *testing.T) *http.Request {
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
	return req
}
