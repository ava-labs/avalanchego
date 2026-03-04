// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import "io/fs"

var _ fs.DirEntry = MockFile{}

// MockFile is an implementation of fs.File for unit testing.
type MockFile struct {
	MockName    string
	MockIsDir   bool
	MockType    fs.FileMode
	MockInfo    fs.FileInfo
	MockInfoErr error
}

func (m MockFile) Name() string {
	return m.MockName
}

func (m MockFile) IsDir() bool {
	return m.MockIsDir
}

func (m MockFile) Type() fs.FileMode {
	return m.MockType
}

func (m MockFile) Info() (fs.FileInfo, error) {
	return m.MockInfo, m.MockInfoErr
}
