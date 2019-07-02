// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !caddyserver0.9

package upload // import "blitznote.com/src/http.upload"

import (
	"net/http"
)

// NewHandler creates a new instance of this plugin's upload handler,
// meant to be used in Go's own http server.
//
// XXX: Its responsibility is to reject invalid or formally incorrect configurations.
//
// 'next' is optional.
// 'scope' is a string and the prefix of the upload destination's URL.Path, like `/dir/to/upload/destination`.
func NewHandler(scope string, config *ScopeConfiguration, next http.Handler) (*Handler, error) {
	h := Handler{
		Next:   next,
		Config: config,
		Scope:  scope,
	}

	if next == nil {
		h.Next = http.NotFoundHandler()
	}

	return &h, nil
}

// Handler implements http.Handler.
type Handler struct {
	Next   http.Handler
	Config *ScopeConfiguration
	Scope  string // Basically this will be stripped from the full URL and the target path swapped in.
}

// ServeHTTP handles any uploads, else defers the request to the next handler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var callNext bool

	httpCode, err := h.serveHTTP(w, r,
		h.Scope, h.Config,
		func(w http.ResponseWriter, r *http.Request) (int, error) {
			callNext = true
			return 0, nil
		},
	)

	if callNext {
		h.Next.ServeHTTP(w, r)
		return
	}
	if httpCode >= 400 {
		http.Error(w, err.Error(), httpCode)
	}
}
