package policy

import (
	"testing"

	"machring/internal/resource"
)

func TestResolverDefaultRules(t *testing.T) {
	resolver := NewResolver(DefaultRules())

	tests := []struct {
		name    string
		group   string
		meta    resource.Metadata
		allowed bool
		traffic int64
	}{
		{
			name:    "guest jpg has 1GB monthly resource traffic",
			group:   GroupGuest,
			meta:    resource.Metadata{Extension: "jpg", Type: resource.TypeImage, Size: 1 * MB},
			allowed: true,
			traffic: 1 * GB,
		},
		{
			name:    "logged in jpg has 10GB monthly resource traffic",
			group:   GroupUser,
			meta:    resource.Metadata{Extension: "jpg", Type: resource.TypeImage, Size: 1 * MB},
			allowed: true,
			traffic: 10 * GB,
		},
		{
			name:    "guest zip upload is denied",
			group:   GroupGuest,
			meta:    resource.Metadata{Extension: "zip", Type: resource.TypeArchive, Size: 1 * MB},
			allowed: false,
		},
		{
			name:    "logged in zip upload is allowed",
			group:   GroupUser,
			meta:    resource.Metadata{Extension: "zip", Type: resource.TypeArchive, Size: 1 * MB},
			allowed: true,
			traffic: 5 * GB,
		},
		{
			name:    "regular user exe upload is denied",
			group:   GroupUser,
			meta:    resource.Metadata{Extension: "exe", Type: resource.TypeExecutable, Size: 1 * MB},
			allowed: false,
		},
		{
			name:    "admin exe upload is allowed",
			group:   GroupAdmin,
			meta:    resource.Metadata{Extension: "exe", Type: resource.TypeExecutable, Size: 1 * MB},
			allowed: true,
			traffic: 20 * GB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := resolver.Resolve(ActionUpload, tt.group, tt.meta)
			if decision.Allowed != tt.allowed {
				t.Fatalf("allowed = %v, want %v; reason: %s", decision.Allowed, tt.allowed, decision.Reason)
			}
			if tt.traffic > 0 && decision.Rule.MonthlyTrafficPerResourceBytes != tt.traffic {
				t.Fatalf("traffic = %d, want %d", decision.Rule.MonthlyTrafficPerResourceBytes, tt.traffic)
			}
		})
	}
}

func TestValidateRules(t *testing.T) {
	_, errors := ValidateRules([]Rule{
		{
			UserGroup:        "bad-group",
			ResourceType:     resource.Type("bad-type"),
			Extension:        ".JPG",
			MaxFileSizeBytes: -1,
		},
		{
			UserGroup:    GroupGuest,
			ResourceType: resource.TypeImage,
		},
		{
			UserGroup:    GroupGuest,
			ResourceType: resource.TypeImage,
		},
	})

	if len(errors) == 0 {
		t.Fatal("expected validation errors")
	}
}
