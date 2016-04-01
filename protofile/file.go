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

	// needed for file allocations
	maxInt64 = 1<<63 - 1
)

type ProtoFileBehaver interface {
	// Discards a file that has not yet been persisted.
	Zap() error

	// Emerges the file under the initially given name into observable namespace on disk.
	Persist() error

	// Reserves space on disk for the file contents.
	SizeWillBe(numBytes uint64) error

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
