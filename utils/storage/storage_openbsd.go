// Public Domain

//go:build openbsd
// +build openbsd

package storage

import "syscall"

func AvailableBytes(storagePath string) (uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, err
	}
	avail := uint64(stat.F_bavail) * uint64(stat.F_bsize)
	return avail, nil
}
