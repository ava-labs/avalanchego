// +build !windows

package main

import "syscall"

func osDiskStat(storagePath string) (uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, err
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	return avail, nil
}
