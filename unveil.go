// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !openbsd

package upload

// Unveil registers paths that shall remain accessible.
//
// Is a nop on this operating system.
func unveil(path, perm string) error {
	return nil
}

// UnveilBlock removes access to any remaining paths from this process.
//
// Call this last, after any invocations of Unveil.
//
// Is a nop on this operating system.
func unveilBlock() error {
	return nil
}
