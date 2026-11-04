//go:build asmcore && arm64 && darwin

package asmevm

import "golang.org/x/sys/unix"

// hasSHA3 reports whether the CPU implements FEAT_SHA3 (EOR3/RAX1/XAR/BCAX),
// which the native keccak permutation requires.
func hasSHA3() bool {
	v, err := unix.SysctlUint32("hw.optional.armv8_2_sha3")
	return err == nil && v == 1
}
