package asmevm

// keccakRC holds the 24 Keccak-f[1600] round constants. Referenced by
// keccak_arm64.s and keccakF1600Generic.
var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808a, 0x8000000080008000,
	0x000000000000808b, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008a, 0x0000000000000088, 0x0000000080008009, 0x000000008000000a,
	0x000000008000808b, 0x800000000000008b, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800a, 0x800000008000000a,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

// Keccak256 computes the legacy (Ethereum) Keccak-256 of data. Rate 136 bytes,
// 0x01 padding. Permutation is native on arm64 with FEAT_SHA3, pure Go else.
func Keccak256(data []byte) [32]byte {
	var st [25]uint64
	n := len(data)
	full := n - n%136
	for off := 0; off < full; off += 136 {
		absorb(&st, data[off:off+136])
		keccakPermute(&st)
	}
	var block [136]byte
	copy(block[:], data[full:])
	block[n-full] ^= 0x01
	block[135] ^= 0x80
	absorb(&st, block[:])
	keccakPermute(&st)
	var out [32]byte
	for i := 0; i < 4; i++ {
		putU64(out[i*8:], st[i])
	}
	return out
}

func absorb(st *[25]uint64, block []byte) {
	for i := 0; i < 17; i++ {
		st[i] ^= getU64(block[i*8:])
	}
}

func getU64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func putU64(b []byte, v uint64) {
	_ = b[7]
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	b[4], b[5], b[6], b[7] = byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56)
}
