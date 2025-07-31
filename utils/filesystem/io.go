// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"io/fs"
	"os"
)

var _ Reader = reader{}

// Reader is an interface for reading the filesystem.
type Reader interface {
	// ReadDir reads a given directory.
	// Returns the files in the directory.
	ReadDir(string) ([]fs.DirEntry, error)
}

type reader struct{}

// NewReader returns an instance of Reader
func NewReader() Reader {
	return reader{}
}

// This is just a wrapper around os.ReadDir to make testing easier.
func (reader) ReadDir(dirname string) ([]fs.DirEntry, error) {
	return os.ReadDir(dirname)
}
