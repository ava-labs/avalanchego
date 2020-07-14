// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

// TestVertex is a useful test vertex
type TestVertex struct {
	choices.TestDecidable

	ParentsV []Vertex
	HeightV  uint64
	TxsV     []snowstorm.Tx
	BytesV   []byte
}

// Parents implements the Vertex interface
func (v *TestVertex) Parents() []Vertex { return v.ParentsV }

// Height implements the Vertex interface
func (v *TestVertex) Height() uint64 { return v.HeightV }

// Txs implements the Vertex interface
func (v *TestVertex) Txs() []snowstorm.Tx { return v.TxsV }

// Bytes implements the Vertex interface
func (v *TestVertex) Bytes() []byte { return v.BytesV }
