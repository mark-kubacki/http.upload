// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth // import "blitznote.com/src/caddy.upload/signature.auth"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"text/scanner"
	"time"
)

// Used in errors that are returned when parsing a malformed "Authorization" header.
const (
	errStrUnexpectedPrefix       badRequestError   = "Unexpected token at position: "
	errStrUnexpectedValuePrefix  badRequestError   = "Unexpected value (not in quotes?) at position: "
	errAuthorizationNotSupported unauthorizedError = "Authorization challenge not supported"
	errHeaderIsMissing           badRequestError   = "Header is missing: "
	errRequestTooOld             forbiddenError    = "The request is too old, it's time is outside tolerance"
)

// AuthorizationHeader represents a HTTP header which is used in
// authentication scheme "Signature".
type AuthorizationHeader struct {
	KeyID         string
	Algorithm     string // only hmac-sha256 is currently recognized
	HeadersToSign []string
	Extensions    []string // not used here
	Signature     []byte
}

// Parse translates a string representation to this struct.
//
// Use this to deserialize the result of http.Header.Get(…).
func (a *AuthorizationHeader) Parse(str string) (err AuthError) {
	*a, err = parseAuthorizationHeader(str, *a)
	return
}

func parseAuthorizationHeader(src string, a AuthorizationHeader) (AuthorizationHeader, AuthError) {
	var s scanner.Scanner

	s.Init(strings.NewReader(src))
	tok := s.Scan()
	if tok == scanner.EOF || s.TokenText() != "Signature" {
		return a, errAuthorizationNotSupported
	}

	for tok != scanner.EOF {
		tok = s.Scan()
		if tok != scanner.Ident {
			return a, badRequestError(errStrUnexpectedPrefix.Error() + s.Pos().String())
		}
		ident := strings.ToLower(s.TokenText())

		tok = s.Scan()
		if !(tok == 61 || tok == 58) { // = or :
			return a, badRequestError(errStrUnexpectedPrefix.Error() + s.Pos().String())
		}

		tok = s.Scan()
		if tok != scanner.String {
			return a, badRequestError(errStrUnexpectedPrefix.Error() + s.Pos().String())
		}

		v, err := strconv.Unquote(s.TokenText())
		if err != nil {
			return a, badRequestError(errStrUnexpectedValuePrefix.Error() + s.Pos().String())
		}

		switch ident {
		case "keyid":
			a.KeyID = v
		case "algorithm":
			a.Algorithm = v
		case "extensions":
			if v != "" {
				a.Extensions = strings.Split(v, " ")
			}
		case "headers":
			if v != "" {
				a.HeadersToSign = strings.Split(v, " ")
			}
		case "signature":
			sig, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return a, badRequestError(err.Error())
			}
			a.Signature = sig
		}

		tok = s.Scan()
	}

	return a, nil
}

// CheckFormal returns true if all listed headers are present
// and timestamp(s) (if provided) are within a tolerance.
func (a *AuthorizationHeader) CheckFormal(headers http.Header, timestampRecv, timeTolerance uint64) AuthError {
	for idx := range a.HeadersToSign {
		k := a.HeadersToSign[idx]
		v := headers.Get(k)
		switch {
		case v == "":
			return badRequestError(errHeaderIsMissing.Error() + k)
		case k == "timestamp" || k == "date":
			var timestampThen uint64
			if k == "timestamp" {
				timestampThen, _ = strconv.ParseUint(v, 10, 64)
			} else {
				t, err := time.Parse(http.TimeFormat, v)
				if err != nil {
					return badRequestError(err.Error())
				}
				timestampThen = uint64(t.Unix())
			}

			if abs64(int64(timestampRecv-timestampThen)) > timeTolerance {
				return errRequestTooOld
			}
		}
	}

	return nil
}

// SatisfiedBy tests if the headers and shared secret result in the same signature as given in the header.
//
// As this is a rather costly function, call 'CheckFormal' first to avoid 'SatisfiedBy' where possible.
func (a *AuthorizationHeader) SatisfiedBy(headers http.Header, secret []byte) bool {
	mac := hmac.New(sha256.New, secret)
	for idx := range a.HeadersToSign {
		mac.Write([]byte(headers.Get(a.HeadersToSign[idx])))
	}
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(a.Signature, expectedMAC)
}
