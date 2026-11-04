//go:build !amd64

package keccaksimd

func hashMany(out [][32]byte, msgs [][]byte) { hashGeneric(out, msgs) }
