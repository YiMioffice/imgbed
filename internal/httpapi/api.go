package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"machring/internal/app"
	"machring/internal/auth"
	"machring/internal/persist"
	"machring/internal/policy"
	"machring/internal/resource"
	"machring/internal/storage"
)

type API struct {
	app                 *app.App
	uploadLimiter       *fixedWindowRateLimiter
	loginFailureLimiter *fixedWindowRateLimiter
	compressionLimiter  chan struct{}
}

const sessionCookieName = "machring_session"

func New(app *app.App) *API {
	return &API{
		app:                 app,
		uploadLimiter:       newFixedWindowRateLimiter(defaultUploadRequestLimit, defaultUploadWindow),
		loginFailureLimiter: newFixedWindowRateLimiter(defaultLoginFailureLimit, defaultLoginFailureWindow),
		compressionLimiter:  make(chan struct{}, compressionWorkerLimit()),
	}
}

func (api *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", api.health)
	mux.HandleFunc("GET /api/v1/install/state", api.installState)
	mux.HandleFunc("POST /api/v1/install/setup", api.installSetup)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.HandleFunc("POST /api/v1/auth/logout", api.logout)
	mux.HandleFunc("GET /api/v1/auth/me", api.me)
	mux.HandleFunc("GET /api/v1/account/usage", api.accountUsage)
	mux.HandleFunc("GET /api/v1/site-settings", api.siteSettings)
	mux.HandleFunc("PUT /api/v1/site-settings", api.updateSiteSettings)
	mux.HandleFunc("GET /api/v1/policy-groups", api.policyGroups)
	mux.HandleFunc("POST /api/v1/policy-groups", api.createPolicyGroup)
	mux.HandleFunc("GET /api/v1/policy-groups/{id}", api.policyGroupDetail)
	mux.HandleFunc("PATCH /api/v1/policy-groups/{id}", api.updatePolicyGroup)
	mux.HandleFunc("DELETE /api/v1/policy-groups/{id}", api.deletePolicyGroup)
	mux.HandleFunc("POST /api/v1/policy-groups/{id}/copy", api.copyPolicyGroup)
	mux.HandleFunc("POST /api/v1/policy-groups/{id}/activate", api.activatePolicyGroup)
	mux.HandleFunc("POST /api/v1/policy-groups/{id}/deactivate", api.deactivatePolicyGroup)
	mux.HandleFunc("GET /api/v1/policies", api.policies)
	mux.HandleFunc("PUT /api/v1/policies", api.replacePolicies)
	mux.HandleFunc("GET /api/v1/policies/effective", api.effectivePolicy)
	mux.HandleFunc("POST /api/v1/policies/test", api.testPolicy)
	mux.HandleFunc("GET /api/v1/user-groups", api.userGroups)
	mux.HandleFunc("PUT /api/v1/user-groups/{id}", api.updateUserGroup)
	mux.HandleFunc("GET /api/v1/users", api.users)
	mux.HandleFunc("POST /api/v1/users", api.createUser)
	mux.HandleFunc("PATCH /api/v1/users/{id}", api.updateUser)
	mux.HandleFunc("POST /api/v1/users/{id}/reset-password", api.resetUserPassword)
	mux.HandleFunc("GET /api/v1/storage-configs", api.storageConfigs)
	mux.HandleFunc("PUT /api/v1/storage-configs/{id}", api.upsertStorageConfig)
	mux.HandleFunc("POST /api/v1/storage-configs/health-check", api.storageHealthCheck)
	mux.HandleFunc("GET /api/v1/delivery-routes", api.deliveryRoutes)
	mux.HandleFunc("PUT /api/v1/delivery-routes/{id}", api.upsertDeliveryRoute)
	mux.HandleFunc("DELETE /api/v1/delivery-routes/{id}", api.deleteDeliveryRoute)
	mux.HandleFunc("GET /api/v1/delivery-routes/choices", api.deliveryRouteChoices)
	mux.HandleFunc("GET /api/v1/featured-resources", api.featuredResources)
	mux.HandleFunc("POST /api/v1/featured-resources", api.addFeaturedResource)
	mux.HandleFunc("DELETE /api/v1/featured-resources/{id}", api.removeFeaturedResource)
	mux.HandleFunc("PUT /api/v1/featured-resources/order", api.reorderFeaturedResources)
	mux.HandleFunc("GET /api/v1/resources", api.resources)
	mux.HandleFunc("GET /api/v1/resources/{id}", api.resourceDetail)
	mux.HandleFunc("POST /api/v1/resources/{id}/visibility", api.updateResourceVisibility)
	mux.HandleFunc("POST /api/v1/resources/{id}/signed-link", api.generateSignedResourceLink)
	mux.HandleFunc("DELETE /api/v1/resources/{id}", api.deleteResource)
	mux.HandleFunc("POST /api/v1/resources/{id}/restore", api.restoreResource)
	mux.HandleFunc("POST /api/v1/resources/upload", api.uploadResource)
	mux.HandleFunc("GET /api/v1/stats/overview", api.statsOverview)
	mux.HandleFunc("HEAD /r/{id}", api.serveResource)
	mux.HandleFunc("GET /r/{id}", api.serveResource)
	return secureMiddleware(mux)
}

func (api *API) health(w http.ResponseWriter, r *http.Request) {
	_, activeStore, activeConfig, err := api.resolveDefaultStore(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}
	if err := activeStore.HealthCheck(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "machring",
		"storage": sanitizeStorageConfig(activeConfig),
	})
}

func (api *API) installState(w http.ResponseWriter, r *http.Request) {
	state, err := api.app.Data.InstallState(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load installation state", err))
		return
	}
	if settings, err := api.app.Data.SiteSettings(r.Context()); err == nil && strings.TrimSpace(settings.SiteName) != "" {
		state.SiteName = settings.SiteName
	}
	if api.app.Config.SiteName != "" && state.SiteName == "" {
		state.SiteName = api.app.Config.SiteName
	}

	writeJSON(w, http.StatusOK, state)
}

type installSetupRequest struct {
	SiteName       string `json:"siteName"`
	DefaultStorage string `json:"defaultStorage"`
	AdminUsername  string `json:"adminUsername"`
	DisplayName    string `json:"displayName"`
	Password       string `json:"password"`
}

func (api *API) installSetup(w http.ResponseWriter, r *http.Request) {
	var req installSetupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid installation payload", err))
		return
	}

	req.SiteName = strings.TrimSpace(req.SiteName)
	req.DefaultStorage = strings.TrimSpace(req.DefaultStorage)
	req.AdminUsername = strings.TrimSpace(req.AdminUsername)
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	switch {
	case req.SiteName == "":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "site name is required"})
		return
	case req.AdminUsername == "":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "admin username is required"})
		return
	case req.DisplayName == "":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "display name is required"})
		return
	case len(req.Password) < 8:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password must be at least 8 characters"})
		return
	}
	if req.DefaultStorage == "" {
		req.DefaultStorage = "local"
	}
	if req.DefaultStorage != "local" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "only local storage is currently supported"})
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to hash password", err))
		return
	}

	user, err := api.app.Data.Initialize(r.Context(), persist.InitializeParams{
		SiteName:       req.SiteName,
		DefaultStorage: req.DefaultStorage,
		AdminUsername:  req.AdminUsername,
		DisplayName:    req.DisplayName,
		PasswordHash:   passwordHash,
	})
	if errors.Is(err, persist.ErrAlreadyInitialized) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "installation already completed"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to initialize installation", err))
		return
	}

	api.app.Config.SiteName = req.SiteName
	session, ok, err := api.app.Auth.Login(r.Context(), req.AdminUsername, req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to create session", err))
		return
	}
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create initial session"})
		return
	}

	setSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusCreated, map[string]any{
		"initialized": true,
		"user":        user,
		"expiresAt":   session.ExpiresAt,
	})
}

func (api *API) policies(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("groupId"))
	group, rules, err := api.loadPolicyGroup(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, policy.ErrPolicyGroupNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load policy rules", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"group": group,
		"rules": rules,
	})
}

type replacePoliciesRequest struct {
	Rules []policy.Rule `json:"rules"`
}

type policyGroupRequest struct {
	Name                        string   `json:"name"`
	Description                 string   `json:"description"`
	DefaultDeliveryRouteID      string   `json:"defaultDeliveryRouteId"`
	AllowedDeliveryRouteIDs     []string `json:"allowedDeliveryRouteIds"`
	AllowDeliveryRouteSelection bool     `json:"allowDeliveryRouteSelection"`
}

type copyPolicyGroupRequest struct {
	Name string `json:"name"`
}

