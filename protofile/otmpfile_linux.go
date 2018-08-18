// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !appengine

package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func init() {
	IntentNew = intentNewUnix
}

// unixProtoFile is the variant that utilizes O_TMPFILE.
// Although it might seem that data is written to the parent directory itself,
// it actually goes into a nameless file.
type unixProtoFile ProtoFile

func intentNewUnix(path, filename string) (*ProtoFileBehaver, error) {
	err := os.MkdirAll(path, permBitsDir)
	if err != nil {
		return nil, err
	}
	t, err := os.OpenFile(path, os.O_WRONLY|unix.O_TMPFILE, permBitsFile)
	// did it fail because…
	if err != nil {
		perr, ok := err.(*os.PathError)
		if !ok {
			return nil, err
		}
		switch perr.Err {
		case syscall.EISDIR, syscall.ENOENT: // … kernel does not know O_TMPFILE
			// If so, don't try it again.
			IntentNew = intentNewUnixDotted
			fallthrough
		case syscall.EOPNOTSUPP: // … O_TMPFILE is not supported on this FS
			return intentNewUnixDotted(path, filename)
		default: // … something 'regular'.
			return nil, err
		}
	}
	g := ProtoFileBehaver(unixProtoFile{
		File:      t,
		finalName: path + "/" + filename,
	})
	return &g, nil
}

func (p unixProtoFile) Zap() error {
	// NOP because O_TMPFILE files that have not been named get discarded anyway.
	return p.File.Close()
}

// Persist gives the file a name.
//
// Nameless files can be identified using tuple (PID, FD) and named
// by linking the FD to a name in the filesystem on which it had been opened.
func (p unixProtoFile) Persist() error {
	err := p.File.Sync()
	if err != nil {
		return err
	}

	fd := p.File.Fd()
	oldpath := "/proc/self/fd/" + uitoa(uint(fd)) // always ≥0 (often ≥4)
	// As of Go 1.6 it is not possible to call Linkat with a FD only. This is a workaround.
	err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW)
	if os.IsExist(err) { // Someone claimed our name!
		finfo, err2 := os.Stat(p.finalName)
		if err2 == nil && !finfo.IsDir() {
			os.Remove(p.finalName) // To emulate the behaviour of Create we will "overwrite" the other file.
			err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW)
		}
	}
	// 'linkat' catches many of the errors 'os.Create' would throw,
	// only with O_TMPFILE at a later point in the file's lifecycle.
	if err != nil {
		return err
	}
	p.persisted = true
	return p.Close()
}

func (p unixProtoFile) SizeWillBe(numBytes uint64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}

	fd := int(p.File.Fd())
	if numBytes <= maxInt64 {
		err := syscall.Fallocate(fd, 0, 0, int64(numBytes))
		if err == syscall.EOPNOTSUPP {
			return nil
		}

		_ = unix.Fadvise(fd, 0, int64(numBytes), unix.FADV_WILLNEED)
		_ = unix.Fadvise(fd, 0, int64(numBytes), unix.FADV_SEQUENTIAL)
		return err
	}

	// Yes, every Exbibyte counts.
	err := syscall.Fallocate(fd, 0, 0, maxInt64)
	if err == syscall.EOPNOTSUPP {
		return nil
	}
	if err != nil {
		return err
	}

	err = syscall.Fallocate(fd, 0, maxInt64, int64(numBytes-maxInt64))
	if err != nil {
		return err
	}

	// These are best-efford, so we don't care about any errors.
	// For very large files this is not optimal, but covers most of use-cases for now.
	_ = unix.Fadvise(fd, 0, maxInt64, unix.FADV_WILLNEED)
	_ = unix.Fadvise(fd, 0, maxInt64, unix.FADV_SEQUENTIAL)
	return err
}
