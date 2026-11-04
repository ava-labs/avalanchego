package chunkfile

import "os"

// NoPrealloc: APFS does not preallocate past the end of a file (see prealloc_linux.go).
func NoPrealloc(f *os.File) {}
