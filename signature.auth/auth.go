package auth // import "blitznote.com/src/caddy.upload/signature.auth"

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// Errors thrown by the implementation of the Authorization: Signature scheme.
var (
	errAuthAlgorithm         = errors.New("Authorization: unsupported 'algorithm'")
	errAuthHeaderFieldPrefix = errors.New("Authorization: mismatch in prefix of 'headers'")
	errAuthHeadersLacking    = errors.New("Authorization: not all expected headers had been set correctly")
	errMethodUnauthorized    = errors.New("Method not authorized")
)

// HmacSecrets maps keyIDs to shared secrets.
type HmacSecrets map[string][]byte

// Insert decodes the key/value pairs
// and adds/updates them into the existing HMAC shared secret collection.
//
// The format of each pair is:
//  key=base64(value)
//
// For example:
//  hmac-key-1=yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo=
//
// The first tuple that cannot be decoded is returned as error string.
func (m HmacSecrets) Insert(tuples []string) (err error) {
	for idx := range tuples {
		p := strings.SplitN(tuples[idx], "=", 2)
		if len(p) != 2 {
			return errors.New(tuples[idx])
		}
		binary, err := base64.StdEncoding.DecodeString(p[1])
		if err != nil {
			return errors.New(tuples[idx])
		}
		m[p[0]] = binary
	}

	return
}

// Authenticate implements authorization scheme Signature:
// Knowledge of a shared secret is expressed by providing its "signature".
//
// 'timestampRecv' is the Unix Timestamp at the time when the request has been received.
func Authenticate(headers http.Header, secrets HmacSecrets, timestampRecv, timeTolerance uint64) (httpResponseCode int, err error) {
	if len(secrets) == 0 {
		return http.StatusForbidden, errMethodUnauthorized
	}

	var a AuthorizationHeader
	a.Algorithm = "hmac-sha256"
	a.HeadersToSign = []string{"timestamp", "token"}

	err = a.Parse(headers.Get("Authorization"))
	switch err {
	case errAuthorizationNotSupported: // or the header is empty/not set
		return http.StatusUnauthorized, err
	case nil:
		break
	default:
		return http.StatusBadRequest, err
	}

	if len(a.Signature) == 0 || len(a.HeadersToSign) < 2 ||
		a.Algorithm != "hmac-sha256" {
		return http.StatusBadRequest, errAuthAlgorithm
	}
	if !(a.HeadersToSign[0] == "date" || a.HeadersToSign[0] == "timestamp") ||
		a.HeadersToSign[1] != "token" {
		return http.StatusBadRequest, errAuthHeaderFieldPrefix
	}

	if !a.CheckFormal(headers, timestampRecv, timeTolerance) {
		return http.StatusBadRequest, errAuthHeadersLacking
	}

	hmacSharedSecret, secretFound := secrets[a.KeyID]

	// do this anyway to obscure if the keyId exists
	isSatisfied := a.SatisfiedBy(headers, hmacSharedSecret)

	if !secretFound || !isSatisfied {
		return http.StatusForbidden, errMethodUnauthorized
	}
	return http.StatusOK, nil
}
