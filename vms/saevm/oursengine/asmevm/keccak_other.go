//go:build !arm64 && !amd64

package asmevm

func keccakPermute(st *[25]uint64) { keccakF1600Generic(st) }

// KeccakImpl names the permutation keccakPermute uses.
func KeccakImpl() string { return "go-generic-looped" }
