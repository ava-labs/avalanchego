// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"io/fs"
	"time"
)

// MockFile is an implementation of fs.File for unit testing.
type MockFile struct {
	MockName    string
	MockSize    int64
	MockMode    fs.FileMode
	MockModTime time.Time
	MockIsDir   bool
	MockSys     interface{}
}

func (m MockFile) Name() string {
	return m.MockName
}

func (m MockFile) Size() int64 {
	return m.MockSize
}

func (m MockFile) Mode() fs.FileMode {
	return m.MockMode
}

func (m MockFile) ModTime() time.Time {
	return m.MockModTime
}

func (m MockFile) IsDir() bool {
	return m.MockIsDir
}

func (m MockFile) Sys() interface{} {
	return m.MockSys
}
