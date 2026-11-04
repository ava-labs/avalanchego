package chunkfile

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	seekHole = 3
	seekData = 4
)

// punch frees [off, off+n) with fcntl F_PUNCHHOLE (99); the size is kept.
func punch(f *os.File, off, n int64) error {
	arg := struct {
		flags, reserved uint32
		off, len        int64
	}{off: off, len: n}
	if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), 99, uintptr(unsafe.Pointer(&arg))); e != 0 {
		return e
	}
	return nil
}
