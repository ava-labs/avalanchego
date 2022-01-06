// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
)

var _ Vertex = &TestVertex{}

// TestVertex is a useful test vertex
type TestVertex struct {
	choices.TestDecidable

	ParentsV      []Vertex
	ParentsErrV   error
	WhitelistV    ids.Set
	WhitelistIsV  bool
	WhitelistErrV error
	HeightV       uint64
	HeightErrV    error
	TxsV          []snowstorm.Tx
	TxsErrV       error
	BytesV        []byte
}

// Parents implements the Vertex interface
func (v *TestVertex) Parents() ([]Vertex, error) { return v.ParentsV, v.ParentsErrV }

// Whitelist implements the Vertex interface
func (v *TestVertex) Whitelist() (ids.Set, bool, error) {
	return v.WhitelistV, v.WhitelistIsV, v.WhitelistErrV
}

// Height implements the Vertex interface
func (v *TestVertex) Height() (uint64, error) { return v.HeightV, v.HeightErrV }

// Txs implements the Vertex interface
func (v *TestVertex) Txs() ([]snowstorm.Tx, error) { return v.TxsV, v.TxsErrV }

// Bytes implements the Vertex interface
func (v *TestVertex) Bytes() []byte { return v.BytesV }
