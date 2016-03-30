package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"net/http"
	"time"
)

var (
	ErrAuthAlgorithm         = errors.New("Authorization: unsupported 'algorithm'")
	ErrAuthHeaderFieldPrefix = errors.New("Authorization: mismatch in prefix of 'headers'")
	ErrAuthHeadersLacking    = errors.New("Authorization: not all expected headers had been set correctly")
	ErrMethodUnauthorized    = errors.New("Method Not Authorized")
)

// Results in a syscall issued by 'runtime'.
func getTimestampUsingTime() uint64 {
	t := time.Now()
	return uint64(t.Unix())
}

// Seconds since 1970-01-01 00:00:00Z.
//
// Will be overwritten by another in-house package.
var getTimestamp = getTimestampUsingTime

// Validates and verifies the authorization header.
func (h *Handler) authenticate(r *http.Request) (httpResponseCode int, err error) {
	httpResponseCode = 200 // 200: ok/pass

	h.Config.IncomingHmacSecretsLock.RLock()
	if len(h.Config.IncomingHmacSecrets) == 0 {
		h.Config.IncomingHmacSecretsLock.RUnlock()
		return
	}
	h.Config.IncomingHmacSecretsLock.RUnlock()

	var a AuthorizationHeader
	a.Algorithm = "hmac-sha256"
	a.HeadersToSign = []string{"timestamp", "token"}

	err = a.Parse(r.Header.Get("Authorization"))
	switch err {
	case ErrAuthorizationNotSupported: // or the header is empty/not set
		return http.StatusUnauthorized, err
	case nil:
		break
	default:
		return http.StatusBadRequest, err
	}

	if len(a.Signature) == 0 || len(a.HeadersToSign) < 2 ||
		a.Algorithm != "hmac-sha256" {
		return http.StatusBadRequest, ErrAuthAlgorithm
	}
	if !(a.HeadersToSign[0] == "date" || a.HeadersToSign[0] == "timestamp") ||
		a.HeadersToSign[1] != "token" {
		return http.StatusBadRequest, ErrAuthHeaderFieldPrefix
	}

	if !a.CheckFormal(r.Header, getTimestamp(), h.Config.TimestampTolerance) {
		return http.StatusBadRequest, ErrAuthHeadersLacking
	}

	h.Config.IncomingHmacSecretsLock.RLock()
	hmacSharedSecret, secretFound := h.Config.IncomingHmacSecrets[a.KeyID]
	h.Config.IncomingHmacSecretsLock.RUnlock()

	// do this anyway to obscure if the keyId exists
	isSatisfied := a.SatisfiedBy(r.Header, hmacSharedSecret)

	if !secretFound || !isSatisfied {
		return http.StatusForbidden, ErrMethodUnauthorized // 403: forbidden
	}
	return
}
