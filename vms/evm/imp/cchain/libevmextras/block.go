// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevmextras

import (
	"encoding/json"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// GetBlock returns the [BlockBody] carried by `b`.
func GetBlock(b *ethtypes.Block) *BlockBody {
	return blockPayloads.Block.Get(b)
}

// WithBlock sets `extra` on `b` and returns `b`.
func WithBlock(b *ethtypes.Block, extra *BlockBody) *ethtypes.Block {
	blockPayloads.Block.Set(b, extra)
	return b
}

var _ ethtypes.BlockBodyHooks = (*BlockBody)(nil)

// BlockBody is a struct containing extra fields used by Avalanche
// in the [Block] and [Body].
type BlockBody struct {
	Version uint32
	ExtData *[]byte
}

// Copy deep-copies b; it implements [ethtypes.BlockBodyPayload].
func (b *BlockBody) Copy() *BlockBody {
	cpy := *b
	if b.ExtData != nil {
		data := slices.Clone(*b.ExtData)
		cpy.ExtData = &data
	}
	return &cpy
}

// BodyRLPFieldsForEncoding returns the fields that should be encoded
// for the [Body] and [BlockBody].
// Note the following fields are added (+) and removed (-) compared to geth:
// - (-) [ethtypes.Body] `Withdrawals` field
// - (+) [BlockBody] `Version` field
// - (+) [BlockBody] `ExtData` field
func (b *BlockBody) BodyRLPFieldsForEncoding(body *ethtypes.Body) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			body.Transactions,
			body.Uncles,
			b.Version,
			b.ExtData,
		},
	}
}

// BodyRLPFieldPointersForDecoding returns the fields that should be decoded to
// for the [Body] and [BlockBody].
func (b *BlockBody) BodyRLPFieldPointersForDecoding(body *ethtypes.Body) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			&body.Transactions,
			&body.Uncles,
			&b.Version,
			&b.ExtData,
		},
	}
}

func (b *BlockBody) PostRPCMarshal(_ *ethtypes.Block, m map[string]any) {
	var extData hexutil.Bytes
	if b.ExtData != nil {
		extData = *b.ExtData
	}
	m["blockExtraData"] = extData
}

func (b *BlockBody) PostRPCUnmarshal(_ *ethtypes.Block, raw json.RawMessage) error {
	var fields struct {
		BlockExtraData *hexutil.Bytes `json:"blockExtraData"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	b.ExtData = (*[]byte)(fields.BlockExtraData)
	return nil
}

func BlockExtData(b *ethtypes.Block) []byte {
	if data := GetBlock(b).ExtData; data != nil {
		return *data
	}
	return nil
}

func BlockVersion(b *ethtypes.Block) uint32 {
	return GetBlock(b).Version
}

func BlockTimeMilliseconds(b *ethtypes.Block) *uint64 {
	time := GetHeader(b.Header()).TimeMilliseconds
	if time == nil {
		return nil
	}
	cp := *time
	return &cp
}

// CalcExtDataHash returns the hash a header commits to for its block's ext
// data; nil and empty ext data hash identically.
func CalcExtDataHash(extdata []byte) common.Hash {
	return ethtypes.RLPHash(extdata)
}

// EmptyExtDataHash is [CalcExtDataHash] of no ext data.
var EmptyExtDataHash = CalcExtDataHash(nil)

// NewBlock builds a block whose header commits to `extdata` via
// [Header.ExtDataHash] and whose body carries it. The block keeps
// `extdata` rather than copying it, so the caller MUST NOT modify it after.
func NewBlock(
	header *ethtypes.Header, txs []*ethtypes.Transaction, receipts []*ethtypes.Receipt, extdata []byte,
) *ethtypes.Block {
	GetHeader(header).ExtDataHash = CalcExtDataHash(extdata)
	block := ethtypes.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil))
	return WithBlock(block, &BlockBody{ExtData: &extdata})
}
