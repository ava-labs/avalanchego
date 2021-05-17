// +build windows

package main

import "fmt"

func verifyDiskStorage(path string) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("storage space verification not yet implemented for windows")
}
