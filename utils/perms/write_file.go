// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package perms

import (
	"errors"
	"os"

	// `maybe.WriteFile` will use `ioutil.WriteFile` (non-atomic) on
	// windows, and `renameio.WriteFile` (atomic) on everything else.
	"github.com/google/renameio/v2/maybe"
)

// WriteFile writes [data] to [filename] and ensures that [filename] has [perm]
// permissions.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		// The file doesn't exist, so try to write it.
		return maybe.WriteFile(filename, data, perm)
	}
	if err != nil {
		return err
	}
	if info.Mode() != perm {
		// The file currently has the wrong permissions, so update them.
		if err := os.Chmod(filename, perm); err != nil {
			return err
		}
	}
	// The file has the right permissions, so truncate any data and write the
	// file.
	return maybe.WriteFile(filename, data, perm)
}
