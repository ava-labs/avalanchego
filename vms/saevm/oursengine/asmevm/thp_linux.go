package asmevm

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// hugePages backs the process's large anonymous read-write mappings (the Go
// heap: state tables, decoded code streams, maps) with transparent huge pages:
// MADV_HUGEPAGE for later faults and MADV_COLLAPSE for what is populated. It is
// setup work before the timed replay; the kernel only needs THP "madvise" (the
// EC2 default). On the AMD box it removes nearly all page walks of the replay.
// ASMEVM_THP=0 turns it off for A/B measurements.
func hugePages() {
	if os.Getenv("ASMEVM_THP") == "0" {
		return
	}
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		return
	}
	defer f.Close()
	const huge = 2 << 20
	const madvCollapse = 0x19 // MADV_COLLAPSE (Linux 6.1), not in package syscall
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text()) // start-end perms offset dev inode [path]
		if len(p) < 5 || p[1] != "rw-p" || len(p) > 5 && !strings.HasPrefix(p[5], "[anon:") {
			continue // not writable, file-backed, or [stack]/[heap]; Go names its heap "[anon: Go: heap]"
		}
		r := strings.SplitN(p[0], "-", 2)
		lo, err1 := strconv.ParseUint(r[0], 16, 64)
		hi, err2 := strconv.ParseUint(r[1], 16, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		lo, hi = (lo+huge-1)&^(huge-1), hi&^(huge-1)
		if hi <= lo {
			continue
		}
		syscall.Syscall(syscall.SYS_MADVISE, uintptr(lo), uintptr(hi-lo), syscall.MADV_HUGEPAGE)
		syscall.Syscall(syscall.SYS_MADVISE, uintptr(lo), uintptr(hi-lo), madvCollapse)
	}
}
