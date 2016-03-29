// +build linux,!sparc,!sparc64,!alpha,!parisc,!hppa,!gccgo,!appengine

package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func init() {
	IntentNew = intentNewUnix
}

type unixProtoFile ProtoFile

func intentNewUnix(path, filename string) (*ProtoFileBehaver, error) {
	err := os.MkdirAll(path, 0750)
	if err != nil {
		return nil, err
	}
	t, err := os.OpenFile(path, os.O_WRONLY|unix.O_TMPFILE, 0600)
	// did it fail because…
	if err != nil {
		switch err {
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

func (p unixProtoFile) Persist() error {
	err := p.File.Sync()
	if err != nil {
		return err
	}

	fd := p.File.Fd()
	oldpath := "/proc/self/fd/" + uitoa(uint(fd)) // always ≥0 (often ≥4)
	// As of Go 1.5 it is not possible to call Linkat with a FD only.
	// The first parameter is not AT_FDCWD: ignored with {'oldpath',AT_SYMLINK_FOLLOW}, else needed
	err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW)
	if os.IsExist(err) {
		finfo, err2 := os.Stat(p.finalName)
		if err2 == nil && !finfo.IsDir() {
			os.Remove(p.finalName)
			err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW) // try again
		}
	}
	// 'linkat' catches many of the errors 'os.Create' would throw.
	if err != nil {
		return err
	}
	p.persisted = true
	return p.Close()
}

func (p unixProtoFile) SizeWillBe(numBytes int64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}
	return p.Truncate(numBytes)
}
