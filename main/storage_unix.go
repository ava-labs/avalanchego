// +build !windows

package main

import "syscall"

func verifyDiskStorage(storagePath string) (uint64, uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, 0, err
	}
	size, dsErr := dirSize(storagePath)
	if dsErr != nil {
		return 0, 0, dsErr
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	twox := size + size
	saftyBuf := (twox * 15) / 100
	return avail, size + saftyBuf, nil
}
