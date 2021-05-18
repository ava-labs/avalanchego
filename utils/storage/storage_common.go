package storage
// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
import (
	"os"
	"path/filepath"
)

func DirSize(path string) (uint64, error) {
	var size int64
	err := filepath.Walk(path,
		func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				size += info.Size()
			}
			return nil
		})
	return uint64(size), err
}
