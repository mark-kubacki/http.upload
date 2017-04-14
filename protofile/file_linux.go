// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"os"
	"syscall"
)

// Call this to discard the file.
// If it has already been persisted (and thereby is a 'regular' one) this will be a NOP.
func (p generalizedProtoFile) Zap() error {
	if p.persisted {
		return nil
	}
	os.RemoveAll(p.File.Name())
	return p.File.Close()
}

// Promotes a proto file to a 'regular' one, which will appear under its final name.
func (p generalizedProtoFile) Persist() error {
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
	return p.File.Close()
}

// Asks the filesystem to reserve some space for this file's contents.
// This could result in a sparse file (if you wrote less than anticipated)
// or shrink the file.
func (p generalizedProtoFile) SizeWillBe(numBytes uint64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}

	fd := int(p.File.Fd())
	if numBytes <= maxInt64 {
		err := syscall.Fallocate(fd, 0, 0, int64(numBytes))
		if err == syscall.EOPNOTSUPP {
			return nil
		}
		return err
	}
	err := syscall.Fallocate(fd, 0, 0, maxInt64)
	if err == syscall.EOPNOTSUPP {
		return nil
	}
	if err != nil {
		return err
	}
	return syscall.Fallocate(fd, 0, maxInt64, int64(numBytes-maxInt64))
}
