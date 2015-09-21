package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"io"
	"io/ioutil"
	"os"
)

const (
	// If a file is expected to be smaller than this (in bytes) won't seek
	// to EOF for writing a single byte, which would've "announced" the final size.
	reserveFileSizeThreshold = 1 << 15
)

type ProtoFileBehaver interface {
	// Discards a file that has not yet been persisted.
	Zap() error

	// Emerges the file under the initially given name into observable namespace on disk.
	Persist() error

	// Reserves space on disk for the file contents.
	SizeWillBe(numBytes int64) error

	io.Writer
}

type ProtoFile struct {
	*os.File

	persisted bool // Has this already appeared under its final name?
	finalName string
}

// Calls to 'IntentNew' result in a sink for writes to disk
// which can be emerged into a regular file by calling member function 'Persist'.
//
// Depending on operation- and filesystem a degraded implementation of ProtoFile
// will be used.
var IntentNew func(path, filename string) (*ProtoFileBehaver, error) = intentNewUniversal

type generalizedProtoFile ProtoFile

func intentNewUniversal(path, filename string) (*ProtoFileBehaver, error) {
	err := os.MkdirAll(path, 0750)
	if err != nil {
		return nil, err
	}
	t, err := ioutil.TempFile(path, "."+filename)
	if err != nil {
		return nil, err
	}
	g := ProtoFileBehaver(generalizedProtoFile{
		File:      t,
		finalName: path + "/" + filename,
	})
	return &g, nil
}

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
func (p generalizedProtoFile) SizeWillBe(numBytes int64) error {
	if numBytes <= reserveFileSizeThreshold {
		return nil
	}
	return p.Truncate(numBytes)
}
