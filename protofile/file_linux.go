package protofile // import "blitznote.com/src/caddy.upload/protofile"

import "syscall"

// Asks the filesystem to reserve some space for this file's contents.
// This could result in a sparse file (if you wrote less than anticipated)
// or shrink the file.
func (p generalizedProtoFile) SizeWillBe(numBytes uint64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}

	fd := int(p.File.Fd())
	if numBytes <= maxInt64 {
		return syscall.Fallocate(fd, 0, 0, int64(numBytes))
	}
	err := syscall.Fallocate(fd, 0, 0, maxInt64)
	if err != nil {
		return err
	}
	return syscall.Fallocate(fd, 0, maxInt64, int64(numBytes-maxInt64))
}
