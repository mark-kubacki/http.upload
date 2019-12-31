// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"path/filepath"
	"sync"
	"unicode"

	"blitznote.com/src/http.upload/v4/signature.auth"
	"golang.org/x/text/unicode/norm"
)

// ScopeConfiguration represents the settings of a scope (URL path).
type ScopeConfiguration struct {
	// How big a difference between 'now' and the provided timestamp do we tolerate?
	// In seconds. Due to possible optimizations this should be an order of 2.
	// A reasonable default is 1<<2.
	TimestampTolerance uint64

	MaxFilesize        uint64
	MaxTransactionSize uint64

	// Target directory on disk that serves as upload destination.
	WriteToPath string

	// Uploaded files can be gotten back from here.
	// If ≠ "" this will trigger sending headers such as "Location".
	ApparentLocation string

	// Maps KeyIDs to shared secrets.
	// Here the latter are already decoded from base64 to binary.
	// Request verification is disabled if this is empty.
	IncomingHmacSecrets     auth.HmacSecrets
	IncomingHmacSecretsLock sync.RWMutex

	// If false, this plugin returns HTTP Errors.
	// If true, passes the given request to the next middleware
	// which could respond with an Error of its own, poorly obscuring where this plugin is used.
	SilenceAuthErrors bool

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
		unveil(targetDirectory, "rw")
	}

	cfg := ScopeConfiguration{
		TimestampTolerance:  1 << 2,
		WriteToPath:         targetDirectory,
		IncomingHmacSecrets: make(auth.HmacSecrets),
	}

	return &cfg
}

// FinishSetup communicates any collected white- and blacklists to the operating system.
//
// Call this on systems and with seervers that support OS-based locking down.
// Servers that feature a dynamic reconfiguration or the like should not call this.
// Do not call this in unit tests.
//
// Subject to change, has only an effect on OpenBSD.
func FinishSetup() error {
	return unveilBlock()
}
