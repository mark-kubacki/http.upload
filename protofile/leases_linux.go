// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !appengine

package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"os"
	"syscall"
)

// Is used with Linux if O_TMPFILE didn't work.
// Utilizes Linux facilities that prevent tampering with file-contents.
type unixDottedProtoFile struct {
	generalizedProtoFile
}

// Getting a lease on a file will result in the kernel notifying us about
// any side effects (e.g. other processes) breaking that lease.
// The signal RT_SIGNAL_LEASE is for that, but we won't use it here.
// We're after the benefit of the kernel halting our 'write' call rather than
// killing our process.

// Utilizes the kernel's file locking mechanisms.
// A different process watching file (creation) events would ideally
// 'open' with O_NONBLOCK and notice its mistake (if it opened it prematuerly)
// by getting a EWOULDBLOCK due to the lease.
func intentNewUnixDotted(path, filename string) (*ProtoFileBehaver, error) {
	orig, err := intentNewUniversal(path, filename)
	if err != nil {
		return orig, err
	}
	g := (*orig).(generalizedProtoFile)

	fcntl(g.File.Fd(), syscall.F_SETLEASE, syscall.F_WRLCK) // WRLCK includes RDLCK
	// An error is not expected because we created that file, with a random name;
	// - either the kernel does not support locking at all and the error can be ignored anyway
	// - or anything malevolent is locking our file.

	n := ProtoFileBehaver(unixDottedProtoFile{
		generalizedProtoFile: g,
	})
	return &n, err
}

func (p unixDottedProtoFile) Zap() error {
	if p.persisted {
		return nil
	}
	fcntl(p.File.Fd(), syscall.F_SETLEASE, syscall.F_UNLCK)
	return p.generalizedProtoFile.Zap()
}

func (p unixDottedProtoFile) Persist() error {
	defer p.File.Close() // yes, this gets called up to two times
	err := p.File.Sync()
	if err != nil {
		return err
	}
	err = os.Rename(p.File.Name(), p.finalName)
	if err != nil {
		return err
	}
	p.persisted = true

	fcntl(p.File.Fd(), syscall.F_SETLEASE, syscall.F_UNLCK)

	return p.File.Close()
}
