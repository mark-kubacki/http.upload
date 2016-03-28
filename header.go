package upload // import "blitznote.com/src/caddy.upload"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"text/scanner"
	"time"

	. "plugin.hosting/go/abs"
)

const (
	ErrStrAuthorizationChallengeNotSupported = "authorization challenge not supported"
	ErrStrUnexpectedPrefix                   = "unexpected token at position: "
	ErrStrUnexpectedValuePrefix              = "unexpected value (not in quotes?) at position: "
)

var (
	ErrAuthorizationNotSupported = errors.New(ErrStrAuthorizationChallengeNotSupported)
)

type AuthorizationHeader struct {
	KeyId         string
	Algorithm     string // only hmac-sha256 is currently recognized
	HeadersToSign []string
	Extensions    []string // not used here
	Signature     []byte
}

func (h *AuthorizationHeader) Parse(str string) (err error) {
	*h, err = parseAuthorizationHeader(str)
	return
}

func parseAuthorizationHeader(src string) (AuthorizationHeader, error) {
	var (
		a AuthorizationHeader
		s scanner.Scanner
	)

	s.Init(strings.NewReader(src))
	tok := s.Scan()
	if tok == scanner.EOF || s.TokenText() != "Signature" {
		return a, ErrAuthorizationNotSupported
	}

	for tok != scanner.EOF {
		tok = s.Scan()
		if tok != scanner.Ident {
			return a, errors.New(ErrStrUnexpectedPrefix + s.Pos().String())
		}
		ident := strings.ToLower(s.TokenText())

		tok = s.Scan()
		if !(tok == 61 || tok == 58) { // = or :
			return a, errors.New(ErrStrUnexpectedPrefix + s.Pos().String())
		}

		tok = s.Scan()
		if tok != scanner.String {
			return a, errors.New(ErrStrUnexpectedPrefix + s.Pos().String())
		}

		v, err := strconv.Unquote(s.TokenText())
		if err != nil {
			return a, errors.New(ErrStrUnexpectedValuePrefix + s.Pos().String())
		}

		switch ident {
		case "keyid":
			a.KeyId = v
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
				return a, err
			}
			a.Signature = sig
		}

		tok = s.Scan()
	}

	return a, nil
}

// Returns true if all listed headers are present and timestamp(s) are within the given tolerance (if given).
func (a *AuthorizationHeader) CheckFormal(headers http.Header, timestampNow, timeTolerance uint64) bool {
	for idx, _ := range a.HeadersToSign {
		v := headers.Get(a.HeadersToSign[idx])
		if v == "" {
			return false
		}
		if a.HeadersToSign[idx] == "timestamp" || a.HeadersToSign[idx] == "date" {
			var timestampThen uint64
			if a.HeadersToSign[idx] == "timestamp" {
				timestampThen, _ = strconv.ParseUint(v, 10, 64)
			} else {
				t, err := time.Parse(http.TimeFormat, v)
				if err != nil {
					return false
				}
				timestampThen = uint64(t.Unix())
			}

			if Abs64(int64(timestampNow-timestampThen)) > timeTolerance {
				return false
			}
		}
	}

	return true
}

// Checks if the headers match the signature.
func (a *AuthorizationHeader) SatisfiedBy(headers http.Header, secret []byte) bool {
	mac := hmac.New(sha256.New, secret)
	for idx, _ := range a.HeadersToSign {
		mac.Write([]byte(headers.Get(a.HeadersToSign[idx])))
	}
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(a.Signature, expectedMAC)
}
