// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package upload contains a HTTP handler
// that provides facilities for uploading files.
//
// Use flags for http server implementations other than Go's own,
// like this:
//  go build -tags "caddyserver0.9 caddyserver1.0" …
// Those tags start with the first version, followed by all major.minor up to its current version.
// Please see how Go does it: https://golang.org/pkg/go/build/#hdr-Build_Constraints
//
// Absent any meaningful flags use the http.Handler implementation (see the following example).
//
package upload // import "blitznote.com/src/http.upload/v3"
