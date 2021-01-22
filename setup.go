// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"path/filepath"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ScopeConfiguration represents the settings of a scope (URL path).
type ScopeConfiguration struct {
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
}

// NewDefaultConfiguration creates a new default configuration.
func NewDefaultConfiguration(targetDirectory string) *ScopeConfiguration {
	if targetDirectory != "" { // Primarily to strip any trailing slash (separator).
		targetDirectory = filepath.Clean(targetDirectory)
	}

	cfg := ScopeConfiguration{
		WriteToPath: targetDirectory,
	}

	return &cfg
}
