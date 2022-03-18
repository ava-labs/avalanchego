// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"io/fs"
	"io/ioutil"
)

var _ Reader = reader{}

// Reader is an interface for reading the filesystem.
type Reader interface {
	// ReadDir reads a given directory.
	// Returns the files in the directory.
	ReadDir(string) ([]fs.FileInfo, error)
}

type reader struct{}

// NewReader returns an instance of Reader
func NewReader() Reader {
	return reader{}
}

// This is just a wrapper around ioutil.ReadDir to make testing easier.
func (reader) ReadDir(dirname string) ([]fs.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}
