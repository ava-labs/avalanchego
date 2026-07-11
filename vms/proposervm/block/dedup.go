// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "errors"

var errUnknownBlockType = errors.New("unknown block type")

// setInnerBytes replaces the inner (wrapped) block bytes carried by [b].
func setInnerBytes(b Block, innerBytes []byte) error {
	switch blk := b.(type) {
	case *statelessBlock:
		blk.StatelessBlock.Block = innerBytes
	case *statelessGraniteBlock:
		blk.StatelessGraniteBlock.StatelessBlock.Block = innerBytes
	case *option:
		blk.InnerBytes = innerBytes
	default:
		return errUnknownBlockType
	}
	return nil
}

// StripInnerBytes returns the serialized form of the proposervm block encoded by
// [fullBytes] with its inner (wrapped) block bytes removed. The inner bytes can
// be re-injected with [RestoreInnerBytes] to recover [fullBytes] exactly.
//
// This lets a caller that can recover the inner block bytes from another source
// (e.g. the inner VM) avoid persisting them a second time.
func StripInnerBytes(fullBytes []byte) ([]byte, error) {
	var b Block
	version, err := Codec.Unmarshal(fullBytes, &b)
	if err != nil {
		return nil, err
	}
	if err := setInnerBytes(b, nil); err != nil {
		return nil, err
	}
	return Codec.Marshal(version, &b)
}

// RestoreInnerBytes is the inverse of [StripInnerBytes]: it re-injects
// [innerBytes] into the stripped block encoded by [strippedBytes] and returns
// the recovered full serialized block. When [innerBytes] are the exact bytes
// that were stripped, the result is byte-identical to the original block, so its
// ID and signature are preserved.
func RestoreInnerBytes(strippedBytes []byte, innerBytes []byte) ([]byte, error) {
	var b Block
	version, err := Codec.Unmarshal(strippedBytes, &b)
	if err != nil {
		return nil, err
	}
	if err := setInnerBytes(b, innerBytes); err != nil {
		return nil, err
	}
	return Codec.Marshal(version, &b)
}
