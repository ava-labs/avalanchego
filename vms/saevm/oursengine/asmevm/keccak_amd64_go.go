//go:build amd64

package asmevm

import "golang.org/x/sys/cpu"

// keccakF1600 is the BMI1/BMI2 scalar permutation in keccak_amd64.s.
//
//go:noescape
func keccakF1600(a *[25]uint64)

var keccakNative = cpu.X86.HasBMI1 && cpu.X86.HasBMI2

func keccakPermute(st *[25]uint64) {
	if keccakNative {
		keccakF1600(st)
		return
	}
	keccakF1600Generic(st)
}

// KeccakImpl names the permutation keccakPermute uses at runtime.
func KeccakImpl() string {
	if keccakNative {
		return "amd64-bmi2-asm"
	}
	return "go-generic-looped"
}
