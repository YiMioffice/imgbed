package resource

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type Type string

const (
	TypeImage      Type = "image"
	TypeScript     Type = "script"
	TypeStylesheet Type = "stylesheet"
	TypeArchive    Type = "archive"
	TypeExecutable Type = "executable"
	TypeDocument   Type = "document"
	TypeFont       Type = "font"
	TypeVideo      Type = "video"
	TypeOther      Type = "other"
)

type Metadata struct {
	Filename    string `json:"filename"`
	Extension   string `json:"extension"`
	Type        Type   `json:"type"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

type StoredMetadata struct {
	ResourceID   string    `json:"resourceId"`
	HeaderSHA256 string    `json:"headerSha256"`
	ImageWidth   int       `json:"imageWidth"`
	ImageHeight  int       `json:"imageHeight"`
	ImageDecoded bool      `json:"imageDecoded"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Status string

const (
	StatusActive  Status = "active"
	StatusDeleted Status = "deleted"
)

type Record struct {
	ID              string    `json:"id"`
	OwnerUserID     string    `json:"ownerUserId,omitempty"`
	OwnerUsername   string    `json:"ownerUsername,omitempty"`
	UserGroup       string    `json:"userGroup"`
	IsPrivate       bool      `json:"isPrivate"`
	StorageDriver   string    `json:"storageDriver"`
	ObjectKey       string    `json:"objectKey"`
	DeliveryRouteID string    `json:"deliveryRouteId,omitempty"`
	PublicURL       string    `json:"publicUrl"`
	OriginalName    string    `json:"originalName"`
	Extension       string    `json:"extension"`
	Type            Type      `json:"type"`
	Size            int64     `json:"size"`
	ContentType     string    `json:"contentType"`
	Hash            string    `json:"hash"`
	Status          Status    `json:"status"`
	AccessCount     int64     `json:"accessCount"`
	TrafficBytes    int64     `json:"trafficBytes"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	DeletedAt       time.Time `json:"deletedAt,omitempty"`
	CacheControl    string    `json:"cacheControl,omitempty"`
	Disposition     string    `json:"disposition,omitempty"`
	MonthlyLimit    int64     `json:"monthlyLimit"`
	MonthlyTraffic  int64     `json:"monthlyTraffic"`
	MonthWindow     string    `json:"monthWindow"`
	UploadIP        string    `json:"uploadIp,omitempty"`
	UploadUserAgent string    `json:"uploadUserAgent,omitempty"`
}

type Variant struct {
	ID            string    `json:"id"`
	ResourceID    string    `json:"resourceId"`
	Kind          string    `json:"kind"`
	StorageDriver string    `json:"storageDriver"`
	ObjectKey     string    `json:"objectKey"`
	ContentType   string    `json:"contentType"`
	Size          int64     `json:"size"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Links struct {
	Direct   string `json:"direct"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	BBCode   string `json:"bbcode"`
}

type TrafficWindow struct {
	ResourceID   string    `json:"resourceId"`
	UserID       string    `json:"userId,omitempty"`
	ResourceType Type      `json:"resourceType"`
	WindowType   string    `json:"windowType"`
	WindowKey    string    `json:"windowKey"`
	RequestCount int64     `json:"requestCount"`
	TrafficBytes int64     `json:"trafficBytes"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type TrafficPoint struct {
	Label string `json:"label"`
	Bytes int64  `json:"bytes"`
}

type Detail struct {
	Record         Record          `json:"record"`
	Metadata       StoredMetadata  `json:"metadata"`
	Variants       []Variant       `json:"variants"`
	Links          Links           `json:"links"`
	TrafficWindows []TrafficWindow `json:"trafficWindows"`
}

type ListParams struct {
	Page           int
	PageSize       int
	Search         string
	Type           Type
	Status         Status
	UserGroup      string
	IncludeDeleted bool
	Sort           string
}

type ListResult struct {
	Items      []Record `json:"items"`
	Total      int      `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"pageSize"`
	TotalPages int      `json:"totalPages"`
}

type Stats struct {
	TotalResources    int            `json:"totalResources"`
	ActiveResources   int            `json:"activeResources"`
	TotalStorageBytes int64          `json:"totalStorageBytes"`
	TotalTrafficBytes int64          `json:"totalTrafficBytes"`
	TodayUploads      int            `json:"todayUploads"`
	RecentTraffic     []TrafficPoint `json:"recentTraffic"`
}

func AllTypes() []Type {
	return []Type{
		TypeImage,
		TypeScript,
		TypeStylesheet,
		TypeArchive,
		TypeExecutable,
		TypeDocument,
		TypeFont,
		TypeVideo,
		TypeOther,
	}
}

type Detector struct{}

func (Detector) Detect(filename, declaredContentType string, sniff []byte, size int64) Metadata {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	contentType := declaredContentType
	if contentType == "" && len(sniff) > 0 {
		contentType = http.DetectContentType(sniff)
	}

	return Metadata{
		Filename:    filename,
		Extension:   ext,
		Type:        classify(ext, contentType),
		ContentType: contentType,
		Size:        size,
	}
}

func classify(ext, contentType string) Type {
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "ico", "avif":
		return TypeImage
	case "svg", "html", "htm", "xhtml":
		return TypeOther
	case "js", "mjs", "cjs":
		return TypeScript
	case "css":
		return TypeStylesheet
	case "zip", "rar", "7z", "tar", "gz", "tgz":
		return TypeArchive
	case "exe", "msi", "bat", "cmd", "sh":
		return TypeExecutable
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "md":
		return TypeDocument
	case "woff", "woff2", "ttf", "otf", "eot":
		return TypeFont
	case "mp4", "webm", "mov", "mkv", "avi":
		return TypeVideo
	}

	if strings.HasPrefix(contentType, "image/") {
		if strings.Contains(contentType, "svg") {
			return TypeOther
		}
		return TypeImage
	}
	if strings.Contains(contentType, "html") {
		return TypeOther
	}
	if strings.Contains(contentType, "javascript") {
		return TypeScript
	}
	if strings.Contains(contentType, "css") {
		return TypeStylesheet
	}
	if strings.HasPrefix(contentType, "video/") {
		return TypeVideo
	}

	return TypeOther
}
