//go:build asmcore && amd64

package asmevm

import "golang.org/x/sys/cpu"

// The core uses SHLD/SHRD/BSWAP/MULQ/DIVQ (baseline x86-64) and calls the
// BMI1/BMI2 keccak permutation.
var asmEVMAvailable = cpu.X86.HasBMI1 && cpu.X86.HasBMI2

func asmPreempt() bool

// archNativeCalls: the core implements CALL, STATICCALL and DELEGATECALL frames.
const archNativeCalls = true

// archFused: the amd64 core has every fused slot.
const archFused = fusedCount

// slotsOK checks the slot-check run of asmRun: ret1 is slot 0 as derived from
// the probe slot after the fused slots, ret0 an address just before slot 0.
// An overflowing slot shifts the probe by 128 bytes and puts ret1 before ret0.
func slotsOK(f *asmFrame) bool { d := f.ret1 - f.ret0; return f.ret1 > f.ret0 && d < 160 }

// AsmKeccakImpl names the permutation the native core uses for KECCAK256.
func AsmKeccakImpl() string { return "amd64-bmi2-asm (CALLed from the core)" }
