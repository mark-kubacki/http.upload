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
	err := os.MkdirAll(path, 0750)
	if err != nil {
		return nil, err
	}
	t, err := os.OpenFile(path, os.O_WRONLY|unix.O_TMPFILE, 0600)
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
// by linking the FD to a name in the filesystem where it had been opened.
func (p unixProtoFile) Persist() error {
	err := p.File.Sync()
	if err != nil {
		return err
	}

	fd := p.File.Fd()
	oldpath := "/proc/self/fd/" + uitoa(uint(fd)) // always ≥0 (often ≥4)
	// As of Go 1.6 it is not possible to call Linkat with a FD only.
	// Therefore we must be tricky with those parameters:
	err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW)
	if os.IsExist(err) { // Someone claimed or name!
		finfo, err2 := os.Stat(p.finalName)
		if err2 == nil && !finfo.IsDir() {
			os.Remove(p.finalName) // Similar to creat() we will "overwrite" it.
			err = linkat(fd, oldpath, unix.AT_FDCWD, p.finalName, unix.AT_SYMLINK_FOLLOW)
		}
	}
	// 'linkat' catches many of the errors 'os.Create' would throw.
	// Only at a later point in a file's lifecycle.
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
		return syscall.Fallocate(fd, 0, 0, int64(numBytes))
	}
	// Yes, every Exbibyte counts.
	err := syscall.Fallocate(fd, 0, 0, maxInt64)
	if err != nil {
		return err
	}
	return syscall.Fallocate(fd, 0, maxInt64, int64(numBytes-maxInt64))
}
