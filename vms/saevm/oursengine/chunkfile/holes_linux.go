package chunkfile

import (
	"os"
	"syscall"
)

const (
	seekData = 3
	seekHole = 4
)

// punch frees [off, off+n) with fallocate PUNCH_HOLE|KEEP_SIZE.
func punch(f *os.File, off, n int64) error {
	return syscall.Fallocate(int(f.Fd()), 0x2|0x1, off, n)
}
