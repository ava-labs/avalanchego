// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

func (b *Block) WithExtData(version uint32, extdata *[]byte) *Block {
	b.version = version
	b.setExtDataHelper(extdata, false)
	return b
}

func (b *Block) setExtDataHelper(data *[]byte, recalc bool) {
	if data == nil {
		b.setExtData(nil, recalc)
		return
	}
	b.setExtData(*data, recalc)
}

func (b *Block) setExtData(data []byte, recalc bool) {
	_data := make([]byte, len(data))
	b.extdata = &_data
	copy(*b.extdata, data)
	if recalc {
		b.header.ExtDataHash = CalcExtDataHash(*b.extdata)
	}
}

func (b *Block) ExtData() []byte {
	if b.extdata == nil {
		return nil
	}
	return *b.extdata
}

func (b *Block) Version() uint32 {
	return b.version
}

func (b *Block) ExtDataGasUsed() *big.Int {
	if b.header.ExtDataGasUsed == nil {
		return nil
	}
	return new(big.Int).Set(b.header.ExtDataGasUsed)
}

func CalcExtDataHash(extdata []byte) common.Hash {
	if len(extdata) == 0 {
		return EmptyExtDataHash
	}
	return rlpHash(extdata)
}

func NewBlockWithExtData(
	header *Header, txs []*Transaction, uncles []*Header, receipts []*Receipt,
	hasher TrieHasher, extdata []byte, recalc bool,
) *Block {
	b := NewBlock(header, txs, uncles, receipts, hasher)
	b.setExtData(extdata, recalc)
	return b
}
