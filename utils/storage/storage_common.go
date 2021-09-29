// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package storage

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// fileExists checks if a file exists before we
// try using it to prevent further errors.
func FileExists(filePath string) (bool, error) {
	info, err := os.Stat(filePath)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// ReadFileWithName reads a single file with name fileNameWithoutExt without specifying any extension.
// it errors when there are more than 1 file with the given fileName
func ReadFileWithName(parentDir string, fileNameNoExt string) ([]byte, error) {
	filePath := filepath.Join(parentDir, fileNameNoExt)
	files, err := filepath.Glob(filePath + ".*") // all possible extensions
	if err != nil {
		return nil, err
	}
	if len(files) > 1 {
		return nil, fmt.Errorf(`too many files matched "%s.*" in %s`, fileNameNoExt, parentDir)
	}
	if len(files) == 0 { // no file found, return nothing
		return nil, nil
	}
	return ioutil.ReadFile(files[0])
}

// folderExists checks if a folder exists before we
// try using it to prevent further errors.
func FolderExists(filePath string) (bool, error) {
	info, err := os.Stat(filePath)
	if err == nil {
		return info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

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
