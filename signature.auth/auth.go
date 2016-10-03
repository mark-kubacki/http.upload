package auth // import "hub.blitznote.com/src/caddy.upload/signature.auth"

import (
	"encoding/base64"
	"net/http"
	"strings"
)

// Errors thrown by the implementation of the Authorization: Signature scheme.
const (
	errAuthAlgorithm         unauthorizedError = "unsupported 'algorithm'"
	errAuthHeaderFieldPrefix unauthorizedError = "mismatch in prefix of 'headers'"
	errAuthHeadersLacking    badRequestError   = "not all expected headers had been set correctly"
	errMethodUnauthorized    forbiddenError    = "Method not authorized"
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
func (m HmacSecrets) Insert(tuples []string) error {
	for idx := range tuples {
		p := strings.SplitN(tuples[idx], "=", 2)
		if len(p) != 2 {
			return badRequestError(tuples[idx])
		}
		binary, err := base64.StdEncoding.DecodeString(p[1])
		if err != nil {
			return badRequestError(tuples[idx])
		}
		m[p[0]] = binary
	}

	return nil
}

// Authenticate implements authorization scheme Signature:
// Knowledge of a shared secret is expressed by providing its "signature".
//
// 'timestampRecv' is the Unix Timestamp at the time when the request has been received.
func Authenticate(headers http.Header, secrets HmacSecrets, timestampRecv, timeTolerance uint64) AuthError {
	if len(secrets) == 0 {
		return errMethodUnauthorized
	}

	var a AuthorizationHeader
	a.Algorithm = "hmac-sha256"
	a.HeadersToSign = []string{"timestamp", "token"}

	if err := a.Parse(headers.Get("Authorization")); err != nil {
		return err
	}

	if len(a.Signature) == 0 || len(a.HeadersToSign) < 2 {
		return errAuthHeadersLacking
	}
	if a.Algorithm != "hmac-sha256" {
		return errAuthAlgorithm
	}
	if !(a.HeadersToSign[0] == "date" || a.HeadersToSign[0] == "timestamp") ||
		a.HeadersToSign[1] != "token" {
		return errAuthHeaderFieldPrefix
	}

	if err := a.CheckFormal(headers, timestampRecv, timeTolerance); err != nil {
		return err
	}

	hmacSharedSecret, secretFound := secrets[a.KeyID]

	// do this anyway to obscure if the keyId exists
	isSatisfied := a.SatisfiedBy(headers, hmacSharedSecret)

	if !secretFound || !isSatisfied {
		return errMethodUnauthorized
	}
	return nil
}
