package asmevm

import "math/bits"

var keccakRot = [25]uint{
	0, 1, 62, 28, 27, 36, 44, 6, 55, 20, 3, 10, 43, 25, 39,
	41, 45, 15, 21, 8, 18, 2, 61, 56, 14,
}

// keccakF1600Generic is a plain Go Keccak-f[1600]; the x86 port replaces this
// with a tuned scalar permutation (coordinator note 2026-10-18).
func keccakF1600Generic(a *[25]uint64) {
	for round := 0; round < 24; round++ {
		var c [5]uint64
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		var d [5]uint64
		for x := 0; x < 5; x++ {
			d[x] = c[(x+4)%5] ^ bits.RotateLeft64(c[(x+1)%5], 1)
		}
		for i := 0; i < 25; i++ {
			a[i] ^= d[i%5]
		}
		var b [25]uint64
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				i := x + 5*y
				j := y + 5*((2*x+3*y)%5)
				b[j] = bits.RotateLeft64(a[i], int(keccakRot[i]))
			}
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				a[x+5*y] = b[x+5*y] ^ (^b[(x+1)%5+5*y] & b[(x+2)%5+5*y])
			}
		}
		a[0] ^= keccakRC[round]
	}
}
