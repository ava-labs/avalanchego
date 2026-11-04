package chunkfile

import (
	"os"
	"syscall"
	"unsafe"
)

// NoPrealloc gives f, still empty, a 1 MB extent size hint: XFS then allocates in 1 MB steps
// instead of preallocating speculatively past the end of a growing file (GBs on a file of tens
// of GBs, measured 8.6 GB at 6 GB written, kept until the file is closed), which would hold
// the disk a fold gives back by punching the old base. Best effort: other file systems refuse
// the hint and allocate as they do.
func NoPrealloc(f *os.File) {
	var x struct {
		xflags, extsize, nextents, projid, cowextsize uint32
		pad                                           [8]byte
	}
	const get, set, flagExtsize = 0x801c581f, 0x401c5820, 0x800 // FS_IOC_FSGETXATTR, FS_IOC_FSSETXATTR, FS_XFLAG_EXTSIZE
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), get, uintptr(unsafe.Pointer(&x))); e != 0 {
		return
	}
	x.xflags |= flagExtsize
	x.extsize = ChunkSize
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), set, uintptr(unsafe.Pointer(&x)))
}
