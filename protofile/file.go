package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"io"
	"io/ioutil"
	"os"
)

const (
	// If a file is expected to be smaller than this (in bytes)
	// then skip reserving space for it in advance.
	reserveFileSizeThreshold = 1 << 15

	// Defined here to avoid the import of "math", and needed in file allocation functions.
	maxInt64 = 1<<63 - 1
)

// ProtoFileBehaver is implemented by all variants of ProtoFile.
//
// Use this in pointers to any ProtoFile you want to utilize.
type ProtoFileBehaver interface {
	// Discards a file that has not yet been persisted/closed, else a NOP.
	Zap() error

	// Emerges the file under the initially given name into observable namespace on disk.
	// This closes the file.
	Persist() error

	// Reserves space on disk by writelessly inflating the (then empty) file.
	SizeWillBe(numBytes uint64) error

	io.Writer
}

// ProtoFile represents a file that can be discarded or named after having been written.
// (With normal files such an committment is made ex ante, on creation.)
type ProtoFile struct {
	*os.File

	persisted bool // Has this already appeared under its final name?
	finalName string
}

// IntentNew "creates" a file which, ideally, is nameless at that point.
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
