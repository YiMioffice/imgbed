package policy

import "machring/internal/resource"

const (
	MB = 1024 * 1024
	GB = 1024 * MB

	DefaultGroupID   = "default"
	DefaultGroupName = "默认策略组"
)

func DefaultRules() []Rule {
	return []Rule{
		{
			UserGroup:                      GroupGuest,
			ResourceType:                   resource.TypeImage,
			AllowUpload:                    true,
			AllowAccess:                    true,
			MaxFileSizeBytes:               10 * MB,
			MonthlyTrafficPerResourceBytes: 1 * GB,
			CacheControl:                   "public, max-age=31536000, immutable",
		},
		{
			UserGroup:                      GroupUser,
			ResourceType:                   resource.TypeImage,
			AllowUpload:                    true,
			AllowAccess:                    true,
			MaxFileSizeBytes:               50 * MB,
			MonthlyTrafficPerResourceBytes: 10 * GB,
			CacheControl:                   "public, max-age=31536000, immutable",
		},
		{
			UserGroup:                      GroupGuest,
			ResourceType:                   resource.TypeArchive,
			AllowUpload:                    false,
			AllowAccess:                    true,
			MonthlyTrafficPerResourceBytes: 1 * GB,
			DownloadDisposition:            "attachment",
		},
		{
			UserGroup:                      GroupUser,
			ResourceType:                   resource.TypeArchive,
			AllowUpload:                    true,
			AllowAccess:                    true,
			MaxFileSizeBytes:               200 * MB,
			MonthlyTrafficPerResourceBytes: 5 * GB,
			DownloadDisposition:            "attachment",
		},
		{
			UserGroup:           GroupUser,
			ResourceType:        resource.TypeExecutable,
			AllowUpload:         false,
			AllowAccess:         false,
			DownloadDisposition: "attachment",
		},
		{
			UserGroup:                      GroupAdmin,
			ResourceType:                   resource.TypeExecutable,
			AllowUpload:                    true,
			AllowAccess:                    true,
			MaxFileSizeBytes:               500 * MB,
			MonthlyTrafficPerResourceBytes: 20 * GB,
			DownloadDisposition:            "attachment",
		},
		{
			UserGroup:                      GroupAdmin,
			ResourceType:                   resource.TypeOther,
			AllowUpload:                    true,
			AllowAccess:                    true,
			MaxFileSizeBytes:               500 * MB,
			MonthlyTrafficPerResourceBytes: 20 * GB,
		},
	}
}