func (api *API) policyGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	groups, err := api.app.PolicyStore.PolicyGroups(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load policy groups", err))
		return
	}
	activeGroup, err := api.app.PolicyStore.ActivePolicyGroup(r.Context())
	if err != nil && !errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load active policy group", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"groups":      groups,
		"activeGroup": activeGroup,
	})
}

func (api *API) policyGroupDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	group, rules, err := api.app.PolicyStore.PolicyGroup(r.Context(), r.PathValue("id"))
	if errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load policy group", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group, "rules": rules})
}

func (api *API) createPolicyGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req policyGroupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid policy group payload", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "policy group name is required"})
		return
	}
	group, err := api.app.PolicyStore.CreatePolicyGroup(r.Context(), req.Name, req.Description)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to create policy group", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": group})
}

func (api *API) updatePolicyGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req policyGroupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid policy group payload", err))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "policy group name is required"})
		return
	}
	group, err := api.app.PolicyStore.UpdatePolicyGroup(r.Context(), policy.Group{
		ID:                          r.PathValue("id"),
		Name:                        req.Name,
		Description:                 req.Description,
		DefaultDeliveryRouteID:      strings.TrimSpace(req.DefaultDeliveryRouteID),
		AllowedDeliveryRouteIDs:     cleanStringList(req.AllowedDeliveryRouteIDs),
		AllowDeliveryRouteSelection: req.AllowDeliveryRouteSelection,
	})
	if errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to update policy group", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

func (api *API) deletePolicyGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	err := api.app.PolicyStore.DeletePolicyGroup(r.Context(), r.PathValue("id"))
	if errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
		return
	}
	if errors.Is(err, policy.ErrPolicyGroupInUse) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "active or default policy group cannot be deleted"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to delete policy group", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) copyPolicyGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req copyPolicyGroupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid copy policy group payload", err))
		return
	}
	group, err := api.app.PolicyStore.CopyPolicyGroup(r.Context(), r.PathValue("id"), strings.TrimSpace(req.Name))
	if errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to copy policy group", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": group})
}

func (api *API) activatePolicyGroup(w http.ResponseWriter, r *http.Request) {
	api.setPolicyGroupActive(w, r, true)
}

func (api *API) deactivatePolicyGroup(w http.ResponseWriter, r *http.Request) {
	api.setPolicyGroupActive(w, r, false)
}

func (api *API) setPolicyGroupActive(w http.ResponseWriter, r *http.Request, active bool) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	group, err := api.app.PolicyStore.SetPolicyGroupActive(r.Context(), r.PathValue("id"), active)
	if errors.Is(err, policy.ErrPolicyGroupNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
		return
	}
	if errors.Is(err, policy.ErrPolicyGroupInvalidState) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "at least one policy group must remain active"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to update policy group state", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

func (api *API) replacePolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}

	var req replacePoliciesRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid policy payload", err))
		return
	}
	if len(req.Rules) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "at least one policy rule is required"})
		return
	}
	normalizedRules, validationErrors := policy.ValidateRules(req.Rules)
	if len(validationErrors) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":            "policy validation failed",
			"validationErrors": validationErrors,
		})
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("groupId"))
	if groupID == "" {
		group, err := api.app.PolicyStore.ActivePolicyGroup(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorPayload("failed to resolve active policy group", err))
			return
		}
		groupID = group.ID
	}
	if err := api.app.PolicyStore.ReplaceRulesForGroup(r.Context(), groupID, normalizedRules); err != nil {
		if errors.Is(err, policy.ErrPolicyGroupNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy group not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to save policy rules", err))
		return
	}
	group, _, err := api.loadPolicyGroup(r.Context(), groupID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to reload policy group", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"group": group, "rules": normalizedRules})
}

func (api *API) effectivePolicy(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	filename := r.URL.Query().Get("filename")
	contentType := r.URL.Query().Get("contentType")
	resourceType := resource.Type(strings.TrimSpace(r.URL.Query().Get("resourceType")))
	extension := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("extension"))), ".")
	if resourceType != "" && !slices.Contains(resource.AllTypes(), resourceType) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid resource type"})
		return
	}
	size := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("size")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "size must be a non-negative integer"})
			return
		}
		size = parsed
	}
	action := policy.Action(strings.TrimSpace(r.URL.Query().Get("action")))
	if action == "" {
		action = policy.ActionUpload
	}

	var meta resource.Metadata
	if resourceType != "" {
		meta = resource.Metadata{
			Filename:    filename,
			Extension:   extension,
			Type:        resourceType,
			ContentType: contentType,
			Size:        size,
		}
	} else {
		meta = api.app.Detector.Detect(filename, contentType, nil, size)
	}
	policyGroup, decision, err := api.resolvePolicy(r.Context(), action, group, meta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to resolve policy", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"metadata":    meta,
		"decision":    decision,
		"policyGroup": policyGroup,
	})
}

type policyTestRequest struct {
	Action      policy.Action `json:"action"`
	Group       string        `json:"group"`
	Filename    string        `json:"filename"`
	ContentType string        `json:"contentType"`
	Size        int64         `json:"size"`
}

func (api *API) testPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}

	var req policyTestRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid policy test payload", err))
		return
	}

	if req.Action == "" {
		req.Action = policy.ActionUpload
	}
	if req.Group == "" {
		req.Group = policy.GroupGuest
	}
	if req.Size < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "size must be greater than or equal to 0",
		})
		return
	}

	meta := api.app.Detector.Detect(req.Filename, req.ContentType, nil, req.Size)
	policyGroup, decision, err := api.resolvePolicy(r.Context(), req.Action, req.Group, meta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to resolve policy", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"metadata":    meta,
		"decision":    decision,
		"policyGroup": policyGroup,
	})
}

type uploadError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type uploadItemResponse struct {
	Success     bool               `json:"success"`
	Status      int                `json:"status"`
	Filename    string             `json:"filename"`
	Metadata    resource.Metadata  `json:"metadata"`
	Resource    *resource.Record   `json:"resource,omitempty"`
	Links       *resource.Links    `json:"links,omitempty"`
	Decision    *policy.Decision   `json:"decision,omitempty"`
	Compression *compressionResult `json:"compression,omitempty"`
	Error       *uploadError       `json:"error,omitempty"`
}

type compressionResult struct {
	Applied         bool    `json:"applied"`
	OriginalBytes   int64   `json:"originalBytes"`
	CompressedBytes int64   `json:"compressedBytes"`
	Quality         int     `json:"quality"`
	Ratio           float64 `json:"ratio"`
}

type userGroupRequest struct {
	Name                       string `json:"name"`
	Description                string `json:"description"`
	TotalCapacityBytes         int64  `json:"totalCapacityBytes"`
	DefaultMonthlyTrafficBytes int64  `json:"defaultMonthlyTrafficBytes"`
	MaxFileSizeBytes           int64  `json:"maxFileSizeBytes"`
	DailyUploadLimit           int    `json:"dailyUploadLimit"`
	DailyIPUploadLimit         *int   `json:"dailyIpUploadLimit"`
	AllowHotlink               bool   `json:"allowHotlink"`
	ImageCompressionEnabled    *bool  `json:"imageCompressionEnabled"`
	ImageCompressionQuality    int    `json:"imageCompressionQuality"`
}

type accountUsageResponse struct {
	User                *auth.User        `json:"user,omitempty"`
	Group               persist.UserGroup `json:"group"`
	UsedStorageBytes    int64             `json:"usedStorageBytes"`
	MonthlyTrafficBytes int64             `json:"monthlyTrafficBytes"`
	DailyUploadCount    int               `json:"dailyUploadCount"`
}

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	GroupID     string `json:"groupId"`
	Status      string `json:"status"`
}

