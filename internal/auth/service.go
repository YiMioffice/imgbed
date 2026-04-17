package auth

import (
	"crypto/rand"
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const AdminRole = "admin"

var ErrUserNotFound = errors.New("user not found")

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	GroupID     string `json:"groupId"`
	GroupName   string `json:"groupName"`
	Status      string `json:"status"`
}

type Session struct {
	Token     string
	User      User
	ExpiresAt time.Time
}

type CredentialStore interface {
	UserByUsername(ctx context.Context, username string) (User, string, error)
	UserByID(ctx context.Context, id string) (User, error)
}

type Service struct {
	store      CredentialStore
	sessionTTL time.Duration

	mu       sync.RWMutex
	sessions map[string]Session
}

func NewService(store CredentialStore, sessionTTL time.Duration) *Service {
	return &Service{
		store:      store,
		sessionTTL: sessionTTL,
		sessions:   make(map[string]Session),
	}
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, bool, error) {
	user, passwordHash, err := s.store.UserByUsername(ctx, username)
	if errors.Is(err, ErrUserNotFound) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	if user.Status != "" && user.Status != "active" {
		return Session{}, false, nil
	}
	if !VerifyPassword(password, passwordHash) {
		return Session{}, false, nil
	}

	token, err := newToken()
	if err != nil {
		return Session{}, false, err
	}

	session := Session{
		Token: token,
		User:  user,
		ExpiresAt: time.Now().Add(s.sessionTTL),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session
	return session, true, nil
}

func (s *Service) UserForToken(token string) (User, bool) {
	if token == "" {
		return User{}, false
	}

	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return User{}, false
	}
	if time.Now().After(session.ExpiresAt) {
		s.Logout(token)
		return User{}, false
	}

	user, err := s.store.UserByID(context.Background(), session.User.ID)
	if errors.Is(err, ErrUserNotFound) {
		s.Logout(token)
		return User{}, false
	}
	if err != nil {
		return session.User, true
	}
	if user.Status != "" && user.Status != "active" {
		s.Logout(token)
		return User{}, false
	}

	s.mu.Lock()
	session.User = user
	s.sessions[token] = session
	s.mu.Unlock()

	return user, true
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *Service) RequireAdmin(token string) (User, error) {
	user, ok := s.UserForToken(token)
	if !ok {
		return User{}, errors.New("authentication required")
	}
	if user.Role != AdminRole {
		return User{}, errors.New("admin role required")
	}
	return user, nil
}

func newToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
