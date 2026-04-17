package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrPolicyGroupNotFound = errors.New("policy group not found")
var ErrPolicyGroupInUse = errors.New("policy group in use")
var ErrPolicyGroupInvalidState = errors.New("invalid policy group state")

type Store interface {
	Rules(ctx context.Context) ([]Rule, error)
	ReplaceRules(ctx context.Context, rules []Rule) error
	ReplaceRulesForGroup(ctx context.Context, groupID string, rules []Rule) error
	ActivePolicyGroup(ctx context.Context) (Group, error)
	PolicyGroups(ctx context.Context) ([]Group, error)
	RulesForGroup(ctx context.Context, groupID string) ([]Rule, error)
	PolicyGroup(ctx context.Context, groupID string) (Group, []Rule, error)
	CreatePolicyGroup(ctx context.Context, name, description string) (Group, error)
	UpdatePolicyGroup(ctx context.Context, groupID, name, description string) (Group, error)
	DeletePolicyGroup(ctx context.Context, groupID string) error
	CopyPolicyGroup(ctx context.Context, sourceGroupID, name string) (Group, error)
	SetPolicyGroupActive(ctx context.Context, groupID string, active bool) (Group, error)
}

type MemoryStore struct {
	mu          sync.RWMutex
	groups      []Group
	rulesByID   map[string][]Rule
	activeGroup string
}

func NewMemoryStore(rules []Rule) *MemoryStore {
	now := time.Now()
	return &MemoryStore{
		groups: []Group{
			{
				ID:        DefaultGroupID,
				Name:      DefaultGroupName,
				IsActive:  true,
				IsDefault: true,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		rulesByID: map[string][]Rule{
			DefaultGroupID: append([]Rule(nil), rules...),
		},
		activeGroup: DefaultGroupID,
	}
}

func (s *MemoryStore) Rules(_ context.Context) ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]Rule(nil), s.rulesByID[s.activeGroup]...), nil
}

func (s *MemoryStore) ReplaceRules(_ context.Context, rules []Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rulesByID[s.activeGroup] = append([]Rule(nil), rules...)
	return nil
}

func (s *MemoryStore) ReplaceRulesForGroup(_ context.Context, groupID string, rules []Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rulesByID[groupID]; !ok {
		return ErrPolicyGroupNotFound
	}
	s.rulesByID[groupID] = append([]Rule(nil), rules...)
	return nil
}

func (s *MemoryStore) ActivePolicyGroup(_ context.Context) (Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, group := range s.groups {
		if group.ID == s.activeGroup {
			return group, nil
		}
	}
	return Group{}, ErrPolicyGroupNotFound
}

func (s *MemoryStore) PolicyGroups(_ context.Context) ([]Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Group(nil), s.groups...), nil
}

func (s *MemoryStore) RulesForGroup(_ context.Context, groupID string) ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Rule(nil), s.rulesByID[groupID]...), nil
}

func (s *MemoryStore) PolicyGroup(_ context.Context, groupID string) (Group, []Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, group := range s.groups {
		if group.ID == groupID {
			return group, append([]Rule(nil), s.rulesByID[groupID]...), nil
		}
	}
	return Group{}, nil, ErrPolicyGroupNotFound
}

func (s *MemoryStore) CreatePolicyGroup(_ context.Context, name, description string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	group := Group{
		ID:          fmt.Sprintf("group-%d", len(s.groups)+1),
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.groups = append(s.groups, group)
	s.rulesByID[group.ID] = nil
	return group, nil
}

func (s *MemoryStore) UpdatePolicyGroup(_ context.Context, groupID, name, description string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == groupID {
			s.groups[i].Name = name
			s.groups[i].Description = description
			s.groups[i].UpdatedAt = time.Now()
			return s.groups[i], nil
		}
	}
	return Group{}, ErrPolicyGroupNotFound
}

func (s *MemoryStore) DeletePolicyGroup(_ context.Context, groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == groupID {
			if s.groups[i].IsDefault {
				return ErrPolicyGroupInUse
			}
			s.groups = append(s.groups[:i], s.groups[i+1:]...)
			delete(s.rulesByID, groupID)
			if s.activeGroup == groupID {
				s.activeGroup = DefaultGroupID
			}
			return nil
		}
	}
	return ErrPolicyGroupNotFound
}

func (s *MemoryStore) CopyPolicyGroup(_ context.Context, sourceGroupID, name string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rulesByID[sourceGroupID]; !ok {
		return Group{}, ErrPolicyGroupNotFound
	}
	sourceRules := append([]Rule(nil), s.rulesByID[sourceGroupID]...)
	now := time.Now()
	group := Group{
		ID:        fmt.Sprintf("group-%d", len(s.groups)+1),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.groups = append(s.groups, group)
	s.rulesByID[group.ID] = sourceRules
	return group, nil
}

func (s *MemoryStore) SetPolicyGroupActive(_ context.Context, groupID string, active bool) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == groupID {
			if active {
				for j := range s.groups {
					s.groups[j].IsActive = false
				}
				s.groups[i].IsActive = true
				s.activeGroup = groupID
			} else {
				if s.groups[i].IsActive {
					return Group{}, ErrPolicyGroupInvalidState
				}
				s.groups[i].IsActive = false
			}
			s.groups[i].UpdatedAt = time.Now()
			return s.groups[i], nil
		}
	}
	return Group{}, ErrPolicyGroupNotFound
}