type updateUserRequest struct {
	DisplayName string `json:"displayName"`
	GroupID     string `json:"groupId"`
	Status      string `json:"status"`
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

type storageConfigRequest struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Name            string `json:"name"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	PublicBaseURL   string `json:"publicBaseUrl"`
	BasePath        string `json:"basePath"`
	UsePathStyle    bool   `json:"usePathStyle"`
	IsDefault       bool   `json:"isDefault"`
}

type deliveryRouteRequest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	PublicBaseURL string `json:"publicBaseUrl"`
	IsDefault     bool   `json:"isDefault"`
	IsEnabled     bool   `json:"isEnabled"`
}

type siteSettingsRequest struct {
	SiteName           string `json:"siteName"`
	ExternalBaseURL    string `json:"externalBaseUrl"`
	AllowGuestUploads  bool   `json:"allowGuestUploads"`
	ShowStatsOnHome    bool   `json:"showStatsOnHome"`
	ShowFeaturedOnHome bool   `json:"showFeaturedOnHome"`
}

type featuredResourceRequest struct {
	ResourceID string `json:"resourceId"`
	SortOrder  int    `json:"sortOrder"`
}

type featuredResourceOrderRequest struct {
	ResourceIDs []string `json:"resourceIds"`
}

type resourceVisibilityRequest struct {
	IsPrivate bool `json:"isPrivate"`
}

type signedLinkRequest struct {
	ExpiresInSeconds int64 `json:"expiresInSeconds"`
}

func (api *API) siteSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := api.app.Data.SiteSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load site settings", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (api *API) updateSiteSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req siteSettingsRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid site settings payload", err))
		return
	}
	req.SiteName = strings.TrimSpace(req.SiteName)
	req.ExternalBaseURL = strings.TrimRight(strings.TrimSpace(req.ExternalBaseURL), "/")
	if req.SiteName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "site name is required"})
		return
	}
	if req.ExternalBaseURL != "" && !strings.HasPrefix(req.ExternalBaseURL, "http://") && !strings.HasPrefix(req.ExternalBaseURL, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "external base url must start with http:// or https://"})
		return
	}
	settings, err := api.app.Data.UpdateSiteSettings(r.Context(), persist.SiteSettings{
		SiteName:           req.SiteName,
		ExternalBaseURL:    req.ExternalBaseURL,
		AllowGuestUploads:  req.AllowGuestUploads,
		ShowStatsOnHome:    req.ShowStatsOnHome,
		ShowFeaturedOnHome: req.ShowFeaturedOnHome,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to save site settings", err))
		return
	}
	api.app.Config.SiteName = settings.SiteName
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (api *API) uploadResource(w http.ResponseWriter, r *http.Request) {
	const maxUploadRequestBytes = 2 << 30
	const maxUploadFiles = 20

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBytes)
	clientAddr := clientIP(r)
	if allowed, resetAt, _ := api.uploadLimiter.Allow(clientAddr, time.Now()); !allowed {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": uploadError{
				Code:    "upload_rate_limited",
				Message: fmt.Sprintf("too many upload requests, retry after %s", resetAt.UTC().Format(time.RFC3339)),
			},
		})
		return
	}
	actor, hasActor := api.currentUserFromRequest(r)
	settings, err := api.app.Data.SiteSettings(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load site settings", err))
		return
	}
	if !hasActor && !settings.AllowGuestUploads {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": uploadError{Code: "guest_uploads_disabled", Message: "guest uploads are disabled"},
		})
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		status, uploadErr := multipartUploadError(err)
		writeJSON(w, status, map[string]any{
			"error": uploadErr,
		})
		return
	}

	group := policy.GroupGuest
	var actorPtr *auth.User
	if hasActor {
		group = strings.TrimSpace(actor.GroupID)
		if group == "" {
			group = policy.GroupGuest
		}
		actorPtr = &actor
	}

	files := append([]*multipart.FileHeader(nil), r.MultipartForm.File["file"]...)
	files = append(files, r.MultipartForm.File["files"]...)
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": uploadError{Code: "missing_file", Message: "file field is required"},
		})
		return
	}
	if len(files) > maxUploadFiles {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": uploadError{Code: "too_many_files", Message: fmt.Sprintf("at most %d files can be uploaded per request", maxUploadFiles)},
		})
		return
	}

	items := make([]uploadItemResponse, len(files))
	successes := 0
	requestedDeliveryRouteID := strings.TrimSpace(r.FormValue("deliveryRouteId"))
	deliveryRoute, err := api.resolveUploadDeliveryRoute(r.Context(), requestedDeliveryRouteID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": uploadError{Code: "delivery_route_invalid", Message: err.Error()},
		})
		return
	}
	publicBaseURL := api.publicResourceBaseURLForRoute(settings, deliveryRoute)
	workerCount := uploadWorkerCount(len(files))
	if api.shouldSerializeUploads(r.Context(), group) {
		workerCount = 1
	}
	if workerCount > 1 {
		var wg sync.WaitGroup
		jobs := make(chan int)
		for range workerCount {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for index := range jobs {
					items[index] = api.handleUploadFile(r.Context(), actorPtr, group, files[index], clientAddr, r.UserAgent(), publicBaseURL, deliveryRoute.ID)
				}
			}()
		}
		for index := range files {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
	} else {
		for index, header := range files {
			items[index] = api.handleUploadFile(r.Context(), actorPtr, group, header, clientAddr, r.UserAgent(), publicBaseURL, deliveryRoute.ID)
		}
	}
	for _, item := range items {
		if item.Success {
			successes++
		}
	}

	status := http.StatusCreated
	switch {
	case successes == len(items):
		status = http.StatusCreated
	case successes == 0:
		status = firstUploadStatus(items, http.StatusBadRequest)
	default:
		status = http.StatusMultiStatus
	}

	payload := map[string]any{
		"items": items,
		"summary": map[string]int{
			"total":     len(items),
			"succeeded": successes,
			"failed":    len(items) - successes,
		},
	}
	if len(items) == 1 {
		payload["metadata"] = items[0].Metadata
		payload["decision"] = items[0].Decision
		if items[0].Resource != nil {
			payload["resource"] = items[0].Resource
		}
		if items[0].Links != nil {
			payload["links"] = items[0].Links
		}
		if items[0].Error != nil {
			payload["error"] = items[0].Error
		}
	}
	writeJSON(w, status, payload)
}

func (api *API) accountUsage(w http.ResponseWriter, r *http.Request) {
	user, ok := api.currentUserFromRequest(r)
	if !ok {
		group, err := api.userGroupByID(r.Context(), policy.GroupGuest)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load guest usage", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"usage": accountUsageResponse{
				Group: group,
			},
		})
		return
	}

	usage, err := api.app.Data.UserUsage(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load account usage", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"usage": accountUsageResponse{
			User:                &usage.User,
			Group:               usage.Group,
			UsedStorageBytes:    usage.UsedStorageBytes,
			MonthlyTrafficBytes: usage.MonthlyTrafficBytes,
			DailyUploadCount:    usage.DailyUploadCount,
		},
	})
}

func (api *API) userGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	groups, err := api.app.Data.UserGroups(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load user groups", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (api *API) updateUserGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req userGroupRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid user group payload", err))
		return
	}
	if req.TotalCapacityBytes < 0 || req.DefaultMonthlyTrafficBytes < 0 || req.MaxFileSizeBytes < 0 || req.DailyUploadLimit < 0 || (req.DailyIPUploadLimit != nil && *req.DailyIPUploadLimit < 0) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "group limits must be greater than or equal to 0"})
		return
	}
	groupID := strings.TrimSpace(r.PathValue("id"))
	existing, err := api.userGroupByID(r.Context(), groupID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user group not found"})
		return
	}
	imageCompressionEnabled := existing.ImageCompressionEnabled
	if req.ImageCompressionEnabled != nil {
		imageCompressionEnabled = *req.ImageCompressionEnabled
	}
	imageCompressionQuality := existing.ImageCompressionQuality
	if req.ImageCompressionQuality > 0 {
		imageCompressionQuality = req.ImageCompressionQuality
	}
	if imageCompressionQuality == 0 {
		imageCompressionQuality = 50
	}
	if imageCompressionQuality < 50 || imageCompressionQuality > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image compression quality must be between 50 and 80"})
		return
	}
	dailyIPUploadLimit := existing.DailyIPUploadLimit
	if req.DailyIPUploadLimit != nil {
		dailyIPUploadLimit = *req.DailyIPUploadLimit
	}
	group := persist.UserGroup{
		ID:                         existing.ID,
		Name:                       firstNonEmpty(strings.TrimSpace(req.Name), existing.Name),
		Description:                strings.TrimSpace(req.Description),
		TotalCapacityBytes:         req.TotalCapacityBytes,
		DefaultMonthlyTrafficBytes: req.DefaultMonthlyTrafficBytes,
		MaxFileSizeBytes:           req.MaxFileSizeBytes,
		DailyUploadLimit:           req.DailyUploadLimit,
		DailyIPUploadLimit:         dailyIPUploadLimit,
		AllowHotlink:               req.AllowHotlink,
		ImageCompressionEnabled:    imageCompressionEnabled,
		ImageCompressionQuality:    imageCompressionQuality,
	}
	updated, err := api.app.Data.UpdateUserGroup(r.Context(), group)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to update user group", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": updated})
}

func (api *API) users(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	users, err := api.app.Data.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load users", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (api *API) createUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req createUserRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid user payload", err))
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Role = strings.TrimSpace(req.Role)
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.Status = strings.TrimSpace(req.Status)

	switch {
	case req.Username == "":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "username is required"})
		return
	case req.DisplayName == "":
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "display name is required"})
		return
	case len(req.Password) < 8:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password must be at least 8 characters"})
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}
	if req.GroupID == "" {
		if req.Role == auth.AdminRole {
			req.GroupID = policy.GroupAdmin
		} else {
			req.GroupID = policy.GroupUser
		}
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if !api.validUserStatus(req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid user status"})
		return
	}
	if req.Role != "user" && req.Role != auth.AdminRole {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid user role"})
		return
	}
	if _, err := api.userGroupByID(r.Context(), req.GroupID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user group not found"})
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to hash password", err))
		return
	}
	user, err := api.app.Data.CreateUser(r.Context(), persist.CreateUserParams{
		Username:     req.Username,
		DisplayName:  req.DisplayName,
		PasswordHash: passwordHash,
		Role:         req.Role,
		GroupID:      req.GroupID,
		Status:       req.Status,
	})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "users.username") {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "username already exists"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to create user", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (api *API) updateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := api.requireAdmin(w, r)
	if !ok {
		return
	}
	var req updateUserRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid user update payload", err))
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.Status = strings.TrimSpace(req.Status)
	if req.DisplayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "display name is required"})
		return
	}
	if req.GroupID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "group id is required"})
		return
	}
	if !api.validUserStatus(req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid user status"})
		return
	}
	if _, err := api.userGroupByID(r.Context(), req.GroupID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "user group not found"})
		return
	}
	userID := r.PathValue("id")
	if userID == admin.ID && req.Status != "active" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cannot disable the current admin account"})
		return
	}
	user, err := api.app.Data.UpdateUser(r.Context(), persist.UpdateUserParams{
		ID:          userID,
		DisplayName: req.DisplayName,
		GroupID:     req.GroupID,
		Status:      req.Status,
	})
	if errors.Is(err, auth.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to update user", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (api *API) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req resetPasswordRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid reset password payload", err))
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password must be at least 8 characters"})
		return
	}
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to hash password", err))
		return
	}
	if err := api.app.Data.SetUserPassword(r.Context(), r.PathValue("id"), passwordHash); errors.Is(err, auth.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to reset password", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) storageConfigs(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	configs, err := api.app.Data.StorageConfigs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load storage configs", err))
		return
	}
	defaultConfig, err := api.app.Data.DefaultStorageConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load default storage config", err))
		return
	}
	sanitized := make([]persist.StorageConfig, 0, len(configs))
	for _, cfg := range configs {
		sanitized = append(sanitized, sanitizeStorageConfig(cfg))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configs":       sanitized,
		"defaultConfig": sanitizeStorageConfig(defaultConfig),
	})
}

func (api *API) upsertStorageConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	cfg, err := api.decodeStorageConfigRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid storage config payload", err))
		return
	}
	cfg.ID = strings.TrimSpace(r.PathValue("id"))
	cfg, err = api.mergeStoredSecrets(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to merge storage config secrets", err))
		return
	}
	if err := validateStorageConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	saved, err := api.app.Data.UpsertStorageConfig(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to save storage config", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": sanitizeStorageConfig(saved)})
}

func (api *API) storageHealthCheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	cfg, err := api.decodeStorageConfigRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid storage health payload", err))
		return
	}
	cfg, err = api.mergeStoredSecrets(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to merge storage config secrets", err))
		return
	}
	if err := validateStorageConfig(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	store, err := api.app.Storages.Resolve(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("failed to initialize storage driver", err))
		return
	}
	if err := store.HealthCheck(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":     false,
			"error":  err.Error(),
			"config": sanitizeStorageConfig(cfg),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": sanitizeStorageConfig(cfg),
	})
}

func (api *API) deliveryRoutes(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	routes, err := api.app.Data.DeliveryRoutes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load delivery routes", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
}

func (api *API) upsertDeliveryRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req deliveryRouteRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid delivery route payload", err))
		return
	}
	route := persist.DeliveryRoute{
		ID:            firstNonEmpty(strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(req.ID)),
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		PublicBaseURL: strings.TrimRight(strings.TrimSpace(req.PublicBaseURL), "/"),
		IsDefault:     req.IsDefault,
		IsEnabled:     req.IsEnabled,
	}
	if route.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "delivery route name is required"})
		return
	}
	if route.PublicBaseURL != "" && !strings.HasPrefix(route.PublicBaseURL, "http://") && !strings.HasPrefix(route.PublicBaseURL, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "delivery route public base url must start with http:// or https://"})
		return
	}
	saved, err := api.app.Data.UpsertDeliveryRoute(r.Context(), route)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to save delivery route", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"route": saved})
}

func (api *API) deleteDeliveryRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	err := api.app.Data.DeleteDeliveryRoute(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "delivery route not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("failed to delete delivery route", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) deliveryRouteChoices(w http.ResponseWriter, r *http.Request) {
	routes, group, err := api.deliveryRouteChoicesForActivePolicy(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load delivery route choices", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"routes":                      routes,
		"defaultDeliveryRouteId":      group.DefaultDeliveryRouteID,
		"allowDeliveryRouteSelection": group.AllowDeliveryRouteSelection,
	})
}

func (api *API) featuredResources(w http.ResponseWriter, r *http.Request) {
	items, err := api.app.Data.FeaturedResources(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load featured resources", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"resources": items,
	})
}

func (api *API) addFeaturedResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req featuredResourceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid featured resource payload", err))
		return
	}
	record, err := api.app.Data.Resource(r.Context(), req.ResourceID)
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load resource", err))
		return
	}
	if record.IsPrivate {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "private resources cannot be featured"})
		return
	}
	item, err := api.app.Data.AddFeaturedResource(r.Context(), req.ResourceID, req.SortOrder)
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to add featured resource", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": item})
}

func (api *API) removeFeaturedResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	if err := api.app.Data.RemoveFeaturedResource(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, persist.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "featured resource not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to remove featured resource", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) reorderFeaturedResources(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req featuredResourceOrderRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid featured resource order payload", err))
		return
	}
	items, err := api.app.Data.ReorderFeaturedResources(r.Context(), req.ResourceIDs)
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "featured resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to reorder featured resources", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api *API) resources(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	params := resource.ListParams{
		Page:      parseIntDefault(r.URL.Query().Get("page"), 1),
		PageSize:  parseIntDefault(r.URL.Query().Get("pageSize"), 20),
		Search:    strings.TrimSpace(r.URL.Query().Get("search")),
		UserGroup: strings.TrimSpace(r.URL.Query().Get("userGroup")),
		Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
	}
	if params.Sort == "" {
		params.Sort = "created_desc"
	}
	if rawType := strings.TrimSpace(r.URL.Query().Get("type")); rawType != "" {
		params.Type = resource.Type(rawType)
		if !slices.Contains(resource.AllTypes(), params.Type) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid resource type"})
			return
		}
	}
	switch strings.TrimSpace(r.URL.Query().Get("status")) {
	case "", "all":
		params.IncludeDeleted = r.URL.Query().Get("includeDeleted") == "true" || strings.TrimSpace(r.URL.Query().Get("status")) == "all"
	case string(resource.StatusActive):
		params.Status = resource.StatusActive
	case string(resource.StatusDeleted):
		params.Status = resource.StatusDeleted
		params.IncludeDeleted = true
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid resource status"})
		return
	}

	result, err := api.app.Data.ListResources(r.Context(), params)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to list resources", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      result.Items,
		"resources":  result.Items,
		"page":       result.Page,
		"pageSize":   result.PageSize,
		"total":      result.Total,
		"totalPages": result.TotalPages,
	})
}

func (api *API) resourceDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	detail, err := api.app.Data.ResourceDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load resource", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": detail.Record,
		"detail":   detail,
	})
}

func (api *API) updateResourceVisibility(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req resourceVisibilityRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid resource visibility payload", err))
		return
	}

	record, err := api.app.Data.UpdateResourceVisibility(r.Context(), r.PathValue("id"), req.IsPrivate)
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to update resource visibility", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": record})
}

func (api *API) generateSignedResourceLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	var req signedLinkRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid signed link payload", err))
		return
	}
	if req.ExpiresInSeconds <= 0 {
		req.ExpiresInSeconds = 3600
	}
	if req.ExpiresInSeconds < 60 || req.ExpiresInSeconds > 7*24*3600 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "signed link expiry must be between 60 seconds and 7 days"})
		return
	}

	record, err := api.app.Data.Resource(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) || record.Status == resource.StatusDeleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load resource", err))
		return
	}
	expiresAt := time.Now().UTC().Add(time.Duration(req.ExpiresInSeconds) * time.Second)
	signedURL, err := api.signedResourceURL(r.Context(), record, expiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to generate signed resource link", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       signedURL,
		"expiresAt": expiresAt,
	})
}

func (api *API) deleteResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	record, err := api.app.Data.MarkResourceDeleted(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to delete resource", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": record})
}

func (api *API) restoreResource(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.requireAdmin(w, r); !ok {
		return
	}
	record, err := api.app.Data.RestoreResource(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to restore resource", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": record})
}

func (api *API) statsOverview(w http.ResponseWriter, r *http.Request) {
	stats, err := api.app.Data.ResourceStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load stats", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats})
}

type byteRange struct {
	start int64
	end   int64
}

func (br byteRange) length() int64 {
	return br.end - br.start + 1
}

func parseByteRange(raw string, size int64) (byteRange, bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return byteRange{}, false, true
	}
	if !strings.HasPrefix(raw, "bytes=") {
		return byteRange{}, true, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(raw, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return byteRange{}, true, false
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return byteRange{}, true, false
	}
	startRaw := strings.TrimSpace(spec[:dash])
	endRaw := strings.TrimSpace(spec[dash+1:])
	if startRaw == "" {
		suffixLength, err := strconv.ParseInt(endRaw, 10, 64)
		if err != nil || suffixLength <= 0 || size <= 0 {
			return byteRange{}, true, false
		}
		if suffixLength >= size {
			return byteRange{start: 0, end: size - 1}, true, true
		}
		return byteRange{start: size - suffixLength, end: size - 1}, true, true
	}

	start, err := strconv.ParseInt(startRaw, 10, 64)
	if err != nil || start < 0 || start >= size {
		return byteRange{}, true, false
	}
	end := size - 1
	if endRaw != "" {
		end, err = strconv.ParseInt(endRaw, 10, 64)
		if err != nil || end < start {
			return byteRange{}, true, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return byteRange{start: start, end: end}, true, true
}

func (api *API) serveResource(w http.ResponseWriter, r *http.Request) {
	record, err := api.app.Data.Resource(r.Context(), r.PathValue("id"))
	if errors.Is(err, persist.ErrNotFound) || record.Status == resource.StatusDeleted {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to load resource", err))
		return
	}

	meta := resource.Metadata{
		Filename:    record.OriginalName,
		Extension:   record.Extension,
		Type:        record.Type,
		ContentType: record.ContentType,
		Size:        record.Size,
	}
	_, decision, err := api.resolvePolicy(r.Context(), policy.ActionAccess, record.UserGroup, meta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to resolve policy", err))
		return
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "resource access rejected by policy", "decision": decision})
		return
	}
	viewer, hasViewer := api.currentUserFromRequest(r)
	signedAccess, signedErr := api.isValidSignedResourceRequest(r.Context(), r, record)
	if signedErr != nil {
		if errors.Is(signedErr, errSignedLinkExpired) || errors.Is(signedErr, errSignedLinkInvalid) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": signedErr.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to validate signed resource link", signedErr))
		return
	}
	if record.IsPrivate && !signedAccess && !canAccessPrivateResource(record, viewer, hasViewer) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "private resource requires authentication or a signed link"})
		return
	}
	groupConfig, err := api.userGroupByID(r.Context(), record.UserGroup)
	if err == nil && !groupConfig.AllowHotlink && !hasViewer && !signedAccess {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": api.limitErrorPayload("hotlink_disabled", "resource hotlinking is disabled for this user group", "user_group_access", 0, 0, record.ID),
		})
		return
	}

	requestedRange, hasRange, rangeOK := parseByteRange(r.Header.Get("Range"), record.Size)
	if !rangeOK {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", record.Size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	plannedBytes := record.Size
	countAccess := true
	if hasRange {
		plannedBytes = requestedRange.length()
		countAccess = requestedRange.start == 0
	}
	if r.Method == http.MethodHead {
		plannedBytes = 0
		countAccess = false
	}

	month := time.Now().Format("2006-01")
	currentMonthlyTraffic := record.MonthlyTraffic
	if record.MonthWindow != month {
		currentMonthlyTraffic = 0
	}
	limit := decision.Rule.MonthlyTrafficPerResourceBytes
	if limit > 0 && plannedBytes > 0 && currentMonthlyTraffic+plannedBytes > limit {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": api.limitErrorPayload("resource_monthly_traffic_exceeded", "resource monthly traffic limit exceeded", "resource_month", limit, currentMonthlyTraffic, record.ID),
		})
		return
	}
	if r.Method == http.MethodGet {
		if redirectURL := api.deliveryRouteRedirectURL(r, record); redirectURL != "" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Location", redirectURL)
			w.WriteHeader(http.StatusFound)
			return
		}
	}

	_, resourceStore, _, err := api.resolveStoreByID(r.Context(), record.StorageDriver)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to resolve resource storage", err))
		return
	}
	contentDisposition := ""
	if shouldForceAttachment(record, decision.Rule.DownloadDisposition) {
		contentDisposition = fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(record.OriginalName))
	}
	if r.Method == http.MethodGet {
		if redirector, ok := resourceStore.(storage.Redirector); ok {
			redirectURL, err := redirector.RedirectURL(r.Context(), record.ObjectKey, storage.RedirectOptions{
				Method:             http.MethodGet,
				Expires:            5 * time.Minute,
				ContentType:        record.ContentType,
				CacheControl:       decision.Rule.CacheControl,
				ContentDisposition: contentDisposition,
			})
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errorPayload("failed to generate storage redirect", err))
				return
			}
			if plannedBytes > 0 {
				api.addResourceTraffic(r.Context(), record, viewer, hasViewer, plannedBytes, countAccess)
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Location", redirectURL)
			w.WriteHeader(http.StatusFound)
			return
		}
	}
	file, err := resourceStore.Get(r.Context(), record.ObjectKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	if record.ContentType != "" {
		w.Header().Set("Content-Type", record.ContentType)
	}
	applyResourceSecurityHeaders(w, record)
	if decision.Rule.CacheControl != "" {
		w.Header().Set("Cache-Control", decision.Rule.CacheControl)
	}
	if contentDisposition != "" {
		w.Header().Set("Content-Disposition", contentDisposition)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	status := http.StatusOK
	contentLength := record.Size
	if hasRange {
		status = http.StatusPartialContent
		contentLength = requestedRange.length()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", requestedRange.start, requestedRange.end, record.Size))
		if requestedRange.start > 0 {
			if seeker, ok := file.(io.Seeker); ok {
				if _, err := seeker.Seek(requestedRange.start, io.SeekStart); err != nil {
					writeJSON(w, http.StatusInternalServerError, errorPayload("failed to seek resource", err))
					return
				}
			} else if _, err := io.CopyN(io.Discard, file, requestedRange.start); err != nil {
				writeJSON(w, http.StatusInternalServerError, errorPayload("failed to seek resource", err))
				return
			}
		}
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}

	var written int64
	if hasRange {
		written, _ = io.CopyN(w, file, contentLength)
	} else {
		written, _ = io.Copy(w, file)
	}
	if written > 0 {
		api.addResourceTraffic(r.Context(), record, viewer, hasViewer, written, countAccess)
	}
}

func (api *API) addResourceTraffic(ctx context.Context, record resource.Record, viewer auth.User, hasViewer bool, bytes int64, countAccess bool) {
	if bytes <= 0 {
		return
	}
	userID := ""
	if hasViewer {
		userID = viewer.ID
	}
	_, _ = api.app.Data.AddResourceTraffic(ctx, persist.AddTrafficParams{
		ResourceID:      record.ID,
		UserID:          userID,
		Bytes:           bytes,
		SkipAccessCount: !countAccess,
		AccessedAt:      time.Now(),
	})
}

func (api *API) handleUploadFile(ctx context.Context, actor *auth.User, group string, header *multipart.FileHeader, uploadIP, userAgent, publicBaseURL, deliveryRouteID string) uploadItemResponse {
	cleanedName := sanitizeUploadFilename(header.Filename)
	item := uploadItemResponse{
		Status:   http.StatusCreated,
		Filename: cleanedName,
	}

	file, err := header.Open()
	if err != nil {
		item.Status = http.StatusBadRequest
		item.Error = &uploadError{Code: "open_failed", Message: "failed to open uploaded file"}
		return item
	}
	defer file.Close()

	sniff := make([]byte, 512)
	n, _ := file.Read(sniff)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "rewind_failed", Message: "failed to rewind uploaded file"}
		return item
	}

	declaredContentType := normalizeContentType(header.Header.Get("Content-Type"))
	sniffContentType := normalizeContentType(http.DetectContentType(sniff[:n]))
	meta := api.app.Detector.Detect(cleanedName, declaredContentType, sniff[:n], header.Size)
	meta.ContentType = sniffContentType
	item.Metadata = meta
	if err := validateUploadMetadata(meta, sniffContentType); err != nil {
		item.Status = http.StatusBadRequest
		item.Error = &uploadError{Code: "content_type_mismatch", Message: err.Error()}
		return item
	}
	_, decision, err := api.resolvePolicy(ctx, policy.ActionUpload, group, meta)
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Decision = &decision
		item.Error = &uploadError{Code: "policy_failed", Message: "failed to resolve policy"}
		return item
	}
	item.Decision = &decision
	if !decision.Allowed {
		item.Status = http.StatusForbidden
		item.Error = &uploadError{Code: "policy_rejected", Message: decision.Reason}
		return item
	}

	tempFile, err := os.CreateTemp(api.app.Config.TempDir, "upload-*")
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "tempfile_failed", Message: "failed to create temporary upload file"}
		return item
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	limit := decision.Rule.MaxFileSizeBytes
	reader := io.Reader(file)
	if limit > 0 {
		reader = &io.LimitedReader{R: file, N: limit + 1}
	}

	written, err := io.Copy(tempFile, reader)
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "buffer_failed", Message: "failed to buffer uploaded file"}
		return item
	}
	if limit > 0 && written > limit {
		item.Status = http.StatusRequestEntityTooLarge
		item.Error = &uploadError{Code: "file_too_large", Message: "file size exceeds policy limit"}
		return item
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "rewind_failed", Message: "failed to rewind temporary upload file"}
		return item
	}

	meta.Size = written
	item.Metadata = meta

	imageWidth, imageHeight, imageDecoded := 0, 0, false
	if meta.Type == resource.TypeImage && shouldValidateImage(meta.Extension, meta.ContentType) {
		imageWidth, imageHeight, imageDecoded = decodeTempImage(tempFile)
		if !imageDecoded {
			item.Status = http.StatusBadRequest
			item.Error = &uploadError{Code: "invalid_image", Message: "image decode validation failed"}
			return item
		}
		if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
			item.Status = http.StatusInternalServerError
			item.Error = &uploadError{Code: "rewind_failed", Message: "failed to rewind temporary upload file"}
			return item
		}
	}

	groupConfig, err := api.userGroupByID(ctx, group)
	if err != nil {
		item.Status = http.StatusBadRequest
		item.Error = &uploadError{Code: "group_not_found", Message: "user group not found"}
		return item
	}
	if meta.Type == resource.TypeImage && imageDecoded && groupConfig.ImageCompressionEnabled {
		result, err := api.compressUploadedImage(ctx, tempFile, meta, groupConfig.ImageCompressionQuality)
		if err != nil {
			_, _ = tempFile.Seek(0, io.SeekStart)
		} else if result != nil {
			item.Compression = result
			meta.Size = result.CompressedBytes
			item.Metadata = meta
		}
	}
	if status, quotaErr := api.checkUploadQuota(ctx, actor, groupConfig, meta.Size, uploadIP); quotaErr != nil {
		item.Status = status
		item.Error = quotaErr
		return item
	}

	hash, finalSniff, finalSniffLen, err := hashAndSniffTempFile(tempFile)
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "hash_failed", Message: "failed to hash uploaded file"}
		return item
	}
	now := time.Now()
	objectKey := buildObjectKey(now, hash, meta.Extension)
	defaultStorageID, activeStore, activeConfig, err := api.resolveDefaultStore(ctx)
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "storage_unavailable", Message: "failed to resolve active storage"}
		return item
	}
	object, err := activeStore.Put(ctx, objectKey, tempFile)
	if err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "storage_failed", Message: "failed to store resource"}
		return item
	}

	id := hash[:16]
	directURL := strings.TrimRight(publicBaseURL, "/") + "/r/" + id
	monthlyLimit := decision.Rule.MonthlyTrafficPerResourceBytes
	if monthlyLimit <= 0 && groupConfig.DefaultMonthlyTrafficBytes > 0 {
		monthlyLimit = groupConfig.DefaultMonthlyTrafficBytes
	}
	record := resource.Record{
		ID:              id,
		OwnerUserID:     ownerUserID(actor),
		OwnerUsername:   ownerUsername(actor),
		UserGroup:       group,
		IsPrivate:       decision.Rule.ForcePrivate,
		StorageDriver:   defaultStorageID,
		ObjectKey:       object.Key,
		DeliveryRouteID: deliveryRouteID,
		PublicURL:       directURL,
		OriginalName:    cleanedName,
		Extension:       meta.Extension,
		Type:            meta.Type,
		Size:            meta.Size,
		ContentType:     meta.ContentType,
		Hash:            hash,
		Status:          resource.StatusActive,
		CacheControl:    decision.Rule.CacheControl,
		Disposition:     decision.Rule.DownloadDisposition,
		MonthlyLimit:    monthlyLimit,
		MonthWindow:     now.Format("2006-01"),
		CreatedAt:       now,
		UpdatedAt:       now,
		UploadIP:        uploadIP,
		UploadUserAgent: userAgent,
	}
	headerDigest := sha256.Sum256(finalSniff[:finalSniffLen])
	metadata := resource.StoredMetadata{
		ResourceID:   id,
		HeaderSHA256: hex.EncodeToString(headerDigest[:]),
		ImageWidth:   imageWidth,
		ImageHeight:  imageHeight,
		ImageDecoded: imageDecoded,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	variant := resource.Variant{
		ID:            id + "_original",
		ResourceID:    id,
		Kind:          "original",
		StorageDriver: activeConfig.ID,
		ObjectKey:     record.ObjectKey,
		ContentType:   record.ContentType,
		Size:          record.Size,
		Width:         imageWidth,
		Height:        imageHeight,
		CreatedAt:     now,
	}
	if err := api.app.Data.CreateResource(ctx, persist.CreateResourceBundle{
		Record:   record,
		Metadata: metadata,
		Variants: []resource.Variant{variant},
	}); err != nil {
		item.Status = http.StatusInternalServerError
		item.Error = &uploadError{Code: "persist_failed", Message: "failed to save resource record"}
		return item
	}

	links := resource.BuildLinks(cleanedName, directURL, meta.Type)
	item.Success = true
	item.Resource = &record
	item.Links = &links
	return item
}

func decodeTempImage(file *os.File) (int, int, bool) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

func (api *API) compressUploadedImage(ctx context.Context, file *os.File, meta resource.Metadata, quality int) (*compressionResult, error) {
	quality = clampImageQuality(quality)
	if !isCompressibleImage(meta) {
		return nil, nil
	}
	select {
	case api.compressionLimiter <- struct{}{}:
		defer func() { <-api.compressionLimiter }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	originalStat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	originalSize := originalStat.Size()

	compressed, err := os.CreateTemp(api.app.Config.TempDir, "compress-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(compressed.Name())
	defer compressed.Close()

	switch {
	case isJPEG(meta):
		err = jpeg.Encode(compressed, img, &jpeg.Options{Quality: quality})
	case isPNG(meta):
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		err = encoder.Encode(compressed, img)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	compressedStat, err := compressed.Stat()
	if err != nil {
		return nil, err
	}
	compressedSize := compressedStat.Size()
	if compressedSize <= 0 || compressedSize >= originalSize {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if _, err := compressed.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.Copy(file, compressed); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return &compressionResult{
		Applied:         true,
		OriginalBytes:   originalSize,
		CompressedBytes: compressedSize,
		Quality:         quality,
		Ratio:           1 - float64(compressedSize)/float64(originalSize),
	}, nil
}

func hashAndSniffTempFile(file *os.File) (string, []byte, int, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", nil, 0, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", nil, 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", nil, 0, err
	}
	sniff := make([]byte, 512)
	n, err := file.Read(sniff)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", nil, 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", nil, 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), sniff, n, nil
}

func isCompressibleImage(meta resource.Metadata) bool {
	return isJPEG(meta) || isPNG(meta)
}

func isJPEG(meta resource.Metadata) bool {
	ext := strings.ToLower(strings.TrimPrefix(meta.Extension, "."))
	return ext == "jpg" || ext == "jpeg" || strings.Contains(strings.ToLower(meta.ContentType), "jpeg")
}

func isPNG(meta resource.Metadata) bool {
	ext := strings.ToLower(strings.TrimPrefix(meta.Extension, "."))
	return ext == "png" || strings.Contains(strings.ToLower(meta.ContentType), "png")
}

func clampImageQuality(value int) int {
	if value < 50 {
		return 50
	}
	if value > 80 {
		return 80
	}
	return value
}

func shouldValidateImage(extension, contentType string) bool {
	switch strings.ToLower(strings.TrimPrefix(extension, ".")) {
	case "jpg", "jpeg", "png", "gif":
		return true
	}
	return strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "png") || strings.Contains(contentType, "gif")
}

func shouldForceAttachment(record resource.Record, disposition string) bool {
	if disposition == "attachment" {
		return true
	}
	return isDangerousResource(record)
}

func firstUploadStatus(items []uploadItemResponse, fallback int) int {
	for _, item := range items {
		if item.Status > 0 {
			return item.Status
		}
	}
	return fallback
}

func uploadWorkerCount(fileCount int) int {
	if fileCount <= 1 {
		return 1
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	if workers > fileCount {
		workers = fileCount
	}
	return workers
}

func compressionWorkerLimit() int {
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		return 1
	}
	if workers > 8 {
		return 8
	}
	return workers
}

func (api *API) shouldSerializeUploads(ctx context.Context, group string) bool {
	groupConfig, err := api.userGroupByID(ctx, group)
	if err != nil {
		return true
	}
	return groupConfig.TotalCapacityBytes > 0 || groupConfig.DailyUploadLimit > 0
}

func multipartUploadError(err error) (int, uploadError) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge, uploadError{
			Code:    "request_too_large",
			Message: "upload request exceeds server limit",
			Detail:  err.Error(),
		}
	}

	detail := err.Error()
	normalized := strings.ToLower(detail)
	message := "invalid multipart request"
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(normalized, "unexpected eof") || strings.Contains(normalized, "i/o timeout") {
		message = "upload request was interrupted before it completed"
	}
	return http.StatusBadRequest, uploadError{
		Code:    "invalid_multipart",
		Message: message,
		Detail:  detail,
	}
}

func parseIntDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	clientAddr := clientIP(r)
	if blocked, resetAt, _ := api.loginFailureLimiter.IsBlocked(clientAddr, time.Now()); blocked {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{
				"code":       "login_rate_limited",
				"message":    "too many failed login attempts",
				"retryAfter": resetAt.UTC().Format(time.RFC3339),
			},
		})
		return
	}

	var req loginRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid login payload", err))
		return
	}

	session, ok, err := api.app.Auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload("failed to create session", err))
		return
	}
	if !ok {
		blocked, resetAt, _ := api.loginFailureLimiter.AddFailure(clientAddr, time.Now())
		if blocked {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error": map[string]any{
					"code":       "login_rate_limited",
					"message":    "too many failed login attempts",
					"retryAfter": resetAt.UTC().Format(time.RFC3339),
				},
			})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "invalid username or password",
		})
		return
	}
	api.loginFailureLimiter.Reset(clientAddr)

	setSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      session.User,
		"expiresAt": session.ExpiresAt,
	})
}

func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		api.app.Auth.Logout(cookie.Value)
	}

	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *API) me(w http.ResponseWriter, r *http.Request) {
	user, ok := api.currentUserFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (api *API) currentUserFromRequest(r *http.Request) (auth.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return auth.User{}, false
	}
	return api.app.Auth.UserForToken(cookie.Value)
}

func (api *API) requireAuth(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := api.currentUserFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "authentication required",
		})
		return auth.User{}, false
	}
	return user, true
}

func (api *API) requireAdmin(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := api.requireAuth(w, r)
	if !ok {
		return auth.User{}, false
	}
	if user.Role != auth.AdminRole {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "admin role required",
		})
		return auth.User{}, false
	}

	return user, true
}

func (api *API) decodeStorageConfigRequest(r *http.Request) (persist.StorageConfig, error) {
	var req storageConfigRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return persist.StorageConfig{}, err
	}
	return persist.StorageConfig{
		ID:              firstNonEmpty(strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(req.ID)),
		Type:            strings.ToLower(strings.TrimSpace(req.Type)),
		Name:            strings.TrimSpace(req.Name),
		Endpoint:        strings.TrimSpace(req.Endpoint),
		Region:          strings.TrimSpace(req.Region),
		Bucket:          strings.TrimSpace(req.Bucket),
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(req.SecretAccessKey),
		Username:        strings.TrimSpace(req.Username),
		Password:        strings.TrimSpace(req.Password),
		PublicBaseURL:   strings.TrimSpace(req.PublicBaseURL),
		BasePath:        strings.Trim(strings.ReplaceAll(req.BasePath, "\\", "/"), "/"),
		UsePathStyle:    req.UsePathStyle,
		IsDefault:       req.IsDefault,
	}, nil
}

func (api *API) mergeStoredSecrets(ctx context.Context, cfg persist.StorageConfig) (persist.StorageConfig, error) {
	if cfg.ID == "" {
		return cfg, nil
	}
	existing, err := api.findStorageConfigByID(ctx, cfg.ID)
	if errors.Is(err, persist.ErrNotFound) {
		return cfg, nil
	}
	if err != nil {
		return persist.StorageConfig{}, err
	}
	if cfg.SecretAccessKey == "" {
		cfg.SecretAccessKey = existing.SecretAccessKey
	}
	if cfg.Password == "" {
		cfg.Password = existing.Password
	}
	if cfg.Name == "" {
		cfg.Name = existing.Name
	}
	return cfg, nil
}

func (api *API) publicResourceBaseURL(settings persist.SiteSettings) string {
	if base := strings.TrimRight(strings.TrimSpace(settings.ExternalBaseURL), "/"); base != "" {
		return base
	}
	return strings.TrimRight(strings.TrimSpace(api.app.Config.PublicBaseURL), "/")
}

func (api *API) publicResourceBaseURLForRoute(settings persist.SiteSettings, route persist.DeliveryRoute) string {
	if base := strings.TrimRight(strings.TrimSpace(route.PublicBaseURL), "/"); base != "" {
		return base
	}
	return api.publicResourceBaseURL(settings)
}

func (api *API) resolveUploadDeliveryRoute(ctx context.Context, requestedID string) (persist.DeliveryRoute, error) {
	routes, group, err := api.deliveryRouteChoicesForActivePolicy(ctx)
	if err != nil {
		return persist.DeliveryRoute{}, err
	}
	routeByID := map[string]persist.DeliveryRoute{}
	for _, route := range routes {
		routeByID[route.ID] = route
	}
	selectedID := strings.TrimSpace(group.DefaultDeliveryRouteID)
	if group.AllowDeliveryRouteSelection && strings.TrimSpace(requestedID) != "" {
		selectedID = strings.TrimSpace(requestedID)
	}
	if selectedID == "" {
		for _, route := range routes {
			if route.IsDefault {
				selectedID = route.ID
				break
			}
		}
	}
	route, ok := routeByID[selectedID]
	if !ok {
		return persist.DeliveryRoute{}, errors.New("selected delivery route is not allowed")
	}
	return route, nil
}

func (api *API) deliveryRouteChoicesForActivePolicy(ctx context.Context) ([]persist.DeliveryRoute, policy.Group, error) {
	routes, err := api.app.Data.DeliveryRoutes(ctx)
	if err != nil {
		return nil, policy.Group{}, err
	}
	group, err := api.app.PolicyStore.ActivePolicyGroup(ctx)
	if err != nil {
		return nil, policy.Group{}, err
	}
	enabled := make([]persist.DeliveryRoute, 0, len(routes))
	for _, route := range routes {
		if route.IsEnabled {
			enabled = append(enabled, route)
		}
	}
	if len(enabled) == 0 {
		enabled = append(enabled, persist.DeliveryRoute{ID: "default", Name: "默认线路", IsDefault: true, IsEnabled: true})
	}
	if strings.TrimSpace(group.DefaultDeliveryRouteID) == "" {
		for _, route := range enabled {
			if route.IsDefault {
				group.DefaultDeliveryRouteID = route.ID
				break
			}
		}
	}
	allowedIDs := cleanStringList(group.AllowedDeliveryRouteIDs)
	if len(allowedIDs) == 0 {
		group.AllowedDeliveryRouteIDs = routeIDs(enabled)
		return enabled, group, nil
	}
	allowedSet := map[string]bool{}
	for _, id := range allowedIDs {
		allowedSet[id] = true
	}
	filtered := enabled[:0]
	for _, route := range enabled {
		if allowedSet[route.ID] {
			filtered = append(filtered, route)
		}
	}
	if len(filtered) == 0 {
		filtered = enabled
		group.AllowedDeliveryRouteIDs = routeIDs(enabled)
	} else {
		group.AllowedDeliveryRouteIDs = allowedIDs
	}
	return filtered, group, nil
}

func (api *API) deliveryRouteRedirectURL(r *http.Request, record resource.Record) string {
	if strings.TrimSpace(record.DeliveryRouteID) == "" || strings.TrimSpace(record.DeliveryRouteID) == "default" || strings.TrimSpace(record.PublicURL) == "" {
		return ""
	}
	target, err := url.Parse(record.PublicURL)
	if err != nil || target.Host == "" {
		return ""
	}
	requestHosts := []string{requestHost(r), strings.TrimSpace(r.Header.Get("Host")), strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))}
	for _, host := range requestHosts {
		if strings.EqualFold(host, target.Host) {
			return ""
		}
	}
	if r.URL.RawQuery != "" {
		query := target.Query()
		for key, values := range r.URL.Query() {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		target.RawQuery = query.Encode()
	}
	return target.String()
}

func (api *API) resolveDefaultStore(ctx context.Context) (string, storage.Store, persist.StorageConfig, error) {
	cfg, err := api.app.Data.DefaultStorageConfig(ctx)
	if err != nil {
		return "", nil, persist.StorageConfig{}, err
	}
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = "local"
	}
	if strings.TrimSpace(cfg.Type) == "" {
		cfg.Type = "local"
	}
	if api.app.Storages == nil {
		if api.app.Storage == nil {
			return "", nil, persist.StorageConfig{}, errors.New("storage manager is not configured")
		}
		return cfg.ID, api.app.Storage, cfg, nil
	}
	store, err := api.app.Storages.Resolve(ctx, cfg)
	if err != nil {
		return "", nil, persist.StorageConfig{}, err
	}
	return cfg.ID, store, cfg, nil
}

func (api *API) resolveStoreByID(ctx context.Context, id string) (string, storage.Store, persist.StorageConfig, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "local" {
		localCfg := persist.StorageConfig{ID: "local", Type: "local", Name: "本机存储", IsDefault: true}
		if api.app.Storages == nil {
			if api.app.Storage == nil {
				return "", nil, persist.StorageConfig{}, errors.New("storage manager is not configured")
			}
			return "local", api.app.Storage, localCfg, nil
		}
		store, err := api.app.Storages.Resolve(ctx, localCfg)
		return "local", store, localCfg, err
	}
	cfg, err := api.findStorageConfigByID(ctx, id)
	if err != nil {
		return "", nil, persist.StorageConfig{}, err
	}
	if api.app.Storages == nil {
		if api.app.Storage == nil {
			return "", nil, persist.StorageConfig{}, errors.New("storage manager is not configured")
		}
		return cfg.ID, api.app.Storage, cfg, nil
	}
	store, err := api.app.Storages.Resolve(ctx, cfg)
	if err != nil {
		return "", nil, persist.StorageConfig{}, err
	}
	return cfg.ID, store, cfg, nil
}

func (api *API) findStorageConfigByID(ctx context.Context, id string) (persist.StorageConfig, error) {
	configs, err := api.app.Data.StorageConfigs(ctx)
	if err != nil {
		return persist.StorageConfig{}, err
	}
	for _, cfg := range configs {
		if cfg.ID == id {
			return cfg, nil
		}
	}
	return persist.StorageConfig{}, persist.ErrNotFound
}

func validateStorageConfig(cfg persist.StorageConfig) error {
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	switch cfg.Type {
	case "", "local":
		return nil
	case "s3":
		switch {
		case cfg.Endpoint == "":
			return errors.New("s3 endpoint is required")
		case cfg.Bucket == "":
			return errors.New("s3 bucket is required")
		case cfg.AccessKeyID == "":
			return errors.New("s3 access key is required")
		case cfg.SecretAccessKey == "":
			return errors.New("s3 secret key is required")
		default:
			return nil
		}
	case "webdav":
		if cfg.Endpoint == "" {
			return errors.New("webdav endpoint is required")
		}
		return nil
	default:
		return errors.New("unsupported storage type")
	}
}

func sanitizeStorageConfig(cfg persist.StorageConfig) persist.StorageConfig {
	cfg.SecretAccessKey = ""
	cfg.Password = ""
	return cfg
}

func (api *API) checkUploadQuota(ctx context.Context, actor *auth.User, group persist.UserGroup, size int64, uploadIP string) (int, *uploadError) {
	if group.MaxFileSizeBytes > 0 && size > group.MaxFileSizeBytes {
		return http.StatusRequestEntityTooLarge, &uploadError{
			Code:    "group_file_too_large",
			Message: "file size exceeds user group limit",
		}
	}
	if actor == nil {
		usedStorageBytes, dailyUploadCount, err := api.app.Data.AnonymousUsage(ctx, group.ID)
		if err != nil {
			return http.StatusInternalServerError, &uploadError{
				Code:    "usage_lookup_failed",
				Message: "failed to resolve account usage",
			}
		}
		if group.TotalCapacityBytes > 0 && usedStorageBytes+size > group.TotalCapacityBytes {
			return http.StatusForbidden, &uploadError{
				Code:    "storage_quota_exceeded",
				Message: "account storage quota exceeded",
			}
		}
		if group.DailyUploadLimit > 0 && dailyUploadCount+1 > group.DailyUploadLimit {
			return http.StatusTooManyRequests, &uploadError{
				Code:    "daily_upload_limit_exceeded",
				Message: "daily upload limit exceeded",
			}
		}
		if group.DailyIPUploadLimit > 0 && strings.TrimSpace(uploadIP) != "" {
			dailyIPUploadCount, err := api.app.Data.AnonymousIPDailyUploadCount(ctx, group.ID, uploadIP)
			if err != nil {
				return http.StatusInternalServerError, &uploadError{
					Code:    "usage_lookup_failed",
					Message: "failed to resolve ip usage",
				}
			}
			if dailyIPUploadCount+1 > group.DailyIPUploadLimit {
				return http.StatusTooManyRequests, &uploadError{
					Code:    "daily_ip_upload_limit_exceeded",
					Message: "same IP daily upload limit exceeded",
				}
			}
		}
		return 0, nil
	}
	usage, err := api.app.Data.UserUsage(ctx, actor.ID)
	if err != nil {
		return http.StatusInternalServerError, &uploadError{
			Code:    "usage_lookup_failed",
			Message: "failed to resolve account usage",
		}
	}
	if usage.Group.TotalCapacityBytes > 0 && usage.UsedStorageBytes+size > usage.Group.TotalCapacityBytes {
		return http.StatusForbidden, &uploadError{
			Code:    "storage_quota_exceeded",
			Message: "account storage quota exceeded",
		}
	}
	if usage.Group.DailyUploadLimit > 0 && usage.DailyUploadCount+1 > usage.Group.DailyUploadLimit {
		return http.StatusTooManyRequests, &uploadError{
			Code:    "daily_upload_limit_exceeded",
			Message: "daily upload limit exceeded",
		}
	}
	return 0, nil
}

func (api *API) userGroupByID(ctx context.Context, id string) (persist.UserGroup, error) {
	groups, err := api.app.Data.UserGroups(ctx)
	if err != nil {
		return persist.UserGroup{}, err
	}
	for _, group := range groups {
		if group.ID == id {
			return group, nil
		}
	}
	return persist.UserGroup{}, persist.ErrNotFound
}

func (api *API) validUserStatus(status string) bool {
	switch status {
	case "active", "disabled", "banned":
		return true
	default:
		return false
	}
}

func (api *API) limitErrorPayload(code, message, scope string, limit, used int64, resourceID string) map[string]any {
	payload := map[string]any{
		"code":    code,
		"message": message,
		"scope":   scope,
		"limit":   limit,
		"used":    used,
	}
	if resourceID != "" {
		payload["resourceId"] = resourceID
	}
	return payload
}

func ownerUserID(actor *auth.User) string {
	if actor == nil {
		return ""
	}
	return actor.ID
}

func ownerUsername(actor *auth.User) string {
	if actor == nil {
		return ""
	}
	return actor.Username
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func routeIDs(routes []persist.DeliveryRoute) []string {
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		if strings.TrimSpace(route.ID) != "" {
			ids = append(ids, route.ID)
		}
	}
	return ids
}

func (api *API) loadPolicyGroup(ctx context.Context, groupID string) (policy.Group, []policy.Rule, error) {
	if groupID != "" {
		return api.app.PolicyStore.PolicyGroup(ctx, groupID)
	}
	group, err := api.app.PolicyStore.ActivePolicyGroup(ctx)
	if err != nil {
		return policy.Group{}, nil, err
	}
	rules, err := api.app.PolicyStore.RulesForGroup(ctx, group.ID)
	return group, rules, err
}

func (api *API) resolvePolicy(ctx context.Context, action policy.Action, group string, meta resource.Metadata) (policy.Group, policy.Decision, error) {
	policyGroup, rules, err := api.loadPolicyGroup(ctx, "")
	if err != nil {
		return policy.Group{}, policy.Decision{}, err
	}
	return policyGroup, policy.NewResolver(rules).Resolve(action, group, meta), nil
}

func buildObjectKey(now time.Time, hash, ext string) string {
	name := hash
	if ext != "" {
		name += "." + strings.TrimPrefix(ext, ".")
	}
	return path.Join(now.Format("2006/01/02"), name)
}

func sanitizeFilename(filename string) string {
	filename = strings.ReplaceAll(filename, `"`, "")
	filename = strings.ReplaceAll(filename, "\\", "")
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "." || filename == "/" || filename == "" {
		return "download"
	}
	return filename
}

func errorPayload(message string, err error) map[string]any {
	payload := map[string]any{"error": message}
	if err != nil {
		payload["detail"] = err.Error()
	}
	return payload
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		fmt.Fprintf(w, `{"error":"failed to encode json","detail":%q}`, err.Error())
	}
}
