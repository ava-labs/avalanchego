// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/json"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// GetBlockExtra returns the [BlockBodyExtra] carried by `b`.
func GetBlockExtra(b *ethtypes.Block) *BlockBodyExtra {
	return extras.Block.Get(b)
}

// WithBlockExtra sets `extra` on `b` and returns `b`.
func WithBlockExtra(b *ethtypes.Block, extra *BlockBodyExtra) *ethtypes.Block {
	extras.Block.Set(b, extra)
	return b
}

// BlockBodyExtra is a struct containing extra fields used by Avalanche
// in the [Block] and [Body].
type BlockBodyExtra struct {
	Version uint32
	ExtData *[]byte
}

// Copy deep-copies b; it implements [ethtypes.BlockBodyPayload].
func (b *BlockBodyExtra) Copy() *BlockBodyExtra {
	cpy := *b
	if b.ExtData != nil {
		data := slices.Clone(*b.ExtData)
		cpy.ExtData = &data
	}
	return &cpy
}

// BodyRLPFieldsForEncoding returns the fields that should be encoded
// for the [Body] and [BlockBodyExtra].
// Note the following fields are added (+) and removed (-) compared to geth:
// - (-) [ethtypes.Body] `Withdrawals` field
// - (+) [BlockBodyExtra] `Version` field
// - (+) [BlockBodyExtra] `ExtData` field
func (b *BlockBodyExtra) BodyRLPFieldsForEncoding(body *ethtypes.Body) *rlp.Fields {
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
// for the [Body] and [BlockBodyExtra].
func (b *BlockBodyExtra) BodyRLPFieldPointersForDecoding(body *ethtypes.Body) *rlp.Fields {
	return &rlp.Fields{
		Required: []any{
			&body.Transactions,
			&body.Uncles,
			&b.Version,
			&b.ExtData,
		},
	}
}

func (b *BlockBodyExtra) PostRPCMarshal(_ *ethtypes.Block, m map[string]any) {
	var extData hexutil.Bytes
	if b.ExtData != nil {
		extData = *b.ExtData
	}
	m["blockExtraData"] = extData
}

func (b *BlockBodyExtra) PostRPCUnmarshal(_ *ethtypes.Block, raw json.RawMessage) error {
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
	if data := GetBlockExtra(b).ExtData; data != nil {
		return *data
	}
	return nil
}

func BlockVersion(b *ethtypes.Block) uint32 {
	return GetBlockExtra(b).Version
}

func BlockTimeMilliseconds(b *ethtypes.Block) *uint64 {
	time := GetHeaderExtra(b.Header()).TimeMilliseconds
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
// [HeaderExtra.ExtDataHash] and whose body carries it. The block keeps
// `extdata` rather than copying it, so the caller MUST NOT modify it after.
func NewBlock(
	header *ethtypes.Header, txs []*ethtypes.Transaction, receipts []*ethtypes.Receipt, extdata []byte,
) *ethtypes.Block {
	GetHeaderExtra(header).ExtDataHash = CalcExtDataHash(extdata)
	block := ethtypes.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil))
	return WithBlockExtra(block, &BlockBodyExtra{ExtData: &extdata})
}
