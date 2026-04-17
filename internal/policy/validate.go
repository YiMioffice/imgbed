package policy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"machring/internal/resource"
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

var extensionPattern = regexp.MustCompile(`^[a-z0-9]+$`)

func ValidGroups() []string {
	return []string{GroupGuest, GroupUser, GroupAdmin}
}

func ValidDownloadDispositions() []string {
	return []string{"", "inline", "attachment"}
}

func NormalizeRule(rule Rule) Rule {
	rule.UserGroup = strings.TrimSpace(rule.UserGroup)
	rule.Extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(rule.Extension)), ".")
	rule.CacheControl = strings.TrimSpace(rule.CacheControl)
	rule.DownloadDisposition = strings.TrimSpace(rule.DownloadDisposition)
	return rule
}

func ValidateRules(rules []Rule) ([]Rule, []ValidationError) {
	normalized := make([]Rule, len(rules))
	errors := make([]ValidationError, 0)
	seen := make(map[string]int)

	for i, rule := range rules {
		rule = NormalizeRule(rule)
		normalized[i] = rule
		prefix := fmt.Sprintf("rules[%d]", i)

		if !slices.Contains(ValidGroups(), rule.UserGroup) {
			errors = append(errors, ValidationError{Field: prefix + ".userGroup", Message: "invalid user group"})
		}
		if !slices.Contains(resource.AllTypes(), rule.ResourceType) {
			errors = append(errors, ValidationError{Field: prefix + ".resourceType", Message: "invalid resource type"})
		}
		if rule.Extension != "" && !extensionPattern.MatchString(rule.Extension) {
			errors = append(errors, ValidationError{Field: prefix + ".extension", Message: "extension must contain only lowercase letters and digits"})
		}
		if !slices.Contains(ValidDownloadDispositions(), rule.DownloadDisposition) {
			errors = append(errors, ValidationError{Field: prefix + ".downloadDisposition", Message: "download disposition must be inline or attachment"})
		}
		if rule.MaxFileSizeBytes < 0 {
			errors = append(errors, ValidationError{Field: prefix + ".maxFileSizeBytes", Message: "value must be greater than or equal to 0"})
		}
		if rule.MonthlyTrafficPerResourceBytes < 0 {
			errors = append(errors, ValidationError{Field: prefix + ".monthlyTrafficPerResourceBytes", Message: "value must be greater than or equal to 0"})
		}
		if rule.MonthlyTrafficPerUserAndTypeBytes < 0 {
			errors = append(errors, ValidationError{Field: prefix + ".monthlyTrafficPerUserAndTypeBytes", Message: "value must be greater than or equal to 0"})
		}

		key := rule.UserGroup + "|" + string(rule.ResourceType) + "|" + rule.Extension
		if prev, ok := seen[key]; ok {
			errors = append(errors, ValidationError{Field: prefix, Message: fmt.Sprintf("duplicate rule, same key as rules[%d]", prev)})
		} else {
			seen[key] = i
		}
	}

	return normalized, errors
}
