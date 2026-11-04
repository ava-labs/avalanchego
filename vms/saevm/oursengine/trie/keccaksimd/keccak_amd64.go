package keccaksimd

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var hasAVX2, hasAVX512 = cpu.X86.HasAVX2, cpu.X86.HasAVX512F

func hashMany(out [][32]byte, msgs [][]byte) {
	switch {
	case hasAVX512:
		q := len(msgs) / 8 * 8
		hashLanes(out[:q], msgs[:q], 8)
		if r := len(msgs) - q; r > 4 {
			hashLanes(out[q:], msgs[q:], 8)
		} else if r > 0 {
			hashLanes(out[q:], msgs[q:], 4)
		}
	case hasAVX2:
		hashLanes(out, msgs, 4)
	default:
		hashGeneric(out, msgs)
	}
}

// hashLanes hashes msgs (all with the same number of full 136-byte blocks) in groups of w (4 or 8) lanes; idle lanes of the last group repeat
// its last message. Full blocks are read in place, the padded last block from a copy.
func hashLanes(out [][32]byte, msgs [][]byte, w int) {
	if len(msgs) == 0 {
		return
	}
	full := len(msgs[0]) / rate // every message has this many full blocks; tails differ
	var st [25 * 8]uint64       // [25][w]uint64
	var p [8]*byte
	var pad [8][rate]byte
	absorb := func(blocks int) {
		if w == 8 {
			absorb8((*[25][8]uint64)(unsafe.Pointer(&st)), &p, blocks)
		} else {
			absorb4((*[25][4]uint64)(unsafe.Pointer(&st)), (*[4]*byte)(p[:4]), blocks)
		}
	}
	for i := 0; i < len(msgs); i += w {
		k := min(w, len(msgs)-i)
		clear(st[:])
		if full > 0 {
			for j := 0; j < w; j++ {
				p[j] = &msgs[i+min(j, k-1)][0]
			}
			absorb(full)
		}
		for j := 0; j < w; j++ {
			pad[j] = [rate]byte{}
			m := msgs[i+min(j, k-1)]
			copy(pad[j][:], m[full*rate:])
			pad[j][len(m)-full*rate] = 0x01
			pad[j][rate-1] |= 0x80
			p[j] = &pad[j][0]
		}
		absorb(1)
		for j := 0; j < k; j++ {
			for q := 0; q < 4; q++ {
				binary.LittleEndian.PutUint64(out[i+j][8*q:], st[q*w+j])
			}
		}
	}
}
