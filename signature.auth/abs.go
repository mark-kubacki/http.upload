// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth // import "blitznote.com/src/caddy.upload/signature.auth"

// Provides functions to compute ›absolute differences‹.
//
// Golang Go's missing »left-pad«.

// Returns the absolute value of n.
//
// Branchless, constant time.
func abs64(n int64) uint64 {
	m := n >> (64 - 1)
	return uint64((n ^ m) - m)
}

// Returns the absolute value of n.
//
// Branchless, constant time, boring.
func abs32(n int32) uint32 {
	m := n >> (32 - 1)
	return uint32((n ^ m) - m)
}
