// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"encoding/binary"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// extDataVersion marks block extra data that carries [ExtData] rather than a
// bare slice of atomic transactions.
const extDataVersion uint16 = 1

// ImportRecord is a UTXO that a block imports through the cross-chain transfer
// precompile. The builder resolves it from shared memory before the block is
// accepted so that execution and replay never read shared memory.
type ImportRecord struct {
	UTXOID      avax.UTXOID `serialize:"true"`
	SourceChain ids.ID      `serialize:"true"`
	Owner       ids.ShortID `serialize:"true"`
	Amount      uint64      `serialize:"true"`
}

// ExtData is the block extra data after the cross-chain transfer precompile.
type ExtData struct {
	Txs     []*Tx          `serialize:"true"`
	Imports []ImportRecord `serialize:"true"`
}

var errEmptyImports = errors.New("extData with imports must have at least one import")

// MarshalExtData returns the canonical block extra data. Blocks without
// precompile imports keep the pre-existing atomic transaction encoding.
func MarshalExtData(txs []*Tx, imports []ImportRecord) ([]byte, error) {
	if len(imports) == 0 {
		return MarshalSlice(txs)
	}
	return c.Marshal(extDataVersion, &ExtData{Txs: txs, Imports: imports})
}

// ParseExtData deserializes block extra data written by [MarshalExtData].
func ParseExtData(b []byte) ([]*Tx, []ImportRecord, error) {
	if len(b) < 2 || binary.BigEndian.Uint16(b) != extDataVersion {
		txs, err := ParseSlice(b)
		return txs, nil, err
	}
	var e ExtData
	if _, err := c.Unmarshal(b, &e); err != nil {
		return nil, nil, err
	}
	if len(e.Imports) == 0 {
		return nil, nil, errEmptyImports
	}
	return e.Txs, e.Imports, nil
}
