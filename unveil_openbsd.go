// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// Errors returned by Unveil or UnveilBlock.
const (
	errUnveil       unveilError = "Call 'unveil' failed"
	errUnveilE2BIG  unveilError = "Call 'unveil' failed: Per-process limit reached"
	errUnveilENOENT unveilError = "Call 'unveil' failed: Path does not exist"
	errUnveilEINVAL unveilError = "Call 'unveil' failed: Invalid value for 'permissions'"
	errUnveilEPERM  unveilError = "Call 'unveil' failed: Called after locking"
)

type unveilError string

func (e unveilError) Error() string { return string(e) }

func translateUnveilErrorCode(err error) error {
	if err == nil {
		return nil
	}
	switch err {
	case syscall.E2BIG:
		return errUnveilE2BIG
	case syscall.ENOENT:
		return errUnveilENOENT
	case syscall.EINVAL:
		return errUnveilEINVAL
	case syscall.EPERM:
		return errUnveilEPERM
	}
	return err
}

// Unveil registers paths that shall remain accessible.
func unveil(path, perm string) error {
	return translateUnveilErrorCode(unix.Unveil(path, perm))
}

// UnveilBlock removes access to any remaining paths from this process.
//
// Call this last, after any invocations of Unveil.
func unveilBlock() error {
	return translateUnveilErrorCode(unix.UnveilBlock())
}
