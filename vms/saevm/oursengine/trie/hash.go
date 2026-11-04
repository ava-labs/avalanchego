// Package trie (copied from faster-firewood-trie/cut) is an Ethereum MPT (Firewood ethhash roots) that stores only its top levels:
// every trie has one cut depth, and a subtree at the cut is recomputed from its key run in
// the sorted era base merged with the in-RAM overlay. See assets/trie-design-2026-10-19.md in
// the faster firewood project.
package trie

import (
	"encoding/binary"
	"hash"

	"golang.org/x/crypto/sha3"
)

// Kinds of a position.
const (
	kEmpty  = 0
	kBranch = 1 // a branch sits exactly at this depth
	kOther  = 2 // a single leaf, or an extension over a deeper branch
)

// Pos is a stored position: the reference its parent branch holds for it.
type Pos struct {
	Ref  [32]byte
	N    uint8 // len of the reference (32 = hash, < 32 = inline RLP, 0 = empty)
	Kind uint8
}

func (p *Pos) ref() []byte { return p.Ref[:p.N] }

func posOf(ref []byte, kind uint8) Pos {
	var p Pos
	p.N = uint8(copy(p.Ref[:], ref))
	p.Kind = kind
	return p
}

// EmptyRoot is keccak(0x80).
var EmptyRoot = [32]byte{0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6, 0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e, 0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0, 0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21}

// EmptyCode is keccak("").
var EmptyCode = [32]byte{0xc5, 0xd2, 0x46, 0x01, 0x86, 0xf7, 0x23, 0x3c, 0x92, 0x7e, 0x7d, 0xb2, 0xdc, 0xc7, 0x03, 0xc0, 0xe5, 0x00, 0xb6, 0x53, 0xca, 0x82, 0x27, 0x3b, 0x7b, 0xfa, 0xd8, 0x04, 0x5d, 0x85, 0xa4, 0x70}

type keccakState interface {
	hash.Hash
	Read([]byte) (int, error)
}

// Keccaker is a reusable scalar Keccak-256.
type Keccaker struct{ st keccakState }

func NewKeccaker() *Keccaker { return &Keccaker{st: sha3.NewLegacyKeccak256().(keccakState)} }

func (k *Keccaker) Sum(b []byte) (out [32]byte) {
	k.st.Reset()
	k.st.Write(b)
	k.st.Read(out[:])
	return out
}

// --- RLP ---

