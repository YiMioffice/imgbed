package policy

import (
	"time"

	"machring/internal/resource"
)

type Action string

const (
	ActionUpload Action = "upload"
	ActionAccess Action = "access"
)

const (
	GroupGuest = "guest"
	GroupUser  = "user"
	GroupAdmin = "admin"
)

type Rule struct {
	UserGroup                         string        `json:"userGroup"`
	ResourceType                      resource.Type `json:"resourceType"`
	Extension                         string        `json:"extension,omitempty"`
	AllowUpload                       bool          `json:"allowUpload"`
	AllowAccess                       bool          `json:"allowAccess"`
	MaxFileSizeBytes                  int64         `json:"maxFileSizeBytes"`
	MonthlyTrafficPerResourceBytes    int64         `json:"monthlyTrafficPerResourceBytes"`
	MonthlyTrafficPerUserAndTypeBytes int64         `json:"monthlyTrafficPerUserAndTypeBytes"`
	RequireAuth                       bool          `json:"requireAuth"`
	RequireReview                     bool          `json:"requireReview"`
	ForcePrivate                      bool          `json:"forcePrivate"`
	CacheControl                      string        `json:"cacheControl,omitempty"`
	DownloadDisposition               string        `json:"downloadDisposition,omitempty"`
}

type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Rule    Rule   `json:"rule"`
}

type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"isActive"`
	IsDefault   bool      `json:"isDefault"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
