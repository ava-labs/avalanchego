// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package perms

import (
	"errors"
	"os"

	"github.com/google/renameio/v2/maybe"
)

// WriteFile writes [data] to [filename] and ensures that [filename] has [perm]
// permissions. Will write atomically on linux/macos and fall back to non-atomic
// ioutil.WriteFile on windows.
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
