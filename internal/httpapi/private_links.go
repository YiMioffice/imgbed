package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"machring/internal/auth"
	"machring/internal/resource"
)

var errSignedLinkExpired = errors.New("signed link expired")
var errSignedLinkInvalid = errors.New("signed link is invalid")

func canAccessPrivateResource(record resource.Record, viewer auth.User, hasViewer bool) bool {
	if !hasViewer {
		return false
	}
	if viewer.Role == auth.AdminRole {
		return true
	}
	return record.OwnerUserID != "" && viewer.ID == record.OwnerUserID
}

func (api *API) signedResourceURL(ctx context.Context, record resource.Record, expiresAt time.Time) (string, error) {
	expiryUnix := expiresAt.UTC().Unix()
	signature, err := api.resourceSignature(ctx, record.ID, expiryUnix)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(record.PublicURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("exp", strconv.FormatInt(expiryUnix, 10))
	query.Set("sig", signature)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (api *API) isValidSignedResourceRequest(ctx context.Context, r *http.Request, record resource.Record) (bool, error) {
	expRaw := strings.TrimSpace(r.URL.Query().Get("exp"))
	sigRaw := strings.TrimSpace(r.URL.Query().Get("sig"))
	if expRaw == "" && sigRaw == "" {
		return false, nil
	}
	expiryUnix, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		return false, errSignedLinkInvalid
	}
	if time.Now().UTC().After(time.Unix(expiryUnix, 0).UTC()) {
		return false, errSignedLinkExpired
	}
	signature, err := api.resourceSignature(ctx, record.ID, expiryUnix)
	if err != nil {
		return false, err
	}
	decodedExpected, err := hex.DecodeString(signature)
	if err != nil {
		return false, errSignedLinkInvalid
	}
	decodedProvided, err := hex.DecodeString(sigRaw)
	if err != nil {
		return false, errSignedLinkInvalid
	}
	if !hmac.Equal(decodedExpected, decodedProvided) {
		return false, errSignedLinkInvalid
	}
	return true, nil
}

func (api *API) resourceSignature(ctx context.Context, resourceID string, expiryUnix int64) (string, error) {
	secret, err := api.app.Data.SigningSecret(ctx)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s\n%d", resourceID, expiryUnix)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}
