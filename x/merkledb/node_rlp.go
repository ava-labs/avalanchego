// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

const rlpTokenSize = 4

func toNibbles(key Key) []byte {
	nibbles := make([]byte, key.Length()/rlpTokenSize)
	for i := 0; i < len(nibbles); i++ {
		nibbles[i] = key.Token(i*rlpTokenSize, rlpTokenSize)
	}
	return nibbles
}

func (n *node) isValueNode() bool {
	isLeaf := len(n.children) == 0 && !n.value.IsNothing()
	return isLeaf // || n.isAccountNode()
}

func (n *node) calculateRLP(parent *node) {
	n.rlp = n.encodeRLPWithShortNode(parent)
}

func (n *node) isAccountNode() bool {
	keyLen := n.key.Length() / 8
	isAccount := keyLen == 32
	if isAccount && n.value.IsNothing() {
		panic("account node with no value")
	}
	return isAccount
}

// encodeRLPWithShortNode encodes the node into RLP format (adds a short node if needed)
func (n *node) encodeRLPWithShortNode(parent *node) []byte {
	w := rlp.NewEncoderBuffer(nil)
	n.encodeRLP(w)

	// add the short node if needed
	compressedKey := toNibbles(n.compressedKey(parent))
	return shortNodeIfNeeded(compressedKey, w.ToBytes(), n.isValueNode())
}

func (n *node) compressedKey(parent *node) Key {
	if parent == nil {
		return Key{} // root has no compressed key
	}

	// case: the parent has only 1 child and no value,
	// this is only possible for the root node
	if len(parent.children) == 1 && parent.value.IsNothing() {
		for idx, child := range parent.children {
			compressedKey := ToToken(idx, rlpTokenSize).Extend(child.compressedKey)
			return compressedKey // should only be 1 iteration
		}
	}

	if len(parent.children) > 1 || len(parent.children) == 1 && !parent.value.IsNothing() {
		// find the expected child slot for this node
		idx := n.key.Token(parent.key.Length(), rlpTokenSize)
		child, ok := parent.children[idx]
		if !ok {
			panic("unexpected case: child not found at expected index")
		}
		return child.compressedKey
	}

	panic("unexpected case")
}

func (n *node) encodeRLP(w rlp.EncoderBuffer) {
	// case 1: there are no children
	if len(n.children) == 0 { // || n.isAccountNode() {
		// the case where there is no value corresponds to an empty trie
		// the case with a value correspond to value nodes in ethereum representation.
		if !n.value.IsNothing() {
			w.WriteBytes(n.value.Value())
		} else {
			_, _ = w.Write(rlp.EmptyString)
		}
		return
	}

	// case 2: there is 1 child and no value
	// this is only possible for the root node
	if len(n.children) == 1 && n.value.IsNothing() {
		for _, child := range n.children {
			_, _ = w.Write(child.rlp)
			break // should only be 1 iteration
		}
		return
	}

	// case 3: there is multiple children
	if len(n.children) > 1 || len(n.children) == 1 && !n.value.IsNothing() {
		offset := w.List()
		for i := byte(0); i < 16; i++ {
			if child, ok := n.children[i]; ok {
				hashNodeIfNeeded(w, child.rlp)
			} else {
				_, _ = w.Write(rlp.EmptyString)
			}
		}

		if !n.value.IsNothing() {
			w.WriteBytes(n.value.Value())
		} else {
			_, _ = w.Write(rlp.EmptyString)
		}
		w.ListEnd(offset)
		return
	}

	panic("unexpected case")
}

func shortNodeIfNeeded(compressedKey []byte, val []byte, isValueNode bool) []byte {
	if isValueNode {
		compressedKey = append(compressedKey, 0x10)
	}
	if len(compressedKey) == 0 {
		// adding a short node is not needed
		return val
	}

	w := rlp.NewEncoderBuffer(nil)
	offset := w.List()
	compactKey := hexToCompact(compressedKey)
	w.WriteBytes(compactKey)
	switch {
	case isValueNode:
		_, _ = w.Write(val)
	case len(val) > 0:
		hashNodeIfNeeded(w, val)
	default:
		_, _ = w.Write(rlp.EmptyString)
	}
	w.ListEnd(offset)
	return w.ToBytes()
}

func hashNodeIfNeeded(w rlp.EncoderBuffer, val []byte) {
	if len(val) < 32 {
		_, _ = w.Write(val)
		return
	}
	w.WriteBytes(hashData(val))
}

func hashData(data []byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	_, err := hasher.Write(data)
	if err != nil {
		panic(err)
	}

	return hasher.Sum(nil)
}

func rlpToAltID(b []byte) ids.ID {
	var result ids.ID
	hash := hashData(b)
	copy(result[:], hash)
	return result
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
