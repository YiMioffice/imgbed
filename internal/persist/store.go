package persist

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"machring/internal/auth"
	"machring/internal/policy"
	"machring/internal/resource"
)

var ErrNotFound = errors.New("not found")
var ErrAlreadyInitialized = errors.New("already initialized")

type InstallState struct {
	Initialized    bool   `json:"initialized"`
	SiteName       string `json:"siteName"`
	DefaultStorage string `json:"defaultStorage"`
	AdminUsername  string `json:"adminUsername"`
}

type InitializeParams struct {
	SiteName       string
	DefaultStorage string
	AdminUsername  string
	DisplayName    string
	PasswordHash   string
}

type CreateResourceBundle struct {
	Record   resource.Record
	Metadata resource.StoredMetadata
	Variants []resource.Variant
}

type UserGroup struct {
	ID                         string    `json:"id"`
	Name                       string    `json:"name"`
	Description                string    `json:"description"`
	TotalCapacityBytes         int64     `json:"totalCapacityBytes"`
	DefaultMonthlyTrafficBytes int64     `json:"defaultMonthlyTrafficBytes"`
	MaxFileSizeBytes           int64     `json:"maxFileSizeBytes"`
	DailyUploadLimit           int       `json:"dailyUploadLimit"`
	DailyIPUploadLimit         int       `json:"dailyIpUploadLimit"`
	AllowHotlink               bool      `json:"allowHotlink"`
	ImageCompressionEnabled    bool      `json:"imageCompressionEnabled"`
	ImageCompressionQuality    int       `json:"imageCompressionQuality"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

type UserUsage struct {
	User                auth.User `json:"user"`
	Group               UserGroup `json:"group"`
	UsedStorageBytes    int64     `json:"usedStorageBytes"`
	MonthlyTrafficBytes int64     `json:"monthlyTrafficBytes"`
	DailyUploadCount    int       `json:"dailyUploadCount"`
}

type CreateUserParams struct {
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	GroupID      string
	Status       string
}

type UpdateUserParams struct {
	ID          string
	DisplayName string
	GroupID     string
	Status      string
}

type StorageConfig struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Name            string    `json:"name"`
	Endpoint        string    `json:"endpoint"`
	Region          string    `json:"region"`
	Bucket          string    `json:"bucket"`
	AccessKeyID     string    `json:"accessKeyId"`
	SecretAccessKey string    `json:"secretAccessKey,omitempty"`
	Username        string    `json:"username,omitempty"`
	Password        string    `json:"password,omitempty"`
	PublicBaseURL   string    `json:"publicBaseUrl"`
	BasePath        string    `json:"basePath"`
	UsePathStyle    bool      `json:"usePathStyle"`
	IsDefault       bool      `json:"isDefault"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type DeliveryRoute struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	PublicBaseURL string    `json:"publicBaseUrl"`
	IsDefault     bool      `json:"isDefault"`
	IsEnabled     bool      `json:"isEnabled"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type SiteSettings struct {
	SiteName           string    `json:"siteName"`
	ExternalBaseURL    string    `json:"externalBaseUrl"`
	AllowGuestUploads  bool      `json:"allowGuestUploads"`
	ShowStatsOnHome    bool      `json:"showStatsOnHome"`
	ShowFeaturedOnHome bool      `json:"showFeaturedOnHome"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type FeaturedResource struct {
	Resource  resource.Record `json:"resource"`
	SortOrder int             `json:"sortOrder"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type AddTrafficParams struct {
	ResourceID      string
	UserID          string
	Bytes           int64
	SkipAccessCount bool
	AccessedAt      time.Time
}

type DataStore interface {
	policy.Store
	auth.CredentialStore
	CreateResource(ctx context.Context, bundle CreateResourceBundle) error
	ListResources(ctx context.Context, params resource.ListParams) (resource.ListResult, error)
	Resource(ctx context.Context, id string) (resource.Record, error)
	ResourceDetail(ctx context.Context, id string) (resource.Detail, error)
	UpdateResourceVisibility(ctx context.Context, id string, isPrivate bool) (resource.Record, error)
	MarkResourceDeleted(ctx context.Context, id string) (resource.Record, error)
	RestoreResource(ctx context.Context, id string) (resource.Record, error)
	AddResourceTraffic(ctx context.Context, params AddTrafficParams) (resource.Record, error)
	ResourceStats(ctx context.Context) (resource.Stats, error)
	UserGroups(ctx context.Context) ([]UserGroup, error)
	UpdateUserGroup(ctx context.Context, group UserGroup) (UserGroup, error)
	UserUsage(ctx context.Context, userID string) (UserUsage, error)
	AnonymousUsage(ctx context.Context, groupID string) (int64, int, error)
	AnonymousIPDailyUploadCount(ctx context.Context, groupID, uploadIP string) (int, error)
	ListUsers(ctx context.Context) ([]auth.User, error)
	CreateUser(ctx context.Context, params CreateUserParams) (auth.User, error)
	UpdateUser(ctx context.Context, params UpdateUserParams) (auth.User, error)
	SetUserPassword(ctx context.Context, userID, passwordHash string) error
	StorageConfigs(ctx context.Context) ([]StorageConfig, error)
	UpsertStorageConfig(ctx context.Context, cfg StorageConfig) (StorageConfig, error)
	DefaultStorageConfig(ctx context.Context) (StorageConfig, error)
	DeliveryRoutes(ctx context.Context) ([]DeliveryRoute, error)
	UpsertDeliveryRoute(ctx context.Context, route DeliveryRoute) (DeliveryRoute, error)
	DeleteDeliveryRoute(ctx context.Context, id string) error
	SiteSettings(ctx context.Context) (SiteSettings, error)
	UpdateSiteSettings(ctx context.Context, settings SiteSettings) (SiteSettings, error)
	FeaturedResources(ctx context.Context, includeInactive bool) ([]FeaturedResource, error)
	AddFeaturedResource(ctx context.Context, resourceID string, sortOrder int) (FeaturedResource, error)
	RemoveFeaturedResource(ctx context.Context, resourceID string) error
	ReorderFeaturedResources(ctx context.Context, resourceIDs []string) ([]FeaturedResource, error)
	SigningSecret(ctx context.Context) (string, error)
	InstallState(ctx context.Context) (InstallState, error)
	Initialize(ctx context.Context, params InitializeParams) (auth.User, error)
}

type Store struct {
	path string
	mu   sync.RWMutex
	data dataFile
}

type dataFile struct {
	Rules          []policy.Rule             `json:"rules"`
	Resources      []resource.Record         `json:"resources"`
	Metadatas      []resource.StoredMetadata `json:"metadatas"`
	Variants       []resource.Variant        `json:"variants"`
	TrafficWindows []resource.TrafficWindow  `json:"trafficWindows"`
	Groups         []UserGroup               `json:"groups"`
	Users          []auth.User               `json:"users"`
	StorageConfigs []StorageConfig           `json:"storageConfigs"`
	DeliveryRoutes []DeliveryRoute           `json:"deliveryRoutes"`
	SiteSettings   SiteSettings              `json:"siteSettings"`
	Featured       []FeaturedResource        `json:"featured"`
	AppSettings    map[string]string         `json:"appSettings"`
}

func New(path string, defaultRules []policy.Rule) (*Store, error) {
	store := &Store{path: path}
	if err := store.load(defaultRules); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Rules(_ context.Context) ([]policy.Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]policy.Rule(nil), s.data.Rules...), nil
}

func (s *Store) ReplaceRules(_ context.Context, rules []policy.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Rules = append([]policy.Rule(nil), rules...)
	return s.saveLocked()
}

func (s *Store) ReplaceRulesForGroup(ctx context.Context, groupID string, rules []policy.Rule) error {
	if groupID != "" && groupID != policy.DefaultGroupID {
		return policy.ErrPolicyGroupNotFound
	}
	return s.ReplaceRules(ctx, rules)
}

func (s *Store) ActivePolicyGroup(_ context.Context) (policy.Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	return policy.Group{
		ID:        policy.DefaultGroupID,
		Name:      policy.DefaultGroupName,
		IsActive:  true,
		IsDefault: true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Store) PolicyGroups(ctx context.Context) ([]policy.Group, error) {
	group, err := s.ActivePolicyGroup(ctx)
	if err != nil {
		return nil, err
	}
	return []policy.Group{group}, nil
}

func (s *Store) RulesForGroup(_ context.Context, groupID string) ([]policy.Rule, error) {
	if groupID != "" && groupID != policy.DefaultGroupID {
		return nil, policy.ErrPolicyGroupNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]policy.Rule(nil), s.data.Rules...), nil
}

func (s *Store) PolicyGroup(ctx context.Context, groupID string) (policy.Group, []policy.Rule, error) {
	group, err := s.ActivePolicyGroup(ctx)
	if err != nil {
		return policy.Group{}, nil, err
	}
	rules, err := s.RulesForGroup(ctx, groupID)
	return group, rules, err
}

func (s *Store) CreatePolicyGroup(_ context.Context, name, description string) (policy.Group, error) {
	return policy.Group{}, policy.ErrPolicyGroupInUse
}

func (s *Store) UpdatePolicyGroup(_ context.Context, group policy.Group) (policy.Group, error) {
	return policy.Group{}, policy.ErrPolicyGroupInUse
}

func (s *Store) DeletePolicyGroup(_ context.Context, groupID string) error {
	return policy.ErrPolicyGroupInUse
}

func (s *Store) CopyPolicyGroup(_ context.Context, sourceGroupID, name string) (policy.Group, error) {
	return policy.Group{}, policy.ErrPolicyGroupInUse
}

func (s *Store) SetPolicyGroupActive(_ context.Context, groupID string, active bool) (policy.Group, error) {
	if groupID != policy.DefaultGroupID || !active {
		return policy.Group{}, policy.ErrPolicyGroupInUse
	}
	return s.ActivePolicyGroup(context.Background())
}

func (s *Store) CreateResource(_ context.Context, bundle CreateResourceBundle) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := bundle.Record
	if record.Status == "" {
		record.Status = resource.StatusActive
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	record.UpdatedAt = record.CreatedAt
	s.data.Resources = append(s.data.Resources, record)

	if bundle.Metadata.ResourceID != "" {
		s.upsertMetadataLocked(bundle.Metadata)
	}
	for _, variant := range bundle.Variants {
		s.upsertVariantLocked(variant)
	}
	return s.saveLocked()
}

func (s *Store) ListResources(_ context.Context, params resource.ListParams) (resource.ListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filtered := make([]resource.Record, 0, len(s.data.Resources))
	search := strings.ToLower(strings.TrimSpace(params.Search))
	for _, record := range s.data.Resources {
		if !params.IncludeDeleted && params.Status == "" && record.Status == resource.StatusDeleted {
			continue
		}
		if params.Status != "" && record.Status != params.Status {
			continue
		}
		if params.Type != "" && record.Type != params.Type {
			continue
		}
		if params.UserGroup != "" && record.UserGroup != params.UserGroup {
			continue
		}
		if search != "" {
			target := strings.ToLower(record.OriginalName + " " + record.ID + " " + record.Extension)
			if !strings.Contains(target, search) {
				continue
			}
		}
		filtered = append(filtered, record)
	}

	sortResources(filtered, params.Sort)
	return paginateResources(filtered, params.Page, params.PageSize), nil
}

func (s *Store) Resource(_ context.Context, id string) (resource.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, record := range s.data.Resources {
		if record.ID == id {
			return record, nil
		}
	}
	return resource.Record{}, ErrNotFound
}

func (s *Store) ResourceDetail(_ context.Context, id string) (resource.Detail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.resourceLocked(id)
	if !ok {
		return resource.Detail{}, ErrNotFound
	}

	detail := resource.Detail{
		Record:         record,
		Metadata:       s.metadataLocked(id),
		Variants:       s.variantsLocked(id),
		TrafficWindows: s.trafficWindowsLocked(id),
		Links:          resource.BuildLinks(record.OriginalName, record.PublicURL, record.Type),
	}
	if detail.Variants == nil {
		detail.Variants = []resource.Variant{}
	}
	if detail.TrafficWindows == nil {
		detail.TrafficWindows = []resource.TrafficWindow{}
	}
	return detail, nil
}

func (s *Store) UpdateResourceVisibility(_ context.Context, id string, isPrivate bool) (resource.Record, error) {
	return s.updateResource(id, func(record *resource.Record) {
		record.IsPrivate = isPrivate
		record.UpdatedAt = time.Now()
		if isPrivate {
			filtered := s.data.Featured[:0]
			for _, item := range s.data.Featured {
				if item.Resource.ID != id {
					filtered = append(filtered, item)
				}
			}
			s.data.Featured = filtered
		}
	})
}

func (s *Store) MarkResourceDeleted(_ context.Context, id string) (resource.Record, error) {
	return s.updateResource(id, func(record *resource.Record) {
		now := time.Now()
		record.Status = resource.StatusDeleted
		record.DeletedAt = now
		record.UpdatedAt = now
		filtered := s.data.Featured[:0]
		for _, item := range s.data.Featured {
			if item.Resource.ID != id {
				filtered = append(filtered, item)
			}
		}
		s.data.Featured = filtered
	})
}

func (s *Store) RestoreResource(_ context.Context, id string) (resource.Record, error) {
	return s.updateResource(id, func(record *resource.Record) {
		now := time.Now()
		record.Status = resource.StatusActive
		record.DeletedAt = time.Time{}
		record.UpdatedAt = now
	})
}

func (s *Store) AddResourceTraffic(_ context.Context, params AddTrafficParams) (resource.Record, error) {
	if params.AccessedAt.IsZero() {
		params.AccessedAt = time.Now()
	}
	monthKey := params.AccessedAt.Format("2006-01")
	dayKey := params.AccessedAt.Format("2006-01-02")

	return s.updateResource(params.ResourceID, func(record *resource.Record) {
		if record.MonthWindow != monthKey {
			record.MonthWindow = monthKey
			record.MonthlyTraffic = 0
		}
		if !params.SkipAccessCount {
			record.AccessCount++
		}
		record.TrafficBytes += params.Bytes
		record.MonthlyTraffic += params.Bytes
		record.UpdatedAt = params.AccessedAt

		s.bumpTrafficWindowLocked(record.ID, params.UserID, record.Type, "day", dayKey, params.Bytes, !params.SkipAccessCount, params.AccessedAt)
		s.bumpTrafficWindowLocked(record.ID, params.UserID, record.Type, "month", monthKey, params.Bytes, !params.SkipAccessCount, params.AccessedAt)
	})
}

func (s *Store) ResourceStats(_ context.Context) (resource.Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	dayStart, dayEnd := dayBoundsUTC(now)
	stats := resource.Stats{}
	trafficByDay := map[string]int64{}

	for _, record := range s.data.Resources {
		stats.TotalResources++
		if record.Status == resource.StatusActive {
			stats.ActiveResources++
			stats.TotalStorageBytes += record.Size
		}
		stats.TotalTrafficBytes += record.TrafficBytes
		createdAt := record.CreatedAt.UTC()
		if !createdAt.Before(dayStart) && createdAt.Before(dayEnd) {
			stats.TodayUploads++
		}
	}

	for _, window := range s.data.TrafficWindows {
		if window.WindowType == "day" {
			trafficByDay[window.WindowKey] += window.TrafficBytes
		}
	}

	points := make([]resource.TrafficPoint, 0, 7)
	for i := 6; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format("2006-01-02")
		points = append(points, resource.TrafficPoint{
			Label: key[5:],
			Bytes: trafficByDay[key],
		})
	}
	stats.RecentTraffic = points
	return stats, nil
}

func (s *Store) UserGroups(_ context.Context) ([]UserGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups := slices.Clone(s.data.Groups)
	for i := range groups {
		groups[i] = normalizeUserGroup(groups[i])
	}
	return groups, nil
}

func (s *Store) UpdateUserGroup(_ context.Context, group UserGroup) (UserGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Groups {
		if s.data.Groups[i].ID == group.ID {
			group = normalizeUserGroup(group)
			group.CreatedAt = s.data.Groups[i].CreatedAt
			group.UpdatedAt = time.Now()
			s.data.Groups[i] = group
			return group, s.saveLocked()
		}
	}
	group = normalizeUserGroup(group)
	group.CreatedAt = time.Now()
	group.UpdatedAt = group.CreatedAt
	s.data.Groups = append(s.data.Groups, group)
	return group, s.saveLocked()
}

func (s *Store) UserUsage(_ context.Context, userID string) (UserUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.data.Users {
		if user.ID == userID {
			usage := UserUsage{User: user}
			for _, group := range s.data.Groups {
				if group.ID == user.GroupID {
					usage.Group = normalizeUserGroup(group)
					break
				}
			}
			dayStart, dayEnd := dayBoundsUTC(time.Now())
			monthKey := time.Now().UTC().Format("2006-01")
			for _, record := range s.data.Resources {
				if record.OwnerUserID != userID || record.Status != resource.StatusActive {
					continue
				}
				usage.UsedStorageBytes += record.Size
				createdAt := record.CreatedAt.UTC()
				if !createdAt.Before(dayStart) && createdAt.Before(dayEnd) {
					usage.DailyUploadCount++
				}
			}
			for _, window := range s.data.TrafficWindows {
				if window.UserID == userID && window.WindowType == "month" && window.WindowKey == monthKey {
					usage.MonthlyTrafficBytes += window.TrafficBytes
				}
			}
			return usage, nil
		}
	}
	return UserUsage{}, ErrNotFound
}

func (s *Store) AnonymousUsage(_ context.Context, groupID string) (int64, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var usedStorageBytes int64
	dailyUploadCount := 0
	dayStart, dayEnd := dayBoundsUTC(time.Now())
	for _, record := range s.data.Resources {
		if record.UserGroup != groupID || record.OwnerUserID != "" || record.Status != resource.StatusActive {
			continue
		}
		usedStorageBytes += record.Size
		createdAt := record.CreatedAt.UTC()
		if !createdAt.Before(dayStart) && createdAt.Before(dayEnd) {
			dailyUploadCount++
		}
	}
	return usedStorageBytes, dailyUploadCount, nil
}

func (s *Store) AnonymousIPDailyUploadCount(_ context.Context, groupID, uploadIP string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dailyUploadCount := 0
	dayStart, dayEnd := dayBoundsUTC(time.Now())
	for _, record := range s.data.Resources {
		if record.UserGroup != groupID || record.OwnerUserID != "" || record.UploadIP != uploadIP {
			continue
		}
		createdAt := record.CreatedAt.UTC()
		if !createdAt.Before(dayStart) && createdAt.Before(dayEnd) {
			dailyUploadCount++
		}
	}
	return dailyUploadCount, nil
}

func (s *Store) ListUsers(_ context.Context) ([]auth.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.data.Users), nil
}

func (s *Store) CreateUser(_ context.Context, params CreateUserParams) (auth.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := auth.User{ID: params.Username, Username: params.Username, DisplayName: params.DisplayName, Role: params.Role, GroupID: params.GroupID, GroupName: params.GroupID, Status: params.Status}
	s.data.Users = append(s.data.Users, user)
	return user, s.saveLocked()
}

func (s *Store) UpdateUser(_ context.Context, params UpdateUserParams) (auth.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Users {
		if s.data.Users[i].ID == params.ID {
			s.data.Users[i].DisplayName = params.DisplayName
			s.data.Users[i].GroupID = params.GroupID
			s.data.Users[i].GroupName = params.GroupID
			s.data.Users[i].Status = params.Status
			return s.data.Users[i], s.saveLocked()
		}
	}
	return auth.User{}, ErrNotFound
}

func (s *Store) SetUserPassword(_ context.Context, userID, passwordHash string) error {
	_ = userID
	_ = passwordHash
	return nil
}

func (s *Store) StorageConfigs(_ context.Context) ([]StorageConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.data.StorageConfigs), nil
}

func (s *Store) UpsertStorageConfig(_ context.Context, cfg StorageConfig) (StorageConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.ID == "" {
		cfg.ID = strings.ToLower(strings.ReplaceAll(cfg.Name, " ", "-"))
		if cfg.ID == "" {
			cfg.ID = cfg.Type
		}
		if cfg.ID == "" {
			cfg.ID = "storage"
		}
	}
	if cfg.IsDefault {
		for i := range s.data.StorageConfigs {
			s.data.StorageConfigs[i].IsDefault = false
		}
	}
	for i := range s.data.StorageConfigs {
		if s.data.StorageConfigs[i].ID == cfg.ID {
			cfg.CreatedAt = s.data.StorageConfigs[i].CreatedAt
			cfg.UpdatedAt = time.Now()
			s.data.StorageConfigs[i] = cfg
			return cfg, s.saveLocked()
		}
	}
	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = cfg.CreatedAt
	s.data.StorageConfigs = append(s.data.StorageConfigs, cfg)
	return cfg, s.saveLocked()
}

func (s *Store) DefaultStorageConfig(_ context.Context) (StorageConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cfg := range s.data.StorageConfigs {
		if cfg.IsDefault {
			return cfg, nil
		}
	}
	return StorageConfig{ID: "local", Type: "local", Name: "本机存储", IsDefault: true}, nil
}

func (s *Store) DeliveryRoutes(_ context.Context) ([]DeliveryRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeDeliveryRoutes(s.data.DeliveryRoutes), nil
}

func (s *Store) UpsertDeliveryRoute(_ context.Context, route DeliveryRoute) (DeliveryRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route = normalizeDeliveryRoute(route)
	if route.ID == "" {
		route.ID = strings.ToLower(strings.ReplaceAll(route.Name, " ", "-"))
		if route.ID == "" {
			route.ID = "route"
		}
	}
	if route.IsDefault {
		for i := range s.data.DeliveryRoutes {
			s.data.DeliveryRoutes[i].IsDefault = false
		}
	}
	for i := range s.data.DeliveryRoutes {
		if s.data.DeliveryRoutes[i].ID == route.ID {
			route.CreatedAt = s.data.DeliveryRoutes[i].CreatedAt
			route.UpdatedAt = time.Now()
			s.data.DeliveryRoutes[i] = route
			return route, s.saveLocked()
		}
	}
	route.CreatedAt = time.Now()
	route.UpdatedAt = route.CreatedAt
	s.data.DeliveryRoutes = append(s.data.DeliveryRoutes, route)
	return route, s.saveLocked()
}

func (s *Store) DeleteDeliveryRoute(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, route := range s.data.DeliveryRoutes {
		if route.ID == id {
			if route.IsDefault {
				return errors.New("default delivery route cannot be deleted")
			}
			s.data.DeliveryRoutes = append(s.data.DeliveryRoutes[:i], s.data.DeliveryRoutes[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) SigningSecret(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.AppSettings == nil {
		s.data.AppSettings = map[string]string{}
	}
	if secret := strings.TrimSpace(s.data.AppSettings["resource_signing_secret"]); secret != "" {
		return secret, nil
	}
	secret := "dev-signing-secret"
	s.data.AppSettings["resource_signing_secret"] = secret
	return secret, s.saveLocked()
}

func (s *Store) updateResource(id string, mutate func(*resource.Record)) (resource.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Resources {
		if s.data.Resources[i].ID == id {
			mutate(&s.data.Resources[i])
			if err := s.saveLocked(); err != nil {
				return resource.Record{}, err
			}
			return s.data.Resources[i], nil
		}
	}
	return resource.Record{}, ErrNotFound
}

func (s *Store) resourceLocked(id string) (resource.Record, bool) {
	for _, record := range s.data.Resources {
		if record.ID == id {
			return record, true
		}
	}
	return resource.Record{}, false
}

func (s *Store) metadataLocked(resourceID string) resource.StoredMetadata {
	for _, item := range s.data.Metadatas {
		if item.ResourceID == resourceID {
			return item
		}
	}
	return resource.StoredMetadata{}
}

func (s *Store) variantsLocked(resourceID string) []resource.Variant {
	variants := make([]resource.Variant, 0, 1)
	for _, item := range s.data.Variants {
		if item.ResourceID == resourceID {
			variants = append(variants, item)
		}
	}
	sort.Slice(variants, func(i, j int) bool {
		return variants[i].CreatedAt.Before(variants[j].CreatedAt)
	})
	return variants
}

func (s *Store) trafficWindowsLocked(resourceID string) []resource.TrafficWindow {
	windows := make([]resource.TrafficWindow, 0, 2)
	for _, item := range s.data.TrafficWindows {
		if item.ResourceID == resourceID {
			windows = append(windows, item)
		}
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].WindowType == windows[j].WindowType {
			return windows[i].WindowKey > windows[j].WindowKey
		}
		return windows[i].WindowType < windows[j].WindowType
	})
	return windows
}

func (s *Store) upsertMetadataLocked(metadata resource.StoredMetadata) {
	for i := range s.data.Metadatas {
		if s.data.Metadatas[i].ResourceID == metadata.ResourceID {
			s.data.Metadatas[i] = metadata
			return
		}
	}
	s.data.Metadatas = append(s.data.Metadatas, metadata)
}

func (s *Store) upsertVariantLocked(variant resource.Variant) {
	for i := range s.data.Variants {
		if s.data.Variants[i].ID == variant.ID {
			s.data.Variants[i] = variant
			return
		}
	}
	s.data.Variants = append(s.data.Variants, variant)
}

func (s *Store) bumpTrafficWindowLocked(resourceID, userID string, resourceType resource.Type, windowType, windowKey string, bytes int64, countAccess bool, accessedAt time.Time) {
	requestCount := int64(0)
	if countAccess {
		requestCount = 1
	}
	for i := range s.data.TrafficWindows {
		item := &s.data.TrafficWindows[i]
		if item.ResourceID == resourceID && item.UserID == userID && item.WindowType == windowType && item.WindowKey == windowKey {
			item.RequestCount += requestCount
			item.TrafficBytes += bytes
			item.UpdatedAt = accessedAt
			return
		}
	}
	s.data.TrafficWindows = append(s.data.TrafficWindows, resource.TrafficWindow{
		ResourceID:   resourceID,
		UserID:       userID,
		ResourceType: resourceType,
		WindowType:   windowType,
		WindowKey:    windowKey,
		RequestCount: requestCount,
		TrafficBytes: bytes,
		UpdatedAt:    accessedAt,
	})
}

func (s *Store) load(defaultRules []policy.Rule) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data.Rules = append([]policy.Rule(nil), defaultRules...)
		return s.saveLocked()
	}
	if err != nil {
		return err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&s.data); err != nil {
		return err
	}
	if s.data.AppSettings == nil {
		s.data.AppSettings = map[string]string{}
	}
	s.data.DeliveryRoutes = normalizeDeliveryRoutes(s.data.DeliveryRoutes)
	if len(s.data.Rules) == 0 {
		s.data.Rules = append([]policy.Rule(nil), defaultRules...)
		return s.saveLocked()
	}
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

func sortResources(records []resource.Record, sortBy string) {
	switch sortBy {
	case "created_asc":
		sort.Slice(records, func(i, j int) bool {
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		})
	default:
		sort.Slice(records, func(i, j int) bool {
			return records[i].CreatedAt.After(records[j].CreatedAt)
		})
	}
}

func paginateResources(records []resource.Record, page, pageSize int) resource.ListResult {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	total := len(records)
	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(pageSize)))
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	items := slices.Clone(records[start:end])
	return resource.ListResult{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

func normalizeUserGroup(group UserGroup) UserGroup {
	if group.ImageCompressionQuality == 0 {
		group.ImageCompressionQuality = 50
		group.ImageCompressionEnabled = true
	}
	if group.ImageCompressionQuality < 50 {
		group.ImageCompressionQuality = 50
	}
	if group.ImageCompressionQuality > 80 {
		group.ImageCompressionQuality = 80
	}
	return group
}

func normalizeDeliveryRoutes(routes []DeliveryRoute) []DeliveryRoute {
	if len(routes) == 0 {
		now := time.Now()
		return []DeliveryRoute{{
			ID:            "default",
			Name:          "默认线路",
			Description:   "使用站点外部访问地址或服务默认地址。",
			PublicBaseURL: "",
			IsDefault:     true,
			IsEnabled:     true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}}
	}
	next := slices.Clone(routes)
	hasDefault := false
	for i := range next {
		next[i] = normalizeDeliveryRoute(next[i])
		if next[i].IsDefault {
			if hasDefault {
				next[i].IsDefault = false
			} else {
				hasDefault = true
			}
		}
	}
	if !hasDefault {
		next[0].IsDefault = true
	}
	return next
}

func normalizeDeliveryRoute(route DeliveryRoute) DeliveryRoute {
	route.ID = strings.TrimSpace(route.ID)
	route.Name = strings.TrimSpace(route.Name)
	route.Description = strings.TrimSpace(route.Description)
	route.PublicBaseURL = strings.TrimRight(strings.TrimSpace(route.PublicBaseURL), "/")
	if route.Name == "" {
		route.Name = route.ID
	}
	if route.ID == "" && route.Name == "" {
		route.ID = "default"
		route.Name = "默认线路"
	}
	return route
}

func dayBoundsUTC(now time.Time) (time.Time, time.Time) {
	start := now.UTC().Truncate(24 * time.Hour)
	return start, start.Add(24 * time.Hour)
}