func appendLen(dst []byte, base byte, n int) []byte {
	if n < 56 {
		return append(dst, base+byte(n))
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	i := 0
	for b[i] == 0 {
		i++
	}
	dst = append(dst, base+55+byte(8-i))
	return append(dst, b[i:]...)
}

func appendStr(dst, s []byte) []byte {
	if len(s) == 1 && s[0] < 0x80 {
		return append(dst, s[0])
	}
	return append(appendLen(dst, 0x80, len(s)), s...)
}

// appendRef appends a child reference item: a 32-byte string or the raw inline RLP.
func appendRef(dst, ref []byte) []byte {
	if len(ref) == 32 {
		return appendStr(dst, ref)
	}
	if len(ref) == 0 {
		return append(dst, 0x80)
	}
	return append(dst, ref...)
}

func nib(k *[32]byte, i int) byte {
	if i&1 == 0 {
		return k[i>>1] >> 4
	}
	return k[i>>1] & 15
}

// appendHexPrefix appends the compact encoding of key nibbles [from, to) as an RLP string.
func appendHexPrefix(dst []byte, k *[32]byte, from, to int, leaf bool) []byte {
	n := to - from
	l := n/2 + 1
	flag := byte(0)
	if leaf {
		flag = 2
	}
	var first byte
	i := from
	if n%2 == 1 {
		first = (flag+1)<<4 | nib(k, i)
		i++
	} else {
		first = flag << 4
	}
	if l == 1 {
		return appendStr(dst, []byte{first})
	}
	dst = appendLen(dst, 0x80, l)
	dst = append(dst, first)
	if i&1 == 0 {
		return append(dst, k[i>>1:to>>1]...)
	}
	for ; i < to; i += 2 {
		dst = append(dst, nib(k, i)<<4|nib(k, i+1))
	}
	return dst
}

// wrapList writes the list header for payload p (already in buf after hdr room) and returns
// the encoding. buf[:9] is room for the header.
func wrapList(buf []byte) []byte {
	n := len(buf) - 9
	var h [9]byte
	hb := appendLen(h[:0], 0xc0, n)
	s := 9 - len(hb)
	copy(buf[s:], hb)
	return buf[s:]
}

// leafEnc returns the leaf node encoding for key nibbles [from, 64) and value item val
// (val is wrapped as an RLP string).
func leafEnc(buf []byte, k *[32]byte, from int, val []byte) []byte {
	buf = append(buf[:0], make([]byte, 9)...)
	buf = appendHexPrefix(buf, k, from, 64, true)
	buf = appendStr(buf, val)
	return wrapList(buf)
}

func extEnc(buf []byte, k *[32]byte, from, to int, child []byte) []byte {
	buf = append(buf[:0], make([]byte, 9)...)
	buf = appendHexPrefix(buf, k, from, to, false)
	buf = appendRef(buf, child)
	return wrapList(buf)
}

// branchEnc encodes a 17-item branch with the given child references (nil = empty).
func branchEnc(buf []byte, kids *[16][]byte) []byte {
	buf = append(buf[:0], make([]byte, 9)...)
	for _, r := range kids {
		buf = appendRef(buf, r)
	}
	buf = append(buf, 0x80)
	return wrapList(buf)
}

// refOf is the node reference of an encoding: inline below 32 bytes, else its hash.
func (k *Keccaker) refOf(dst *Pos, enc []byte, kind uint8) {
	dst.Kind = kind
	if len(enc) < 32 {
		dst.N = uint8(copy(dst.Ref[:], enc))
		return
	}
	dst.Ref = k.Sum(enc)
	dst.N = 32
}

// RootHash is the trie root hash of a level-0 position.
func (k *Keccaker) RootHash(p *Pos) [32]byte {
	if p.N == 0 {
		return EmptyRoot
	}
	if p.N == 32 {
		return p.Ref
	}
	return k.Sum(p.Ref[:p.N])
}

// StorageVal is the leaf value item of a storage slot: the RLP string of the trimmed word.
func StorageVal(dst, trimmed []byte) []byte {
	for len(trimmed) > 0 && trimmed[0] == 0 {
		trimmed = trimmed[1:]
	}
	return appendStr(dst, trimmed)
}

func appendUint(dst []byte, v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	i := 0
	for i < 8 && b[i] == 0 {
		i++
	}
	return appendStr(dst, b[i:])
}

// AccountVal is the account leaf value: RLP [nonce, balance, storage root, code hash] plus
// the coreth multicoin bool when fiveFields (coreth registers it, so the C-Chain writes 5).
func AccountVal(dst []byte, nonce uint64, balance []byte, root *[32]byte, codeHash []byte, multicoin, fiveFields bool) []byte {
	for len(balance) > 0 && balance[0] == 0 {
		balance = balance[1:]
	}
	if codeHash == nil {
		codeHash = EmptyCode[:]
	}
	s := len(dst)
	dst = append(dst, make([]byte, 9)...)
	dst = appendUint(dst, nonce)
	dst = appendStr(dst, balance)
	dst = appendStr(dst, root[:])
	dst = appendStr(dst, codeHash)
	if fiveFields {
		if multicoin {
			dst = append(dst, 0x01)
		} else {
			dst = append(dst, 0x80)
		}
	}
	enc := wrapList(dst[s:])
	n := copy(dst[s:], enc)
	return dst[:s+n]
}
