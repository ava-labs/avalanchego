// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

func (n *node) isValueNode() bool {
	return len(n.children) == 0 && !n.value.IsNothing()
}

func (n *node) encodeRLP(w rlp.EncoderBuffer) {
	// case 1: there are no children
	if len(n.children) == 0 {
		// the case where there is no value corresponds to an empty trie
		// the case with a value correspond to value nodes in ethereum representation.
		if !n.value.IsNothing() {
			w.Write(valueOrHash(n.value.Value()))
		} else {
			w.Write(rlp.EmptyString)
		}
		return
	}

	// case 2: there is 1 child and no value
	// this is only possible for the root node
	// NOTE: this case will change when the implementation of the root node changes
	if len(n.children) == 1 && n.value.IsNothing() {
		for idx, child := range n.children {
			compressedPath := append([]byte{idx}, child.compressedPath.Bytes()...)
			// altID will contain either the hash or the RLP encoded value if < 32 bytes
			bytes := shortNodeIfNeeded(compressedPath, child.altID, child.isValueNode)
			w.Write(bytes)
			break // should only be 1 iteration
		}
		return
	}

	// case 3: there is multiple children
	if len(n.children) > 1 || len(n.children) == 1 && !n.value.IsNothing() {
		offset := w.List()
		for i := byte(0); i < 16; i++ {
			if child, ok := n.children[i]; ok {
				// altID will contain either the hash or the RLP encoded value if < 32 bytes
				bytes := shortNodeIfNeeded(child.compressedPath.Bytes(), child.altID, child.isValueNode)
				w.Write(valueOrHash(bytes))
			} else {
				w.Write(rlp.EmptyString)
			}
		}

		if !n.value.IsNothing() {
			w.WriteBytes(valueOrHash(n.value.Value()))
		} else {
			w.Write(rlp.EmptyString)
		}
		w.ListEnd(offset)
		return
	}

	panic("unexpected case")
}

func shortNodeIfNeeded(compressedKey []byte, val []byte, isValueNode bool) []byte {
	w := rlp.NewEncoderBuffer(nil)
	shortNodeIfNeededWriter(w, compressedKey, val, isValueNode)

	bytes := w.ToBytes()
	fmt.Println(
		"shortNodeIfNeeded: bytes",
		common.Bytes2Hex(bytes),
	)
	return bytes
}

func shortNodeIfNeededWriter(w rlp.EncoderBuffer, compressedKey []byte, val []byte, isValueNode bool) {
	if isValueNode {
		compressedKey = append(compressedKey, 0x10)
	}

	if len(compressedKey) == 0 {
		fmt.Println("shortNodeIfNeeded: compressedKey is empty", common.Bytes2Hex(val))
		w.Write(val)
		return
	}

	offset := w.List()

	compactKey := hexToCompact(compressedKey)
	w.WriteBytes(compactKey)
	if len(val) > 0 {
		w.WriteBytes(val)
	} else {
		w.Write(rlp.EmptyString)
	}
	w.ListEnd(offset)
	fmt.Println("shortNodeIfNeeded: compactKey", common.Bytes2Hex(compactKey), "val", common.Bytes2Hex(val))
}

func valueOrHash(value []byte) []byte {
	if len(value) < 32 {
		return value
	}
	return hashData(value)
}

func hashData(data []byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	_, err := hasher.Write(data)
	if err != nil {
		panic(err)
	}

	return hasher.Sum(nil)
}

func hexToCompact(hex []byte) []byte {
	terminator := byte(0)
	if hasTerm(hex) {
		terminator = 1
		hex = hex[:len(hex)-1]
	}
	buf := make([]byte, len(hex)/2+1)
	buf[0] = terminator << 5 // the flag byte
	if len(hex)&1 == 1 {
		buf[0] |= 1 << 4 // odd flag
		buf[0] |= hex[0] // first nibble is contained in the first byte
		hex = hex[1:]
	}
	decodeNibbles(hex, buf[1:])
	return buf
}

func decodeNibbles(nibbles []byte, bytes []byte) {
	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
	}
}

// hasTerm returns whether a hex key has the terminator flag.
func hasTerm(s []byte) bool {
	return len(s) > 0 && s[len(s)-1] == 16
}

func bytesToAltID(b []byte) ids.ID {
	var result ids.ID
	if len(b) < 32 {
		b = hashData(b)
	}

	copy(result[:], b)
	return result
}
