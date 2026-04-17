package policy

import (
	"fmt"
	"strings"

	"machring/internal/resource"
)

type Resolver struct {
	rules []Rule
}

func NewResolver(rules []Rule) *Resolver {
	return &Resolver{rules: rules}
}

func (r *Resolver) Resolve(action Action, group string, meta resource.Metadata) Decision {
	if group == "" {
		group = GroupGuest
	}

	rule, ok := r.find(group, meta)
	if !ok {
		return Decision{
			Allowed: false,
			Reason:  fmt.Sprintf("no policy rule matched group %q and resource type %q", group, meta.Type),
		}
	}

	switch action {
	case ActionUpload:
		if !rule.AllowUpload {
			return Decision{Allowed: false, Reason: "upload denied by policy", Rule: rule}
		}
		if rule.RequireAuth && group == GroupGuest {
			return Decision{Allowed: false, Reason: "login required by policy", Rule: rule}
		}
		if rule.MaxFileSizeBytes > 0 && meta.Size > 0 && meta.Size > rule.MaxFileSizeBytes {
			return Decision{
				Allowed: false,
				Reason:  fmt.Sprintf("file size exceeds policy limit of %d bytes", rule.MaxFileSizeBytes),
				Rule:    rule,
			}
		}
	case ActionAccess:
		if !rule.AllowAccess {
			return Decision{Allowed: false, Reason: "access denied by policy", Rule: rule}
		}
	default:
		return Decision{Allowed: false, Reason: fmt.Sprintf("unsupported policy action %q", action), Rule: rule}
	}

	return Decision{Allowed: true, Reason: "allowed by policy", Rule: rule}
}

func (r *Resolver) Rules() []Rule {
	return append([]Rule(nil), r.rules...)
}

func (r *Resolver) find(group string, meta resource.Metadata) (Rule, bool) {
	var typeMatch *Rule
	for i := range r.rules {
		rule := &r.rules[i]
		if rule.UserGroup != group {
			continue
		}
		if rule.Extension != "" && strings.EqualFold(rule.Extension, meta.Extension) {
			return *rule, true
		}
		if rule.Extension == "" && rule.ResourceType == meta.Type {
			typeMatch = rule
		}
	}
	if typeMatch != nil {
		return *typeMatch, true
	}
	if group != GroupGuest {
		return r.find(GroupGuest, meta)
	}
	return Rule{}, false
}
