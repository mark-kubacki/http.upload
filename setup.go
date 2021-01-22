// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"net/http"
	"path/filepath"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Handler will deal with anything that manipulates files,
// but won't deliver a listing or serve them.
type Handler struct {
	MaxFilesize        int64
	MaxTransactionSize int64

	// Target directory on disk that serves as upload destination.
	WriteToPath string

	// Uploaded files can be gotten back from here.
	// If ≠ "" this will trigger sending headers such as "Location".
	ApparentLocation string

	// Enables MOVE, DELETE, and similar. Without this only POST and PUT will be recognized.
	EnableWebdav bool

	// Set this to reject any non-conforming filenames.
	UnicodeForm *struct{ Use norm.Form }

	// Limit the acceptable alphabet(s) for filenames by setting this value.
	RestrictFilenamesTo []*unicode.RangeTable

	// Append '_' and a randomized suffix of that length.
	RandomizedSuffixLength uint32

	// For methods that are not recognized.
	Next http.Handler
	// The path, to be stripped from the full URL and the target path swapped in.
	Scope string
}

// NewHandler creates a new instance of this plugin's upload handler,
// meant to be used in Go's own http server.
//
// 'scope' is the prefix of the upload destination's URL.Path, like `/dir/to/upload/destination`.
//
// 'next' is optional and can be nil.
func NewHandler(scope string, targetDirectory string, next http.Handler) (*Handler, error) {
	if targetDirectory != "" { // Primarily to strip any trailing slash (separator).
		targetDirectory = filepath.Clean(targetDirectory)
	}
	// Don't check whether targetDirectory actually exists.
	// Years ago this has been part of a http server daemon whose author
	// insists on “lazy checks.”

	h := Handler{
		WriteToPath: targetDirectory,
		Next:        next,
		Scope:       scope,
	}
	return &h, nil
}
