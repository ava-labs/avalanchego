// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package filesystem

import (
	"errors"
	"io/fs"
	"os"
)

// Renames the file "a" to "b" iff "a" exists.
// It returns "true" and no error, if rename were successful.
// It returns "false" and no error, if the file "a" does not exist.
// It returns "false" and an error, if rename failed.
func RenameIfExists(a, b string) (renamed bool, err error) {
	err = os.Rename(a, b)
	renamed = err == nil
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return renamed, err
}
