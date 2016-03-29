package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"net/http"
	"time"
)

// Results in a syscall issued by 'runtime'.
func getTimestampUsingTime() uint64 {
	t := time.Now()
	return uint64(t.Unix())
}

// Seconds since 1970-01-01 00:00:00Z.
//
// Will be overwritten by another in-house package.
var getTimestamp func() uint64 = getTimestampUsingTime

// Validates and verifies the authorization header.
func (h *UploadHandler) authenticate(r *http.Request) (httpResponseCode int, err error) {
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
		return 401, err
	case nil:
		break
	default:
		return 400, err
	}

	if len(a.Signature) == 0 || len(a.HeadersToSign) < 2 ||
		a.Algorithm != "hmac-sha256" {
		return 400, errors.New("Authorization: unsupported 'algorithm'")
	}
	if !(a.HeadersToSign[0] == "date" || a.HeadersToSign[0] == "timestamp") ||
		a.HeadersToSign[1] != "token" {
		return 400, errors.New("Authorization: mismatch in prefix of 'headers'")
	}

	if !a.CheckFormal(r.Header, getTimestamp(), h.Config.TimestampTolerance) {
		return 400, errors.New("Authorization: not all expected headers had been set correctly")
	}

	h.Config.IncomingHmacSecretsLock.RLock()
	hmacSharedSecret, secretNotFound := h.Config.IncomingHmacSecrets[a.KeyId]
	h.Config.IncomingHmacSecretsLock.RUnlock()

	// do this anyway to not reveal if the keyId exists
	isSatisfied := a.SatisfiedBy(r.Header, hmacSharedSecret)

	if !secretNotFound || !isSatisfied {
		return 403, errors.New("Method Not Authorized") // 403: forbidden
	}
	return
}
