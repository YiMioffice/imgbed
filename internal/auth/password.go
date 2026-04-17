package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	passwordHashScheme     = "pbkdf2_sha256"
	passwordHashIterations = 600000
	passwordHashSaltBytes  = 16
	passwordHashKeyBytes   = 32
)

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password is required")
	}

	salt := make([]byte, passwordHashSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, passwordHashIterations, passwordHashKeyBytes)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordHashScheme,
		passwordHashIterations,
		hex.EncodeToString(salt),
		hex.EncodeToString(derived),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordHashScheme {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}

	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}

	expected, err := hex.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return false
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(derived, expected) == 1
}
