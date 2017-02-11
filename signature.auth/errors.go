// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"net/http"
)

// AuthError adds a behavioural hint to an Error.
type AuthError interface {
	error

	// SuggestedResponseCode gives a HTTP status code.
	SuggestedResponseCode() int
}

// badRequestError is returned on formal errors.
type badRequestError string

// Error implements the error interface.
func (e badRequestError) Error() string { return string(e) }

// SuggestedResponseCode implements the AuthError interface.
func (e badRequestError) SuggestedResponseCode() int { return http.StatusBadRequest }

// unauthorizedError is given when the credentials have not been found in a database.
//
// The client should try again using different credentials.
type unauthorizedError string

// Error implements the error interface.
func (e unauthorizedError) Error() string { return string(e) }

// SuggestedResponseCode implements the AuthError interface.
func (e unauthorizedError) SuggestedResponseCode() int { return http.StatusUnauthorized }

// forbiddenError is return when the credentials have been found, but don't grant the necessary rights.
//
// The client should not try again.
type forbiddenError string

// Error implements the error interface.
func (e forbiddenError) Error() string { return string(e) }

// SuggestedResponseCode implements the AuthError interface.
func (e forbiddenError) SuggestedResponseCode() int { return http.StatusForbidden }
