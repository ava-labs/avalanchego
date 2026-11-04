//go:build asmcore && arm64

package asmevm

const archNativeCalls = false // CALLs go through the host

var asmEVMAvailable = hasSHA3()

// slotsOK checks the slot-check run of asmRun: ret0 is the span from slot 0 to
// the end of the fused slots, which must be exactly 128 bytes per slot.
func slotsOK(f *asmFrame) bool { return f.ret0 == uint64(256+archFused)*128 }

// archFused: the arm64 core has the first 9 fused slots only.
const archFused = 9

// AsmKeccakImpl names the permutation the native core uses for KECCAK256.
func AsmKeccakImpl() string { return "arm64-sha3-in-core" }
