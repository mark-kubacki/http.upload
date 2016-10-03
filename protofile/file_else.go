// +build !linux

package protofile // import "hub.blitznote.com/src/caddy.upload/protofile"
import "os"

// Call this to discard the file.
// If it has already been persisted (and thereby is a 'regular' one) this will be a NOP.
func (p generalizedProtoFile) Zap() error {
	if p.persisted {
		return nil
	}
	if err := p.File.Close(); err != nil {
		return err
	}
	return os.RemoveAll(p.File.Name())
}

// Promotes a proto file to a 'regular' one, which will appear under its final name.
func (p generalizedProtoFile) Persist() error {
	defer p.File.Close() // yes, this gets called up to two times
	err := p.File.Sync()
	if err != nil {
		return err
	}
	if err = p.File.Close(); err != nil {
		return err
	}
	err = os.Rename(p.File.Name(), p.finalName)
	if err != nil {
		return err
	}
	p.persisted = true
	return nil
}

// Asks the filesystem to reserve some space for this file's contents.
// This could result in a sparse file (if you wrote less than anticipated)
// or truncate it.
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
