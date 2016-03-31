// +build !linux

package protofile // import "blitznote.com/src/caddy.upload/protofile"

// Asks the filesystem to reserve some space for this file's contents.
// This could result in a sparse file (if you wrote less than anticipated)
// or shrink the file.
func (p generalizedProtoFile) SizeWillBe(numBytes uint64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}

	if numBytes <= maxInt64 {
		return p.Truncate(int64(numBytes))
	}
	// allocate as much as possible
	return p.Truncate(maxInt64)
}
