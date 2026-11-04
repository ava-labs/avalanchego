//go:build arm64

package asmevm

//go:noescape
func keccakF1600(a *[25]uint64)

func keccakPermute(st *[25]uint64) { keccakF1600(st) }

// KeccakImpl names the permutation keccakPermute uses.
func KeccakImpl() string { return "arm64-sha3-asm" }
